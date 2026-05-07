"""HTTP transport for generation export."""

from __future__ import annotations

import json
from urllib import request as urllib_request

from ..models import (
    ExportGenerationResult,
    ExportGenerationsRequest,
    ExportGenerationsResponse,
    ExportWorkflowStepResult,
    ExportWorkflowStepsRequest,
    ExportWorkflowStepsResponse,
)
from ..proto_mapping import generation_to_proto_json, workflow_step_to_proto_json


class HTTPGenerationExporter:
    """Sends generation batches over HTTP JSON parity endpoint."""

    def __init__(self, endpoint: str, headers: dict[str, str] | None = None) -> None:
        self._endpoint = _normalize_endpoint(endpoint, _EXPORT_PATH)
        self._wf_endpoint = _normalize_endpoint(endpoint, _WF_EXPORT_PATH)
        self._headers = dict(headers or {})

    def export_generations(self, request: ExportGenerationsRequest) -> ExportGenerationsResponse:
        payload = {
            "generations": [generation_to_proto_json(generation) for generation in request.generations],
        }
        body = json.dumps(payload).encode("utf-8")

        req = urllib_request.Request(
            self._endpoint,
            data=body,
            method="POST",
            headers={
                "Content-Type": "application/json",
                **self._headers,
            },
        )
        try:
            with urllib_request.urlopen(req, timeout=10) as response:
                status = response.getcode()
                raw = response.read()
        except Exception as exc:  # noqa: BLE001
            raise RuntimeError(f"http generation export failed: {exc}") from exc

        if status < 200 or status >= 300:
            raise RuntimeError(
                f"http generation export status {status}: {raw.decode('utf-8', errors='replace').strip()}"
            )

        parsed = json.loads(raw.decode("utf-8"))
        results = []
        for index, result in enumerate(parsed.get("results", [])):
            generation_id = result.get("generation_id") or result.get("generationId") or request.generations[index].id
            results.append(
                ExportGenerationResult(
                    generation_id=generation_id,
                    accepted=bool(result.get("accepted", False)),
                    error=str(result.get("error", "") or ""),
                )
            )
        return ExportGenerationsResponse(results=results)

    def export_workflow_steps(self, request: ExportWorkflowStepsRequest) -> ExportWorkflowStepsResponse:
        payload = {
            "workflow_steps": [workflow_step_to_proto_json(step) for step in request.workflow_steps],
        }
        body = json.dumps(payload).encode("utf-8")

        req = urllib_request.Request(
            self._wf_endpoint,
            data=body,
            method="POST",
            headers={
                "Content-Type": "application/json",
                **self._headers,
            },
        )
        try:
            with urllib_request.urlopen(req, timeout=10) as response:
                status = response.getcode()
                raw = response.read()
        except Exception as exc:  # noqa: BLE001
            raise RuntimeError(f"http workflow step export failed: {exc}") from exc

        if status < 200 or status >= 300:
            raise RuntimeError(
                f"http workflow step export status {status}: {raw.decode('utf-8', errors='replace').strip()}"
            )

        parsed = json.loads(raw.decode("utf-8"))
        results = []
        for index, result in enumerate(parsed.get("results", [])):
            step_id = result.get("step_id") or result.get("stepId") or request.workflow_steps[index].id
            results.append(
                ExportWorkflowStepResult(
                    step_id=step_id,
                    accepted=bool(result.get("accepted", False)),
                    error=str(result.get("error", "") or ""),
                )
            )
        return ExportWorkflowStepsResponse(results=results)

    def shutdown(self) -> None:
        """HTTP exporter has no persistent resources."""


_EXPORT_PATH = "/api/v1/generations:export"
_WF_EXPORT_PATH = "/api/v1/workflow-steps:export"


def _normalize_endpoint(endpoint: str, path: str = _EXPORT_PATH) -> str:
    trimmed = endpoint.strip()
    if not trimmed:
        raise ValueError("endpoint is required")
    if not (trimmed.startswith("http://") or trimmed.startswith("https://")):
        trimmed = f"http://{trimmed}"
    base = trimmed.rstrip("/")
    if base.endswith(_EXPORT_PATH):
        base = base[: -len(_EXPORT_PATH)]
    if base.endswith(_WF_EXPORT_PATH):
        base = base[: -len(_WF_EXPORT_PATH)]
    return base + path
