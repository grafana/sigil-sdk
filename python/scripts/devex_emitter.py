"""Continuous synthetic SDK traffic emitter for local devex workflows."""

from __future__ import annotations

from dataclasses import dataclass, field
import os
import random
import signal
import time

from sigil_sdk import (
    AuthConfig,
    Client,
    ClientConfig,
    Generation,
    GenerationExportConfig,
    GenerationStart,
    Message,
    MessageRole,
    ModelRef,
    TokenUsage,
    TraceConfig,
    text_part,
    thinking_part,
    user_text_message,
)
try:
    from sigil_sdk_anthropic import (
        AnthropicMessage,
        AnthropicOptions,
        AnthropicRequest,
        AnthropicResponse,
        AnthropicStreamSummary,
        completion,
        completion_stream,
    )
except ModuleNotFoundError as anthropic_import_error:  # pragma: no cover - exercised by sdk-core tests
    AnthropicMessage = AnthropicOptions = AnthropicRequest = AnthropicResponse = AnthropicStreamSummary = object  # type: ignore[assignment]

    def completion(*_args, **_kwargs):
        raise ModuleNotFoundError(
            "sigil_sdk_anthropic is required to run anthropic devex emitter paths"
        ) from anthropic_import_error

    def completion_stream(*_args, **_kwargs):
        raise ModuleNotFoundError(
            "sigil_sdk_anthropic is required to run anthropic devex emitter paths"
        ) from anthropic_import_error

try:
    from sigil_sdk_gemini import (
        GeminiMessage,
        GeminiOptions,
        GeminiRequest,
        GeminiResponse,
        GeminiStreamSummary,
        completion as gemini_completion,
        completion_stream as gemini_completion_stream,
    )
except ModuleNotFoundError as gemini_import_error:  # pragma: no cover - exercised by sdk-core tests
    GeminiMessage = GeminiOptions = GeminiRequest = GeminiResponse = GeminiStreamSummary = object  # type: ignore[assignment]

    def gemini_completion(*_args, **_kwargs):
        raise ModuleNotFoundError(
            "sigil_sdk_gemini is required to run gemini devex emitter paths"
        ) from gemini_import_error

    def gemini_completion_stream(*_args, **_kwargs):
        raise ModuleNotFoundError(
            "sigil_sdk_gemini is required to run gemini devex emitter paths"
        ) from gemini_import_error

try:
    from sigil_sdk_openai import (
        ChatCompletionsStreamSummary,
        OpenAIOptions,
        ResponsesStreamSummary,
        chat,
        responses,
    )
except ModuleNotFoundError as openai_import_error:  # pragma: no cover - exercised by sdk-core tests
    ChatCompletionsStreamSummary = OpenAIOptions = ResponsesStreamSummary = object  # type: ignore[assignment]

    class _MissingProviderNamespace:
        def __getattr__(self, _name: str):
            raise ModuleNotFoundError(
                "sigil_sdk_openai is required to run openai devex emitter paths"
            ) from openai_import_error

    chat = _MissingProviderNamespace()
    responses = _MissingProviderNamespace()

LANGUAGE = "python"
SOURCES = ("openai", "anthropic", "gemini", "mistral")
PERSONAS = ("planner", "retriever", "executor")
STOP_REQUESTED = False


@dataclass(slots=True)
class RuntimeConfig:
    interval_ms: int
    stream_percent: int
    conversations: int
    rotate_turns: int
    custom_provider: str
    gen_http_endpoint: str
    trace_grpc_endpoint: str
    max_cycles: int


@dataclass(slots=True)
class ThreadState:
    conversation_id: str = ""
    turn: int = 0


@dataclass(slots=True)
class SourceState:
    conversations: int
    cursor: int = 0
    slots: list[ThreadState] = field(default_factory=list)

    def __post_init__(self) -> None:
        if not self.slots:
            self.slots = [ThreadState() for _ in range(self.conversations)]


@dataclass(slots=True)
class EmitContext:
    conversation_id: str
    turn: int
    slot: int
    agent_name: str
    agent_version: str
    tags: dict[str, str]
    metadata: dict[str, object]


