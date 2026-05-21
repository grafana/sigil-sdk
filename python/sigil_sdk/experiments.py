"""Experiment and score models for Sigil offline evaluation."""

from __future__ import annotations

import hashlib
import logging
from collections.abc import Callable, Iterable, Sequence
from dataclasses import dataclass, field, is_dataclass
from datetime import datetime, timezone
from typing import Any, Generic, Literal, TypeVar

_logger = logging.getLogger(__name__)

ExperimentStatus = Literal["running", "succeeded", "failed", "canceled"]
ExperimentSource = Literal["collection", "external"]
ScoreType = Literal["number", "bool", "string"]

InputT = TypeVar("InputT")
ExpectedT = TypeVar("ExpectedT")
OutputT = TypeVar("OutputT")


@dataclass(slots=True)
class ScoreValue:
    """Typed Sigil score value. Exactly one field must be set."""

    number: float | None = None
    boolean: bool | None = None
    string: str | None = None

    def to_payload(self) -> dict[str, Any]:
        out: dict[str, Any] = {}
        if self.number is not None:
            out["number"] = self.number
        if self.boolean is not None:
            out["bool"] = self.boolean
        if self.string is not None:
            out["string"] = self.string
        return out

    @classmethod
    def from_payload(cls, payload: dict[str, Any] | None) -> ScoreValue:
        payload = payload or {}
        return cls(
            number=payload.get("number"),
            boolean=payload.get("bool"),
            string=payload.get("string"),
        )


@dataclass(slots=True)
class ScoreSource:
    kind: str = ""
    id: str = ""

    def to_payload(self) -> dict[str, Any]:
        out: dict[str, Any] = {}
        if self.kind:
            out["kind"] = self.kind
        if self.id:
            out["id"] = self.id
        return out


@dataclass(slots=True)
class ScoreItem:
    score_id: str
    generation_id: str
    evaluator_id: str
    evaluator_version: str
    score_key: str
    value: ScoreValue
    conversation_id: str = ""
    trace_id: str = ""
    span_id: str = ""
    rule_id: str = ""
    run_id: str = ""
    passed: bool | None = None
    explanation: str = ""
    metadata: dict[str, Any] = field(default_factory=dict)
    created_at: datetime | None = None
    source: ScoreSource | None = None

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "score_id": self.score_id,
            "generation_id": self.generation_id,
            "evaluator_id": self.evaluator_id,
            "evaluator_version": self.evaluator_version,
            "score_key": self.score_key,
            "value": self.value.to_payload(),
        }
        _put_if(payload, "conversation_id", self.conversation_id)
        _put_if(payload, "trace_id", self.trace_id)
        _put_if(payload, "span_id", self.span_id)
        _put_if(payload, "rule_id", self.rule_id)
        _put_if(payload, "run_id", self.run_id)
        if self.passed is not None:
            payload["passed"] = self.passed
        _put_if(payload, "explanation", self.explanation)
        if self.metadata:
            payload["metadata"] = self.metadata
        if self.created_at is not None:
            payload["created_at"] = _format_datetime(self.created_at)
        if self.source is not None:
            source = self.source.to_payload()
            if source:
                payload["source"] = source
        return payload


@dataclass(slots=True)
class ExportScoresRequest:
    scores: list[ScoreItem] = field(default_factory=list)

    def to_payload(self) -> dict[str, Any]:
        return {"scores": [score.to_payload() for score in self.scores]}


@dataclass(slots=True)
class ExportScoreResult:
    score_id: str
    accepted: bool
    error: str = ""

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> ExportScoreResult:
        return cls(
            score_id=str(payload.get("score_id", "")),
            accepted=bool(payload.get("accepted", False)),
            error=str(payload.get("error", "")),
        )


@dataclass(slots=True)
class ExportScoresResponse:
    results: list[ExportScoreResult] = field(default_factory=list)

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> ExportScoresResponse:
        raw_results = payload.get("results", [])
        results = [ExportScoreResult.from_payload(item) for item in raw_results if isinstance(item, dict)]
        return cls(results=results)

    @property
    def accepted_count(self) -> int:
        return sum(1 for result in self.results if result.accepted)

    @property
    def rejected_count(self) -> int:
        return sum(1 for result in self.results if not result.accepted)


