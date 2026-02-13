"""Anthropic wrapper helpers and payload mappers."""

from __future__ import annotations

import copy
from dataclasses import asdict, dataclass, field
import json
from typing import Any, Awaitable, Callable

from sigil_sdk import (
    Artifact,
    ArtifactKind,
    Generation,
    GenerationMode,
    GenerationStart,
    Message,
    MessageRole,
    ModelRef,
    Part,
    PartKind,
    TokenUsage,
    ToolDefinition,
)

_thinking_budget_metadata_key = "sigil.gen_ai.request.thinking.budget_tokens"


@dataclass(slots=True)
class AnthropicMessage:
    """Simplified Anthropic message shape."""

    role: str
    content: str
    name: str = ""


@dataclass(slots=True)
class AnthropicRequest:
    """Simplified Anthropic completion request shape."""

    model: str
    messages: list[AnthropicMessage]
    system_prompt: str = ""
    max_tokens: int | None = None
    temperature: float | None = None
    top_p: float | None = None
    tool_choice: Any = None
    thinking: Any = None
    tools: list[ToolDefinition] = field(default_factory=list)


@dataclass(slots=True)
class AnthropicResponse:
    """Simplified Anthropic completion response shape."""

    output_text: str
    id: str = ""
    model: str = ""
    usage: TokenUsage = field(default_factory=TokenUsage)
    stop_reason: str = ""
    raw: Any = None


@dataclass(slots=True)
class AnthropicStreamSummary:
    """Simplified Anthropic stream summary shape."""

    output_text: str
    final_response: AnthropicResponse | None = None
    events: list[Any] = field(default_factory=list)


@dataclass(slots=True)
class AnthropicOptions:
    """Optional Sigil enrichments for Anthropic wrappers."""

    provider_name: str = "anthropic"
    conversation_id: str = ""
    agent_name: str = ""
    agent_version: str = ""
    tags: dict[str, str] = field(default_factory=dict)
    metadata: dict[str, Any] = field(default_factory=dict)
    raw_artifacts: bool = False


def completion(
    client,
    request: AnthropicRequest,
    provider_call: Callable[[AnthropicRequest], AnthropicResponse],
    options: AnthropicOptions | None = None,
) -> AnthropicResponse:
    """Runs a sync Anthropic call and records a `SYNC` generation."""

    opts = options or AnthropicOptions()
    recorder = client.start_generation(_start_payload(request, opts, mode=GenerationMode.SYNC))
    try:
        response = provider_call(request)
        recorder.set_result(from_request_response(request, response, opts))
    except Exception as exc:  # noqa: BLE001
        recorder.set_call_error(exc)
        raise
    finally:
        recorder.end()

    if recorder.err() is not None:
        raise recorder.err()
    return response


async def completion_async(
    client,
    request: AnthropicRequest,
    provider_call: Callable[[AnthropicRequest], Awaitable[AnthropicResponse]],
    options: AnthropicOptions | None = None,
) -> AnthropicResponse:
    """Runs an async Anthropic call and records a `SYNC` generation."""

    opts = options or AnthropicOptions()
    recorder = client.start_generation(_start_payload(request, opts, mode=GenerationMode.SYNC))
    try:
        response = await provider_call(request)
        recorder.set_result(from_request_response(request, response, opts))
    except Exception as exc:  # noqa: BLE001
        recorder.set_call_error(exc)
        raise
    finally:
        recorder.end()

    if recorder.err() is not None:
        raise recorder.err()
    return response


def completion_stream(
    client,
    request: AnthropicRequest,
    provider_call: Callable[[AnthropicRequest], AnthropicStreamSummary],
    options: AnthropicOptions | None = None,
) -> AnthropicStreamSummary:
    """Runs a sync Anthropic stream flow and records a `STREAM` generation."""

    opts = options or AnthropicOptions()
    recorder = client.start_streaming_generation(_start_payload(request, opts, mode=GenerationMode.STREAM))
    try:
        summary = provider_call(request)
        recorder.set_result(from_stream(request, summary, opts))
    except Exception as exc:  # noqa: BLE001
        recorder.set_call_error(exc)
        raise
    finally:
        recorder.end()

    if recorder.err() is not None:
        raise recorder.err()
    return summary


