"""OpenAI wrapper helpers and payload mappers."""

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


@dataclass(slots=True)
class OpenAIMessage:
    """Simplified OpenAI chat message shape."""

    role: str
    content: str
    name: str = ""


@dataclass(slots=True)
class OpenAIChatRequest:
    """Simplified OpenAI chat request shape."""

    model: str
    messages: list[OpenAIMessage]
    system_prompt: str = ""
    tools: list[ToolDefinition] = field(default_factory=list)


@dataclass(slots=True)
class OpenAIChatResponse:
    """Simplified OpenAI chat response shape."""

    output_text: str
    id: str = ""
    model: str = ""
    usage: TokenUsage = field(default_factory=TokenUsage)
    stop_reason: str = ""
    raw: Any = None


@dataclass(slots=True)
class OpenAIStreamSummary:
    """Simplified OpenAI stream summary shape."""

    output_text: str
    final_response: OpenAIChatResponse | None = None
    chunks: list[Any] = field(default_factory=list)


@dataclass(slots=True)
class OpenAIOptions:
    """Optional Sigil enrichments for OpenAI wrappers."""

    provider_name: str = "openai"
    conversation_id: str = ""
    agent_name: str = ""
    agent_version: str = ""
    tags: dict[str, str] = field(default_factory=dict)
    metadata: dict[str, Any] = field(default_factory=dict)
    raw_artifacts: bool = False


def chat_completion(
    client,
    request: OpenAIChatRequest,
    provider_call: Callable[[OpenAIChatRequest], OpenAIChatResponse],
    options: OpenAIOptions | None = None,
) -> OpenAIChatResponse:
    """Runs a sync OpenAI call and records a `SYNC` generation."""

    opts = options or OpenAIOptions()
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


async def chat_completion_async(
    client,
    request: OpenAIChatRequest,
    provider_call: Callable[[OpenAIChatRequest], Awaitable[OpenAIChatResponse]],
    options: OpenAIOptions | None = None,
) -> OpenAIChatResponse:
    """Runs an async OpenAI call and records a `SYNC` generation."""

    opts = options or OpenAIOptions()
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


def chat_completion_stream(
    client,
    request: OpenAIChatRequest,
    provider_call: Callable[[OpenAIChatRequest], OpenAIStreamSummary],
    options: OpenAIOptions | None = None,
) -> OpenAIStreamSummary:
    """Runs a sync OpenAI stream flow and records a `STREAM` generation."""

    opts = options or OpenAIOptions()
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


async def chat_completion_stream_async(
    client,
    request: OpenAIChatRequest,
    provider_call: Callable[[OpenAIChatRequest], Awaitable[OpenAIStreamSummary]],
    options: OpenAIOptions | None = None,
) -> OpenAIStreamSummary:
    """Runs an async OpenAI stream flow and records a `STREAM` generation."""

    opts = options or OpenAIOptions()
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
    request: OpenAIChatRequest,
    response: OpenAIChatResponse,
    options: OpenAIOptions | None = None,
) -> Generation:
    """Maps OpenAI request/response payloads into a normalized generation."""

    opts = options or OpenAIOptions()
    generation = Generation(
        conversation_id=opts.conversation_id,
        agent_name=opts.agent_name,
        agent_version=opts.agent_version,
        mode=GenerationMode.SYNC,
        model=ModelRef(provider=opts.provider_name, name=request.model),
        response_id=response.id,
        response_model=response.model or request.model,
        system_prompt=request.system_prompt,
        input=_map_input_messages(request.messages),
        output=[_assistant_text_message(response.output_text)],
        tools=copy.deepcopy(request.tools),
        usage=copy.deepcopy(response.usage),
        stop_reason=response.stop_reason,
        tags=dict(opts.tags),
        metadata=dict(opts.metadata),
    )

    if opts.raw_artifacts:
        generation.artifacts = [
            Artifact(
                kind=ArtifactKind.REQUEST,
                name="openai.chat.request",
                content_type="application/json",
                payload=_json_bytes(asdict(request)),
            ),
            Artifact(
                kind=ArtifactKind.RESPONSE,
                name="openai.chat.response",
                content_type="application/json",
                payload=_json_bytes(response.raw if response.raw is not None else asdict(response)),
            ),
        ]
        if request.tools:
            generation.artifacts.append(
                Artifact(
                    kind=ArtifactKind.TOOLS,
                    name="openai.chat.tools",
                    content_type="application/json",
                    payload=_json_bytes([asdict(tool) for tool in request.tools]),
                )
            )

    return generation


def from_stream(
    request: OpenAIChatRequest,
    summary: OpenAIStreamSummary,
    options: OpenAIOptions | None = None,
) -> Generation:
    """Maps OpenAI stream summary into a normalized generation."""

    opts = options or OpenAIOptions()

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
            input=_map_input_messages(request.messages),
            output=[_assistant_text_message(summary.output_text)],
            tools=copy.deepcopy(request.tools),
            tags=dict(opts.tags),
            metadata=dict(opts.metadata),
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
                    name="openai.chat.request",
                    content_type="application/json",
                    payload=_json_bytes(asdict(request)),
                )
            )
        generation.artifacts.append(
            Artifact(
                kind=ArtifactKind.PROVIDER_EVENT,
                name="openai.chat.stream_chunks",
                content_type="application/json",
                payload=_json_bytes(summary.chunks),
            )
        )

    return generation


def _map_input_messages(messages: list[OpenAIMessage]) -> list[Message]:
    mapped: list[Message] = []
    for message in messages:
        if message.role == "system":
            continue
        role = _normalize_role(message.role)
        mapped.append(
            Message(
                role=role,
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


def _start_payload(request: OpenAIChatRequest, options: OpenAIOptions, mode: GenerationMode) -> GenerationStart:
    return GenerationStart(
        conversation_id=options.conversation_id,
        agent_name=options.agent_name,
        agent_version=options.agent_version,
        mode=mode,
        model=ModelRef(provider=options.provider_name, name=request.model),
        system_prompt=request.system_prompt,
        tools=copy.deepcopy(request.tools),
        tags=dict(options.tags),
        metadata=dict(options.metadata),
    )


def _json_bytes(payload: Any) -> bytes:
    return json.dumps(payload, default=str, separators=(",", ":")).encode("utf-8")