@dataclass(slots=True)
class ExperimentEvaluator:
    id: str
    selector: str

    def to_payload(self) -> dict[str, Any]:
        return {"id": self.id, "selector": self.selector}


@dataclass(slots=True)
class CreateExperimentRequest:
    name: str
    source: ExperimentSource = "external"
    run_id: str = ""
    description: str = ""
    tags: list[str] = field(default_factory=list)
    collection_id: str = ""
    evaluators: list[ExperimentEvaluator] = field(default_factory=list)
    metadata: dict[str, Any] = field(default_factory=dict)

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {"name": self.name, "source": self.source}
        _put_if(payload, "run_id", self.run_id)
        _put_if(payload, "description", self.description)
        if self.tags:
            payload["tags"] = self.tags
        _put_if(payload, "collection_id", self.collection_id)
        if self.evaluators:
            payload["evaluators"] = [entry.to_payload() for entry in self.evaluators]
        if self.metadata:
            payload["metadata"] = self.metadata
        return payload


@dataclass(slots=True)
class UpdateExperimentRequest:
    name: str | None = None
    description: str | None = None
    tags: list[str] | None = None
    status: ExperimentStatus | None = None
    metadata: dict[str, Any] | None = None
    error: str | None = None
    score_count: int | None = None

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {}
        if self.name is not None:
            payload["name"] = self.name
        if self.description is not None:
            payload["description"] = self.description
        if self.tags is not None:
            payload["tags"] = self.tags
        if self.status is not None:
            payload["status"] = self.status
        if self.metadata is not None:
            payload["metadata"] = self.metadata
        if self.error is not None:
            payload["error"] = self.error
        if self.score_count is not None:
            payload["score_count"] = self.score_count
        return payload


@dataclass(slots=True)
class Experiment:
    run_id: str
    name: str
    source: str
    status: str
    tenant_id: str = ""
    description: str = ""
    tags: list[str] = field(default_factory=list)
    collection_id: str = ""
    evaluators: list[ExperimentEvaluator] = field(default_factory=list)
    metadata: dict[str, Any] = field(default_factory=dict)
    score_count: int = 0
    error: str = ""
    created_by: str = ""
    created_at: datetime | None = None
    updated_at: datetime | None = None
    started_at: datetime | None = None
    completed_at: datetime | None = None

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> Experiment:
        return cls(
            tenant_id=str(payload.get("tenant_id", "")),
            run_id=str(payload.get("run_id", "")),
            name=str(payload.get("name", "")),
            description=str(payload.get("description", "")),
            tags=list(payload.get("tags") or []),
            source=str(payload.get("source", "")),
            status=str(payload.get("status", "")),
            collection_id=str(payload.get("collection_id", "")),
            evaluators=[
                ExperimentEvaluator(id=str(item.get("id", "")), selector=str(item.get("selector", "")))
                for item in payload.get("evaluators", []) or []
                if isinstance(item, dict)
            ],
            metadata=dict(payload.get("metadata") or {}),
            score_count=int(payload.get("score_count", 0) or 0),
            error=str(payload.get("error", "")),
            created_by=str(payload.get("created_by", "")),
            created_at=_parse_datetime(payload.get("created_at")),
            updated_at=_parse_datetime(payload.get("updated_at")),
            started_at=_parse_datetime(payload.get("started_at")),
            completed_at=_parse_datetime(payload.get("completed_at")),
        )


@dataclass(slots=True)
class ExperimentListResponse:
    items: list[Experiment]
    next_cursor: str = ""

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> ExperimentListResponse:
        return cls(
            items=[Experiment.from_payload(item) for item in payload.get("items", []) if isinstance(item, dict)],
            next_cursor=str(payload.get("next_cursor", "")),
        )


@dataclass(slots=True)
class ExperimentScoresResponse:
    items: list[dict[str, Any]]
    next_cursor: str = ""

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> ExperimentScoresResponse:
        return cls(
            items=[dict(item) for item in payload.get("items", []) if isinstance(item, dict)],
            next_cursor=str(payload.get("next_cursor", "")),
        )


@dataclass(slots=True)
class ExperimentReport:
    run: Experiment
    summary: dict[str, Any]
    breakdowns: dict[str, Any]
    points: list[dict[str, Any]]

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> ExperimentReport:
        run_payload = payload.get("run")
        return cls(
            run=Experiment.from_payload(run_payload if isinstance(run_payload, dict) else {}),
            summary=dict(payload.get("summary") or {}),
            breakdowns=dict(payload.get("breakdowns") or {}),
            points=[dict(item) for item in payload.get("points", []) if isinstance(item, dict)],
        )