async def completion_stream_async(
    client,
    request: AnthropicRequest,
    provider_call: Callable[[AnthropicRequest], Awaitable[AnthropicStreamSummary]],
    options: AnthropicOptions | None = None,
) -> AnthropicStreamSummary:
    """Runs an async Anthropic stream flow and records a `STREAM` generation."""

    opts = options or AnthropicOptions()
    recorder = client.start_streaming_generation(_start_payload(request, opts, mode=GenerationMode.STREAM))
    try:
        summary = await provider_call(request)
        recorder.set_result(from_stream(request, summary, opts))
    except Exception as exc:  # noqa: BLE001
        recorder.set_call_error(exc)
        raise
    finally:
        recorder.end()

    if recorder.err() is not None:
        raise recorder.err()
    return summary


def from_request_response(
    request: AnthropicRequest,
    response: AnthropicResponse,
    options: AnthropicOptions | None = None,
) -> Generation:
    """Maps Anthropic request/response payloads into a normalized generation."""

    opts = options or AnthropicOptions()
    metadata = _metadata_with_thinking_budget(opts.metadata, _anthropic_thinking_budget(request.thinking))
    generation = Generation(
        conversation_id=opts.conversation_id,
        agent_name=opts.agent_name,
        agent_version=opts.agent_version,
        mode=GenerationMode.SYNC,
        model=ModelRef(provider=opts.provider_name, name=request.model),
        response_id=response.id,
        response_model=response.model or request.model,
        system_prompt=request.system_prompt,
        max_tokens=request.max_tokens,
        temperature=request.temperature,
        top_p=request.top_p,
        tool_choice=_canonical_tool_choice(request.tool_choice),
        thinking_enabled=_anthropic_thinking_enabled(request.thinking),
        input=_map_input_messages(request.messages),
        output=[_assistant_text_message(response.output_text)],
        tools=copy.deepcopy(request.tools),
        usage=copy.deepcopy(response.usage),
        stop_reason=response.stop_reason,
        tags=dict(opts.tags),
        metadata=metadata,
    )

    if opts.raw_artifacts:
        generation.artifacts = [
            Artifact(
                kind=ArtifactKind.REQUEST,
                name="anthropic.request",
                content_type="application/json",
                payload=_json_bytes(asdict(request)),
            ),
            Artifact(
                kind=ArtifactKind.RESPONSE,
                name="anthropic.response",
                content_type="application/json",
                payload=_json_bytes(response.raw if response.raw is not None else asdict(response)),
            ),
        ]
        if request.tools:
            generation.artifacts.append(
                Artifact(
                    kind=ArtifactKind.TOOLS,
                    name="anthropic.tools",
                    content_type="application/json",
                    payload=_json_bytes([asdict(tool) for tool in request.tools]),
                )
            )

    return generation


def from_stream(
    request: AnthropicRequest,
    summary: AnthropicStreamSummary,
    options: AnthropicOptions | None = None,
) -> Generation:
    """Maps Anthropic stream summary into a normalized generation."""

    opts = options or AnthropicOptions()
    metadata = _metadata_with_thinking_budget(opts.metadata, _anthropic_thinking_budget(request.thinking))
    if summary.final_response is not None:
        generation = from_request_response(request, summary.final_response, opts)
        generation.mode = GenerationMode.STREAM
    else:
        generation = Generation(
            conversation_id=opts.conversation_id,
            agent_name=opts.agent_name,
            agent_version=opts.agent_version,
            mode=GenerationMode.STREAM,
            model=ModelRef(provider=opts.provider_name, name=request.model),
            response_model=request.model,
            system_prompt=request.system_prompt,
            max_tokens=request.max_tokens,
            temperature=request.temperature,
            top_p=request.top_p,
            tool_choice=_canonical_tool_choice(request.tool_choice),
            thinking_enabled=_anthropic_thinking_enabled(request.thinking),
            input=_map_input_messages(request.messages),
            output=[_assistant_text_message(summary.output_text)],
            tools=copy.deepcopy(request.tools),
            tags=dict(opts.tags),
            metadata=metadata,
        )

    if generation.output:
        generation.output[0] = _assistant_text_message(summary.output_text)
    else:
        generation.output = [_assistant_text_message(summary.output_text)]

    if opts.raw_artifacts:
        if not any(artifact.kind == ArtifactKind.REQUEST for artifact in generation.artifacts):
            generation.artifacts.append(
                Artifact(
                    kind=ArtifactKind.REQUEST,
                    name="anthropic.request",
                    content_type="application/json",
                    payload=_json_bytes(asdict(request)),
                )
            )
        generation.artifacts.append(
            Artifact(
                kind=ArtifactKind.PROVIDER_EVENT,
                name="anthropic.stream.events",
                content_type="application/json",
                payload=_json_bytes(summary.events),
            )
        )

    return generation