def int_from_env(key: str, default: int) -> int:
    raw = os.getenv(key, "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError:
        return default
    if value <= 0:
        return default
    return value


def string_from_env(key: str, default: str) -> str:
    value = os.getenv(key, "").strip()
    return value if value else default


def load_config() -> RuntimeConfig:
    return RuntimeConfig(
        interval_ms=int_from_env("SIGIL_TRAFFIC_INTERVAL_MS", 2000),
        stream_percent=int_from_env("SIGIL_TRAFFIC_STREAM_PERCENT", 30),
        conversations=int_from_env("SIGIL_TRAFFIC_CONVERSATIONS", 3),
        rotate_turns=int_from_env("SIGIL_TRAFFIC_ROTATE_TURNS", 24),
        custom_provider=string_from_env("SIGIL_TRAFFIC_CUSTOM_PROVIDER", "mistral"),
        gen_http_endpoint=string_from_env(
            "SIGIL_TRAFFIC_GEN_HTTP_ENDPOINT", "http://sigil:8080/api/v1/generations:export"
        ),
        trace_grpc_endpoint=string_from_env("SIGIL_TRAFFIC_TRACE_GRPC_ENDPOINT", "alloy:4317"),
        max_cycles=int_from_env("SIGIL_TRAFFIC_MAX_CYCLES", 0),
    )


def source_tag_for(source: str) -> str:
    return "core_custom" if source == "mistral" else "provider_wrapper"


def provider_shape_for(source: str, turn: int = 0) -> str:
    if source == "openai":
        return "openai_chat_completions" if (turn % 2) == 0 else "openai_responses"
    if source == "anthropic":
        return "messages"
    if source == "gemini":
        return "generate_content"
    return "core_generation"


def scenario_for(source: str, turn: int) -> str:
    even = (turn % 2) == 0
    if source == "openai":
        return "openai_plan" if even else "openai_stream"
    if source == "anthropic":
        return "anthropic_reason" if even else "anthropic_delta"
    if source == "gemini":
        return "gemini_structured" if even else "gemini_flow"
    return "custom_mistral_sync" if even else "custom_mistral_stream"


def persona_for_turn(turn: int) -> str:
    return PERSONAS[turn % len(PERSONAS)]


def choose_mode(stream_percent: int) -> str:
    return "STREAM" if random.randint(0, 99) < stream_percent else "SYNC"


def new_conversation_id(source: str, slot: int) -> str:
    return f"devex-{LANGUAGE}-{source}-{slot}-{int(time.time() * 1000)}"


def resolve_thread(state: SourceState, rotate_turns: int, source: str, slot: int) -> ThreadState:
    thread = state.slots[slot]
    if not thread.conversation_id or thread.turn >= rotate_turns:
        thread.conversation_id = new_conversation_id(source, slot)
        thread.turn = 0
    return thread


def build_tags_metadata(source: str, mode: str, turn: int, slot: int) -> tuple[str, dict[str, str], dict[str, object]]:
    persona = persona_for_turn(turn)
    tags = {
        "sigil.devex.language": LANGUAGE,
        "sigil.devex.provider": source,
        "sigil.devex.source": source_tag_for(source),
        "sigil.devex.scenario": scenario_for(source, turn),
        "sigil.devex.mode": mode,
    }
    metadata: dict[str, object] = {
        "turn_index": turn,
        "conversation_slot": slot,
        "agent_persona": persona,
        "emitter": "sdk-traffic",
        "provider_shape": provider_shape_for(source, turn),
    }
    return persona, tags, metadata


def emit_openai_sync(client: Client, context: EmitContext) -> None:
    request = {
        "model": "gpt-5",
        "max_completion_tokens": 320,
        "temperature": 0.2,
        "top_p": 0.9,
        "reasoning": {"effort": "medium", "max_output_tokens": 768},
        "messages": [
            {"role": "system", "content": "Respond with concise action bullets."},
            {"role": "user", "content": f"Draft rollout checklist {context.turn}."},
        ],
    }

    def provider_call(_request):
        return {
            "id": f"py-openai-sync-{context.turn}",
            "model": "gpt-5",
            "object": "chat.completion",
            "created": 0,
            "choices": [
                {
                    "index": 0,
                    "finish_reason": "stop",
                    "message": {
                        "role": "assistant",
                        "content": f"Checklist {context.turn}: verify canary, rotate owner, publish notes.",
                    },
                }
            ],
            "usage": {
                "prompt_tokens": 79 + (context.turn % 9),
                "completion_tokens": 24 + (context.turn % 6),
                "total_tokens": 103 + (context.turn % 11),
            },
        }

    chat.completions.create(
        client,
        request,
        provider_call,
        OpenAIOptions(
            conversation_id=context.conversation_id,
            agent_name=context.agent_name,
            agent_version=context.agent_version,
            tags=context.tags,
            metadata=context.metadata,
        ),
    )


def emit_openai_stream(client: Client, context: EmitContext) -> None:
    request = {
        "model": "gpt-5",
        "stream": True,
        "max_completion_tokens": 220,
        "reasoning": {"effort": "medium", "max_output_tokens": 640},
        "messages": [
            {"role": "system", "content": "Emit short streaming status deltas."},
            {"role": "user", "content": f"Stream ticket state {context.turn}."},
        ],
    }

    def provider_call(_request):
        return ChatCompletionsStreamSummary(
            events=[
                {
                    "id": f"py-openai-stream-{context.turn}",
                    "object": "chat.completion.chunk",
                    "created": 0,
                    "model": "gpt-5",
                    "choices": [{"index": 0, "delta": {"content": "canary passed"}}],
                },
                {
                    "id": f"py-openai-stream-{context.turn}",
                    "object": "chat.completion.chunk",
                    "created": 0,
                    "model": "gpt-5",
                    "choices": [{"index": 0, "delta": {"content": " traffic fully shifted"}, "finish_reason": "stop"}],
                },
            ]
        )

    chat.completions.stream(
        client,
        request,
        provider_call,
        OpenAIOptions(
            conversation_id=context.conversation_id,
            agent_name=context.agent_name,
            agent_version=context.agent_version,
            tags=context.tags,
            metadata=context.metadata,
        ),
    )


def emit_openai_responses_sync(client: Client, context: EmitContext) -> None:
    request = {
        "model": "gpt-5",
        "instructions": "Respond with concise action bullets.",
        "input": [
            {
                "type": "message",
                "role": "user",
                "content": [{"type": "input_text", "text": f"Draft rollout checklist {context.turn}."}],
            }
        ],
        "max_output_tokens": 320,
        "temperature": 0.2,
        "top_p": 0.9,
        "reasoning": {"effort": "medium", "max_output_tokens": 768},
    }

    def provider_call(_request):
        return {
            "id": f"py-openai-responses-sync-{context.turn}",
            "object": "response",
            "model": "gpt-5",
            "output": [
                {
                    "id": f"py-openai-responses-sync-msg-{context.turn}",
                    "type": "message",
                    "role": "assistant",
                    "status": "completed",
                    "content": [
                        {
                            "type": "output_text",
                            "text": f"Checklist {context.turn}: verify canary, rotate owner, publish notes.",
                            "annotations": [],
                        }
                    ],
                }
            ],
            "status": "completed",
            "parallel_tool_calls": False,
            "temperature": 0.2,
            "top_p": 0.9,
            "tools": [],
            "created_at": 0,
            "incomplete_details": None,
            "metadata": {},
            "error": None,
            "usage": {
                "input_tokens": 79 + (context.turn % 9),
                "output_tokens": 24 + (context.turn % 6),
                "total_tokens": 103 + (context.turn % 11),
                "input_tokens_details": {"cached_tokens": 2},
                "output_tokens_details": {"reasoning_tokens": 3},
            },
        }

    responses.create(
        client,
        request,
        provider_call,
        OpenAIOptions(
            conversation_id=context.conversation_id,
            agent_name=context.agent_name,
            agent_version=context.agent_version,
            tags=context.tags,
            metadata=context.metadata,
        ),
    )


def emit_openai_responses_stream(client: Client, context: EmitContext) -> None:
    request = {
        "model": "gpt-5",
        "stream": True,
        "instructions": "Emit short streaming status deltas.",
        "input": f"Stream ticket state {context.turn}.",
        "max_output_tokens": 220,
    }

    def provider_call(_request):
        return ResponsesStreamSummary(
            events=[
                {
                    "type": "response.output_text.delta",
                    "sequence_number": 1,
                    "output_index": 0,
                    "item_id": f"py-openai-responses-stream-msg-{context.turn}",
                    "content_index": 0,
                    "delta": "canary passed",
                },
                {
                    "type": "response.output_text.delta",
                    "sequence_number": 2,
                    "output_index": 0,
                    "item_id": f"py-openai-responses-stream-msg-{context.turn}",
                    "content_index": 0,
                    "delta": " traffic fully shifted",
                },
            ]
        )

    responses.stream(
        client,
        request,
        provider_call,
        OpenAIOptions(
            conversation_id=context.conversation_id,
            agent_name=context.agent_name,
            agent_version=context.agent_version,
            tags=context.tags,
            metadata=context.metadata,
        ),
    )


def emit_anthropic_sync(client: Client, context: EmitContext) -> None:
    request = AnthropicRequest(
        model="claude-sonnet-4-5",
        system_prompt="Summarize with explicit diagnosis and recommendation.",
        messages=[
            AnthropicMessage(role="user", content=f"Summarize reliability drift {context.turn}."),
        ],
    )

    def provider_call(_request: AnthropicRequest) -> AnthropicResponse:
        return AnthropicResponse(
            id=f"py-anthropic-sync-{context.turn}",
            model="claude-sonnet-4-5",
            output_text=(
                f"Diagnosis {context.turn}: latency drift in eu-west. Recommendation: rebalance ingress workers."
            ),
            stop_reason="end_turn",
            usage=TokenUsage(
                input_tokens=74 + (context.turn % 8),
                output_tokens=29 + (context.turn % 5),
                total_tokens=103 + (context.turn % 10),
                cache_read_input_tokens=9,
            ),
            raw={"shape": "anthropic.sync"},
        )

    completion(
        client,
        request,
        provider_call,
        AnthropicOptions(
            conversation_id=context.conversation_id,
            agent_name=context.agent_name,
            agent_version=context.agent_version,
            tags=context.tags,
            metadata=context.metadata,
        ),
    )


def emit_anthropic_stream(client: Client, context: EmitContext) -> None:
    request = AnthropicRequest(
        model="claude-sonnet-4-5",
        system_prompt="Emit short delta narrative for mitigation progress.",
        messages=[
            AnthropicMessage(role="user", content=f"Stream mitigation deltas {context.turn}."),
        ],
    )

    def provider_call(_request: AnthropicRequest) -> AnthropicStreamSummary:
        final_response = AnthropicResponse(
            id=f"py-anthropic-stream-{context.turn}",
            model="claude-sonnet-4-5",
            output_text=f"Change {context.turn}: guard enabled; verification done.",
            stop_reason="end_turn",
            usage=TokenUsage(
                input_tokens=43 + (context.turn % 6),
                output_tokens=16 + (context.turn % 4),
                total_tokens=59 + (context.turn % 7),
            ),
        )
        return AnthropicStreamSummary(
            output_text=f"Change {context.turn}: guard enabled; verification done.",
            final_response=final_response,
            events=[
                {"type": "message_start"},
                {"type": "delta", "text": "guard enabled"},
                {"type": "message_delta", "stop_reason": "end_turn"},
            ],
        )

    completion_stream(
        client,
        request,
        provider_call,
        AnthropicOptions(
            conversation_id=context.conversation_id,
            agent_name=context.agent_name,
            agent_version=context.agent_version,
            tags=context.tags,
            metadata=context.metadata,
        ),
    )


def emit_gemini_sync(client: Client, context: EmitContext) -> None:
    request = GeminiRequest(
        model="gemini-2.5-pro",
        system_prompt="Use structured note style and explicit tool-response framing.",
        messages=[
            GeminiMessage(role="user", content=f"Generate launch summary {context.turn}."),
            GeminiMessage(role="tool", content='{"tool":"release_metrics","status":"green"}', name="release_metrics"),
        ],
    )

    def provider_call(_request: GeminiRequest) -> GeminiResponse:
        return GeminiResponse(
            id=f"py-gemini-sync-{context.turn}",
            model="gemini-2.5-pro-001",
            output_text=f"Launch {context.turn}: all gates green; rollout metrics stable.",
            stop_reason="STOP",
            usage=TokenUsage(
                input_tokens=59 + (context.turn % 7),
                output_tokens=21 + (context.turn % 5),
                total_tokens=80 + (context.turn % 8),
                reasoning_tokens=7,
            ),
            raw={"shape": "gemini.sync"},
        )

    gemini_completion(
        client,
        request,
        provider_call,
        GeminiOptions(
            conversation_id=context.conversation_id,
            agent_name=context.agent_name,
            agent_version=context.agent_version,
            tags=context.tags,
            metadata=context.metadata,
        ),
    )


def emit_gemini_stream(client: Client, context: EmitContext) -> None:
    request = GeminiRequest(
        model="gemini-2.5-pro",
        system_prompt="Emit migration stream with staged checkpoint language.",
        messages=[
            GeminiMessage(role="user", content=f"Stream migration status for wave {context.turn}."),
        ],
    )

    def provider_call(_request: GeminiRequest) -> GeminiStreamSummary:
        final_response = GeminiResponse(
            id=f"py-gemini-stream-{context.turn}",
            model="gemini-2.5-pro-001",
            output_text=f"Wave {context.turn}: shard sync complete; promotion finished.",
            stop_reason="STOP",
            usage=TokenUsage(
                input_tokens=45 + (context.turn % 5),
                output_tokens=17 + (context.turn % 4),
                total_tokens=62 + (context.turn % 7),
            ),
        )
        return GeminiStreamSummary(
            output_text=f"Wave {context.turn}: shard sync complete; promotion finished.",
            final_response=final_response,
            events=[{"response_id": f"py-gemini-stream-{context.turn}", "delta": "shard sync complete"}],
        )

    gemini_completion_stream(
        client,
        request,
        provider_call,
        GeminiOptions(
            conversation_id=context.conversation_id,
            agent_name=context.agent_name,
            agent_version=context.agent_version,
            tags=context.tags,
            metadata=context.metadata,
        ),
    )


def emit_custom_sync(client: Client, cfg: RuntimeConfig, context: EmitContext) -> None:
    recorder = client.start_generation(
        GenerationStart(
            conversation_id=context.conversation_id,
            agent_name=context.agent_name,
            agent_version=context.agent_version,
            model=ModelRef(provider=cfg.custom_provider, name="mistral-large-devex"),
            tags=context.tags,
            metadata=context.metadata,
        )
    )
    try:
        recorder.set_result(
            Generation(
                input=[user_text_message(f"Draft custom checkpoint {context.turn}.")],
                output=[Message(role=MessageRole.ASSISTANT, parts=[text_part(
                    f"Custom provider sync {context.turn}: all guardrails satisfied."
                )])],
                usage=TokenUsage(
                    input_tokens=28 + (context.turn % 6),
                    output_tokens=14 + (context.turn % 5),
                    total_tokens=42 + (context.turn % 7),
                ),
                stop_reason="stop",
            )
        )
    finally:
        recorder.end()
    if recorder.err() is not None:
        raise recorder.err()  # type: ignore[misc]


def emit_custom_stream(client: Client, cfg: RuntimeConfig, context: EmitContext) -> None:
    recorder = client.start_streaming_generation(
        GenerationStart(
            conversation_id=context.conversation_id,
            agent_name=context.agent_name,
            agent_version=context.agent_version,
            model=ModelRef(provider=cfg.custom_provider, name="mistral-large-devex"),
            tags=context.tags,
            metadata=context.metadata,
        )
    )
    try:
        recorder.set_result(
            Generation(
                input=[user_text_message(f"Stream custom remediation summary {context.turn}.")],
                output=[
                    Message(
                        role=MessageRole.ASSISTANT,
                        parts=[
                            thinking_part("assembling synthetic stream segments"),
                            text_part(
                                f"Custom stream {context.turn}: segment A complete; segment B complete."
                            ),
                        ],
                    )
                ],
                usage=TokenUsage(
                    input_tokens=23 + (context.turn % 5),
                    output_tokens=16 + (context.turn % 4),
                    total_tokens=39 + (context.turn % 6),
                ),
                stop_reason="end_turn",
            )
        )
    finally:
        recorder.end()
    if recorder.err() is not None:
        raise recorder.err()  # type: ignore[misc]


def emit_for_source(client: Client, cfg: RuntimeConfig, source: str, mode: str, context: EmitContext) -> None:
    if source == "openai":
        shape = provider_shape_for("openai", context.turn)
        use_responses = shape == "openai_responses"
        if mode == "STREAM":
            if use_responses:
                emit_openai_responses_stream(client, context)
                return
            emit_openai_stream(client, context)
            return
        if use_responses:
            emit_openai_responses_sync(client, context)
            return
        emit_openai_sync(client, context)
        return

    if source == "anthropic":
        if mode == "STREAM":
            emit_anthropic_stream(client, context)
            return
        emit_anthropic_sync(client, context)
        return

    if source == "gemini":
        if mode == "STREAM":
            emit_gemini_stream(client, context)
            return
        emit_gemini_sync(client, context)
        return

    if mode == "STREAM":
        emit_custom_stream(client, cfg, context)
        return
    emit_custom_sync(client, cfg, context)


def _request_stop(_signum: int, _frame: object) -> None:
    global STOP_REQUESTED
    STOP_REQUESTED = True


def run_emitter(config: RuntimeConfig | None = None) -> None:
    cfg = config if config is not None else load_config()

    signal.signal(signal.SIGINT, _request_stop)
    signal.signal(signal.SIGTERM, _request_stop)

    client = Client(
        ClientConfig(
            trace=TraceConfig(
                protocol="grpc",
                endpoint=cfg.trace_grpc_endpoint,
                auth=AuthConfig(mode="none"),
                insecure=True,
            ),
            generation_export=GenerationExportConfig(
                protocol="http",
                endpoint=cfg.gen_http_endpoint,
                auth=AuthConfig(mode="none"),
                insecure=True,
            ),
        )
    )

    source_state = {source: SourceState(cfg.conversations) for source in SOURCES}
    cycles = 0

    print(
        "[python-emitter] started "
        f"interval_ms={cfg.interval_ms} stream_percent={cfg.stream_percent} "
        f"conversations={cfg.conversations} rotate_turns={cfg.rotate_turns} custom_provider={cfg.custom_provider}"
    )

    try:
        while not STOP_REQUESTED:
            for source in SOURCES:
                state = source_state[source]
                slot = state.cursor % cfg.conversations
                state.cursor += 1

                thread = resolve_thread(state, cfg.rotate_turns, source, slot)
                mode = choose_mode(cfg.stream_percent)
                persona, tags, metadata = build_tags_metadata(source, mode, thread.turn, slot)

                context = EmitContext(
                    conversation_id=thread.conversation_id,
                    turn=thread.turn,
                    slot=slot,
                    agent_name=f"devex-{LANGUAGE}-{source}-{persona}",
                    agent_version="devex-1",
                    tags=tags,
                    metadata=metadata,
                )

                emit_for_source(client, cfg, source, mode, context)
                thread.turn += 1

            cycles += 1
            if cfg.max_cycles > 0 and cycles >= cfg.max_cycles:
                break

            jitter_ms = random.randint(-200, 200)
            sleep_ms = max(200, cfg.interval_ms + jitter_ms)
            time.sleep(sleep_ms / 1000)
    finally:
        client.shutdown()


if __name__ == "__main__":
    run_emitter()