@dataclass(slots=True)
class DatasetRef:
    id: str
    version: str = ""
    uri: str = ""

    def to_metadata(self) -> dict[str, Any]:
        out = {"dataset_id": self.id}
        _put_if(out, "dataset_version", self.version)
        _put_if(out, "dataset_uri", self.uri)
        return out


@dataclass(slots=True)
class CandidateRef:
    git_sha: str = ""
    agent_name: str = ""
    agent_version: str = ""
    prompt_version: str = ""
    model: dict[str, Any] = field(default_factory=dict)

    def to_metadata(self) -> dict[str, Any]:
        out: dict[str, Any] = {}
        _put_if(out, "git_sha", self.git_sha)
        _put_if(out, "agent_name", self.agent_name)
        _put_if(out, "agent_version", self.agent_version)
        _put_if(out, "prompt_version", self.prompt_version)
        if self.model:
            out["model"] = self.model
        return out


@dataclass(slots=True)
class ExperimentSpec:
    run_id: str
    name: str
    dataset: DatasetRef
    description: str = ""
    tags: list[str] = field(default_factory=list)
    candidate: CandidateRef | None = None
    baseline_run_id: str = ""
    metadata: dict[str, Any] = field(default_factory=dict)

    def create_request(self) -> CreateExperimentRequest:
        metadata = dict(self.metadata)
        metadata.update(self.dataset.to_metadata())
        if self.candidate is not None:
            candidate = self.candidate.to_metadata()
            if candidate:
                metadata["candidate"] = candidate
        _put_if(metadata, "baseline_run_id", self.baseline_run_id)
        return CreateExperimentRequest(
            run_id=self.run_id,
            name=self.name,
            description=self.description,
            tags=self.tags,
            source="external",
            metadata=metadata,
        )


@dataclass(slots=True)
class DatasetItem(Generic[InputT, ExpectedT]):
    id: str
    input: InputT
    expected: ExpectedT | None = None
    metadata: dict[str, Any] = field(default_factory=dict)


@dataclass(slots=True)
class DatasetTargetResult(Generic[OutputT]):
    output: OutputT
    generation_ids: list[str]
    conversation_id: str = ""
    metadata: dict[str, Any] = field(default_factory=dict)


@dataclass(slots=True)
class ScoreOutput:
    evaluator_id: str
    evaluator_version: str
    score_key: str
    value: ScoreValue
    generation_id: str = ""
    conversation_id: str = ""
    passed: bool | None = None
    explanation: str = ""
    metadata: dict[str, Any] = field(default_factory=dict)


@dataclass(slots=True)
class ExperimentRunResult:
    experiment: Experiment
    report: ExperimentReport
    exported_scores: int