def _map_input_messages(messages: list[AnthropicMessage]) -> list[Message]:
    mapped: list[Message] = []
    for message in messages:
        if message.role == "system":
            continue
        mapped.append(
            Message(
                role=_normalize_role(message.role),
                name=message.name,
                parts=[Part(kind=PartKind.TEXT, text=message.content)],
            )
        )
    return mapped


def _normalize_role(role: str) -> MessageRole:
    if role == "assistant":
        return MessageRole.ASSISTANT
    if role == "tool":
        return MessageRole.TOOL
    return MessageRole.USER


def _assistant_text_message(text: str) -> Message:
    return Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text=text)])


def _start_payload(
    request: AnthropicRequest,
    options: AnthropicOptions,
    mode: GenerationMode,
) -> GenerationStart:
    metadata = _metadata_with_thinking_budget(options.metadata, _anthropic_thinking_budget(request.thinking))
    return GenerationStart(
        conversation_id=options.conversation_id,
        agent_name=options.agent_name,
        agent_version=options.agent_version,
        mode=mode,
        model=ModelRef(provider=options.provider_name, name=request.model),
        system_prompt=request.system_prompt,
        max_tokens=request.max_tokens,
        temperature=request.temperature,
        top_p=request.top_p,
        tool_choice=_canonical_tool_choice(request.tool_choice),
        thinking_enabled=_anthropic_thinking_enabled(request.thinking),
        tools=copy.deepcopy(request.tools),
        tags=dict(options.tags),
        metadata=metadata,
    )


def _json_bytes(payload: Any) -> bytes:
    return json.dumps(payload, default=str, separators=(",", ":")).encode("utf-8")


def _anthropic_thinking_enabled(thinking: Any) -> bool | None:
    if thinking is None:
        return None
    if isinstance(thinking, bool):
        return thinking
    if isinstance(thinking, dict):
        if "enabled" in thinking and isinstance(thinking["enabled"], bool):
            return thinking["enabled"]
        candidate = thinking.get("type") or thinking.get("mode")
        if isinstance(candidate, str):
            normalized = candidate.strip().lower()
            if normalized in {"enabled", "adaptive"}:
                return True
            if normalized == "disabled":
                return False
        return None
    normalized = str(thinking).strip().lower()
    if normalized in {"enabled", "adaptive"}:
        return True
    if normalized == "disabled":
        return False
    return None


def _anthropic_thinking_budget(thinking: Any) -> int | None:
    if not isinstance(thinking, dict):
        return None
    return _coerce_int(thinking.get("budget_tokens"))


def _canonical_tool_choice(value: Any) -> str | None:
    if value is None:
        return None
    if isinstance(value, str):
        normalized = value.strip().lower()
        return normalized or None
    if hasattr(value, "value"):
        normalized = str(value.value).strip().lower()
        return normalized or None
    try:
        encoded = json.dumps(value, default=str, separators=(",", ":"), sort_keys=True)
    except Exception:  # noqa: BLE001
        normalized = str(value).strip().lower()
        return normalized or None
    return encoded if encoded != "" else None


def _metadata_with_thinking_budget(metadata: dict[str, Any], thinking_budget: int | None) -> dict[str, Any]:
    out = dict(metadata)
    if thinking_budget is not None:
        out[_thinking_budget_metadata_key] = thinking_budget
    return out


def _coerce_int(value: Any) -> int | None:
    if value is None or isinstance(value, bool):
        return None
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        integer = int(value)
        if float(integer) == value:
            return integer
        return None
    if isinstance(value, str):
        text = value.strip()
        if text == "":
            return None
        try:
            return int(text)
        except ValueError:
            return None
    return None