class ExperimentRunner(Generic[InputT, ExpectedT, OutputT]):
    """Small external-run helper over normal Sigil generation instrumentation."""

    def __init__(
        self,
        client: Any,
        spec: ExperimentSpec,
        *,
        score_batch_size: int = 100,
    ) -> None:
        self._client = client
        self._spec = spec
        self._score_batch_size = max(1, score_batch_size)

    def run(
        self,
        items: Iterable[DatasetItem[InputT, ExpectedT]],
        target: Callable[[DatasetItem[InputT, ExpectedT]], DatasetTargetResult[OutputT]],
        scorers: Sequence[
            Callable[[DatasetItem[InputT, ExpectedT], DatasetTargetResult[OutputT]], Sequence[ScoreOutput]]
        ],
    ) -> ExperimentRunResult:
        """Run dataset items, export scores, finalize the experiment, and return its report."""

        experiment = self._client.create_experiment(self._spec.create_request())
        exported_scores = 0
        pending_scores: list[ScoreItem] = []

        try:
            for item in items:
                result = target(item)
                self._client.flush()
                for scorer in scorers:
                    for output in scorer(item, result):
                        pending_scores.append(self._score_item(item, result, output))
                        if len(pending_scores) >= self._score_batch_size:
                            exported_scores += self._export_batch(pending_scores)
                            pending_scores = []
            if pending_scores:
                exported_scores += self._export_batch(pending_scores)

            experiment = self._client.update_experiment(
                self._spec.run_id,
                UpdateExperimentRequest(status="succeeded", score_count=exported_scores),
            )
        except Exception as exc:  # noqa: BLE001
            try:
                self._client.update_experiment(
                    self._spec.run_id,
                    UpdateExperimentRequest(status="failed", error=str(exc), score_count=exported_scores),
                )
            except Exception as cleanup_exc:  # noqa: BLE001
                # Surface the original task error; just record that finalization failed too.
                _logger.warning(
                    "failed to mark experiment %s as failed during cleanup: %s",
                    self._spec.run_id,
                    cleanup_exc,
                )
            raise

        report = self._client.get_experiment_report(self._spec.run_id)
        return ExperimentRunResult(experiment=experiment, report=report, exported_scores=exported_scores)

    def _score_item(
        self,
        item: DatasetItem[InputT, ExpectedT],
        result: DatasetTargetResult[OutputT],
        output: ScoreOutput,
    ) -> ScoreItem:
        generation_id = output.generation_id
        if not generation_id:
            if len(result.generation_ids) != 1:
                raise ValueError("score output must set generation_id when target produced multiple generations")
            generation_id = result.generation_ids[0]

        conversation_id = output.conversation_id or result.conversation_id
        metadata = dict(self._spec.dataset.to_metadata())
        metadata["item_id"] = item.id
        metadata.update(item.metadata)
        metadata.update(result.metadata)
        metadata.update(output.metadata)
        if self._spec.candidate is not None:
            candidate = self._spec.candidate.to_metadata()
            if candidate:
                metadata["candidate"] = candidate

        trial_id = str(metadata.get("trial_id", ""))
        score_id = deterministic_score_id(
            self._spec.run_id,
            item.id,
            generation_id,
            output.evaluator_id,
            output.evaluator_version,
            output.score_key,
            trial_id,
        )
        return ScoreItem(
            score_id=score_id,
            generation_id=generation_id,
            conversation_id=conversation_id,
            evaluator_id=output.evaluator_id,
            evaluator_version=output.evaluator_version,
            score_key=output.score_key,
            value=output.value,
            run_id=self._spec.run_id,
            passed=output.passed,
            explanation=output.explanation,
            metadata=metadata,
            source=ScoreSource(kind="experiment", id=self._spec.run_id),
        )

    def _export_batch(self, scores: list[ScoreItem]) -> int:
        response = self._client.export_scores(ExportScoresRequest(scores=list(scores)))
        rejected = [result for result in response.results if not result.accepted]
        if rejected:
            first = rejected[0]
            raise RuntimeError(f"score export rejected {len(rejected)} score(s), first={first.score_id}: {first.error}")
        return response.accepted_count


def deterministic_score_id(*parts: str) -> str:
    material = "|".join(str(part) for part in parts)
    digest = hashlib.sha256(material.encode("utf-8")).hexdigest()[:32]
    return f"sc_{digest}"


def payload_from_request(value: Any) -> dict[str, Any]:
    if hasattr(value, "to_payload"):
        return value.to_payload()
    if is_dataclass(value):
        return {key: _json_value(val) for key, val in value.__dict__.items() if val is not None}
    if isinstance(value, dict):
        return dict(value)
    raise TypeError(f"unsupported request type {type(value).__name__}")


def _json_value(value: Any) -> Any:
    if hasattr(value, "to_payload"):
        return value.to_payload()
    if isinstance(value, datetime):
        return _format_datetime(value)
    if isinstance(value, list):
        return [_json_value(item) for item in value]
    if isinstance(value, dict):
        return {key: _json_value(val) for key, val in value.items()}
    return value


def _put_if(payload: dict[str, Any], key: str, value: Any) -> None:
    if value is not None and value != "":
        payload[key] = value


def _format_datetime(value: datetime) -> str:
    normalized = value.astimezone(timezone.utc) if value.tzinfo else value.replace(tzinfo=timezone.utc)
    return normalized.isoformat().replace("+00:00", "Z")


def _parse_datetime(value: Any) -> datetime | None:
    if not isinstance(value, str) or value == "":
        return None
    normalized = value.replace("Z", "+00:00")
    try:
        parsed = datetime.fromisoformat(normalized)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        return parsed.replace(tzinfo=timezone.utc)
    return parsed.astimezone(timezone.utc)
