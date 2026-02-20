"""LangGraph callback handlers for Sigil generation recording."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any, Callable
from uuid import UUID

from sigil_sdk import (
    Client,
    Generation,
    GenerationMode,
    GenerationStart,
    Message,
    MessageRole,
    ModelRef,
    Part,
    PartKind,
    TokenUsage,
    user_text_message,
)

try:
    from langchain_core.callbacks import AsyncCallbackHandler, BaseCallbackHandler
except ModuleNotFoundError:  # pragma: no cover - handled by package dependency in normal installs
    class BaseCallbackHandler:  # type: ignore[no-redef]
        """Fallback base class when langchain-core is unavailable."""

    class AsyncCallbackHandler:  # type: ignore[no-redef]
        """Fallback async base class when langchain-core is unavailable."""


ProviderResolver = str | Callable[[str, dict[str, Any] | None, dict[str, Any] | None], str]

_framework_name = "langgraph"
_framework_source = "handler"
_framework_language = "python"


@dataclass(slots=True)
class _RunState:
    recorder: Any
    input_messages: list[Message]
    capture_outputs: bool
    output_chunks: list[str] = field(default_factory=list)


class _SigilLangGraphBase:
    def __init__(
        self,
        *,
        client: Client,
        agent_name: str = "",
        agent_version: str = "",
        provider_resolver: ProviderResolver = "auto",
        provider: str = "",
        capture_inputs: bool = True,
        capture_outputs: bool = True,
        extra_tags: dict[str, str] | None = None,
        extra_metadata: dict[str, Any] | None = None,
    ) -> None:
        self._client = client
        self._agent_name = agent_name
        self._agent_version = agent_version
        self._provider_resolver = provider_resolver
        self._provider = provider
        self._capture_inputs = capture_inputs
        self._capture_outputs = capture_outputs
        self._extra_tags = dict(extra_tags or {})
        self._extra_metadata = dict(extra_metadata or {})
        self._runs: dict[str, _RunState] = {}

    def _on_llm_start(
        self,
        *,
        serialized: dict[str, Any] | None,
        prompts: list[str],
        run_id: UUID,
        invocation_params: dict[str, Any] | None,
        callback_kwargs: dict[str, Any] | None = None,
    ) -> None:
        run_key = str(run_id)
        if run_key in self._runs:
            return

        model_name = _resolve_model_name(serialized, invocation_params)
        provider_name = _resolve_provider(
            explicit_provider=self._provider,
            provider_resolver=self._provider_resolver,
            model_name=model_name,
            serialized=serialized,
            invocation_params=invocation_params,
        )

        mode = GenerationMode.STREAM if _is_streaming(invocation_params) else GenerationMode.SYNC
        input_messages = [user_text_message(prompt) for prompt in prompts if prompt.strip() != ""] if self._capture_inputs else []

        tags = dict(self._extra_tags)
        tags["sigil.framework.name"] = _framework_name
        tags["sigil.framework.source"] = _framework_source
        tags["sigil.framework.language"] = _framework_language

        thread_id = _resolve_framework_thread_id(
            serialized=serialized,
            invocation_params=invocation_params,
            callback_kwargs=callback_kwargs,
        )
        conversation_id = thread_id if thread_id != "" else run_key

        metadata: dict[str, Any] = dict(self._extra_metadata)
        metadata["sigil.framework.run_id"] = run_key
        if thread_id != "":
            metadata["sigil.framework.thread_id"] = thread_id

        start = GenerationStart(
            conversation_id=conversation_id,
            agent_name=self._agent_name,
            agent_version=self._agent_version,
            mode=mode,
            model=ModelRef(provider=provider_name, name=model_name),
            tags=tags,
            metadata=metadata,
        )

        recorder = self._client.start_streaming_generation(start) if mode == GenerationMode.STREAM else self._client.start_generation(start)
        self._runs[run_key] = _RunState(
            recorder=recorder,
            input_messages=input_messages,
            capture_outputs=self._capture_outputs,
        )

    def _on_chat_model_start(
        self,
        *,
        serialized: dict[str, Any] | None,
        messages: list[list[Any]],
        run_id: UUID,
        invocation_params: dict[str, Any] | None,
        callback_kwargs: dict[str, Any] | None = None,
    ) -> None:
        run_key = str(run_id)
        if run_key in self._runs:
            return

        model_name = _resolve_model_name(serialized, invocation_params)
        provider_name = _resolve_provider(
            explicit_provider=self._provider,
            provider_resolver=self._provider_resolver,
            model_name=model_name,
            serialized=serialized,
            invocation_params=invocation_params,
        )

        mode = GenerationMode.STREAM if _is_streaming(invocation_params) else GenerationMode.SYNC
        input_messages = _map_chat_inputs(messages) if self._capture_inputs else []

        tags = dict(self._extra_tags)
        tags["sigil.framework.name"] = _framework_name
        tags["sigil.framework.source"] = _framework_source
        tags["sigil.framework.language"] = _framework_language

        thread_id = _resolve_framework_thread_id(
            serialized=serialized,
            invocation_params=invocation_params,
            callback_kwargs=callback_kwargs,
        )
        conversation_id = thread_id if thread_id != "" else run_key

        metadata: dict[str, Any] = dict(self._extra_metadata)
        metadata["sigil.framework.run_id"] = run_key
        if thread_id != "":
            metadata["sigil.framework.thread_id"] = thread_id

        start = GenerationStart(
            conversation_id=conversation_id,
            agent_name=self._agent_name,
            agent_version=self._agent_version,
            mode=mode,
            model=ModelRef(provider=provider_name, name=model_name),
            tags=tags,
            metadata=metadata,
        )

        recorder = self._client.start_streaming_generation(start) if mode == GenerationMode.STREAM else self._client.start_generation(start)
        self._runs[run_key] = _RunState(
            recorder=recorder,
            input_messages=input_messages,
            capture_outputs=self._capture_outputs,
        )

    def _on_llm_new_token(self, *, token: str, run_id: UUID) -> None:
        run_state = self._runs.get(str(run_id))
        if run_state is None:
            return

        if token.strip() == "":
            return

        if run_state.capture_outputs:
            run_state.output_chunks.append(token)

        run_state.recorder.set_first_token_at(datetime.now(timezone.utc))

    def _on_llm_end(self, *, response: Any, run_id: UUID) -> None:
        run_state = self._runs.pop(str(run_id), None)
        if run_state is None:
            return

        try:
            usage = _map_usage(_read(_read(response, "llm_output"), "token_usage"))
            response_model = _as_str(_read(_read(response, "llm_output"), "model_name"))
            stop_reason = _as_str(_read(_read(response, "llm_output"), "finish_reason"))

            output_messages: list[Message] = []
            if run_state.capture_outputs:
                output_messages = _map_output_messages(response)
                if not output_messages and run_state.output_chunks:
                    output_messages = [
                        Message(
                            role=MessageRole.ASSISTANT,
                            parts=[Part(kind=PartKind.TEXT, text="".join(run_state.output_chunks))],
                        )
                    ]

            run_state.recorder.set_result(
                Generation(
                    input=run_state.input_messages,
                    output=output_messages,
                    usage=usage,
                    response_model=response_model,
                    stop_reason=stop_reason,
                )
            )
        finally:
            run_state.recorder.end()

        recorder_error = run_state.recorder.err()
        if recorder_error is not None:
            raise recorder_error

    def _on_llm_error(self, *, error: BaseException, run_id: UUID) -> None:
        run_state = self._runs.pop(str(run_id), None)
        if run_state is None:
            return

        try:
            run_state.recorder.set_call_error(Exception(str(error)))
            if run_state.capture_outputs and run_state.output_chunks:
                run_state.recorder.set_result(
                    Generation(
                        input=run_state.input_messages,
                        output=[
                            Message(
                                role=MessageRole.ASSISTANT,
                                parts=[Part(kind=PartKind.TEXT, text="".join(run_state.output_chunks))],
                            )
                        ],
                    )
                )
        finally:
            run_state.recorder.end()

        recorder_error = run_state.recorder.err()
        if recorder_error is not None:
            raise recorder_error


class SigilLangGraphHandler(_SigilLangGraphBase, BaseCallbackHandler):
    """Sync LangGraph callback handler that records Sigil generations."""

    def on_llm_start(
        self,
        serialized: dict[str, Any] | None,
        prompts: list[str],
        *,
        run_id: UUID,
        invocation_params: dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> None:
        self._on_llm_start(
            serialized=serialized,
            prompts=prompts,
            run_id=run_id,
            invocation_params=invocation_params,
            callback_kwargs=kwargs,
        )

    def on_chat_model_start(
        self,
        serialized: dict[str, Any] | None,
        messages: list[list[Any]],
        *,
        run_id: UUID,
        invocation_params: dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> None:
        self._on_chat_model_start(
            serialized=serialized,
            messages=messages,
            run_id=run_id,
            invocation_params=invocation_params,
            callback_kwargs=kwargs,
        )

    def on_llm_new_token(
        self,
        token: str,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_llm_new_token(token=token, run_id=run_id)

    def on_llm_end(
        self,
        response: Any,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_llm_end(response=response, run_id=run_id)

    def on_llm_error(
        self,
        error: BaseException,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_llm_error(error=error, run_id=run_id)


class SigilAsyncLangGraphHandler(_SigilLangGraphBase, AsyncCallbackHandler):
    """Async LangGraph callback handler that records Sigil generations."""

    async def on_llm_start(
        self,
        serialized: dict[str, Any] | None,
        prompts: list[str],
        *,
        run_id: UUID,
        invocation_params: dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> None:
        self._on_llm_start(
            serialized=serialized,
            prompts=prompts,
            run_id=run_id,
            invocation_params=invocation_params,
            callback_kwargs=kwargs,
        )

    async def on_chat_model_start(
        self,
        serialized: dict[str, Any] | None,
        messages: list[list[Any]],
        *,
        run_id: UUID,
        invocation_params: dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> None:
        self._on_chat_model_start(
            serialized=serialized,
            messages=messages,
            run_id=run_id,
            invocation_params=invocation_params,
            callback_kwargs=kwargs,
        )

    async def on_llm_new_token(
        self,
        token: str,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_llm_new_token(token=token, run_id=run_id)

    async def on_llm_end(
        self,
        response: Any,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_llm_end(response=response, run_id=run_id)

    async def on_llm_error(
        self,
        error: BaseException,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_llm_error(error=error, run_id=run_id)


def _resolve_provider(
    *,
    explicit_provider: str,
    provider_resolver: ProviderResolver,
    model_name: str,
    serialized: dict[str, Any] | None,
    invocation_params: dict[str, Any] | None,
) -> str:
    explicit = _normalize_provider_name(explicit_provider)
    if explicit != "":
        return explicit

    if callable(provider_resolver):
        resolved = _normalize_provider_name(provider_resolver(model_name, serialized, invocation_params))
        return resolved if resolved != "" else "custom"

    for payload in (invocation_params, serialized):
        provider = _normalize_provider_name(_as_str(_read(payload, "provider")))
        if provider != "":
            return provider
        provider = _normalize_provider_name(_as_str(_read(payload, "ls_provider")))
        if provider != "":
            return provider

    return _infer_provider_from_model_name(model_name)


def _resolve_model_name(serialized: dict[str, Any] | None, invocation_params: dict[str, Any] | None) -> str:
    for payload in (invocation_params, serialized):
        for key in ("model", "model_name", "ls_model_name"):
            value = _as_str(_read(payload, key))
            if value != "":
                return value

        kwargs = _read(payload, "kwargs")
        for key in ("model", "model_name"):
            value = _as_str(_read(kwargs, key))
            if value != "":
                return value

    return "unknown"


def _infer_provider_from_model_name(model_name: str) -> str:
    normalized = model_name.strip().lower()
    if normalized.startswith("gpt-") or normalized.startswith("o1") or normalized.startswith("o3") or normalized.startswith("o4"):
        return "openai"
    if normalized.startswith("claude-"):
        return "anthropic"
    if normalized.startswith("gemini-"):
        return "gemini"
    return "custom"


def _normalize_provider_name(value: str) -> str:
    normalized = value.strip().lower()
    if normalized in {"openai", "anthropic", "gemini"}:
        return normalized
    return "" if normalized == "" else "custom"


def _is_streaming(invocation_params: dict[str, Any] | None) -> bool:
    if invocation_params is None:
        return False
    if _as_bool(_read(invocation_params, "stream")):
        return True
    return _as_bool(_read(invocation_params, "streaming"))


def _resolve_framework_thread_id(
    *,
    serialized: dict[str, Any] | None,
    invocation_params: dict[str, Any] | None,
    callback_kwargs: dict[str, Any] | None,
) -> str:
    for payload in (callback_kwargs, invocation_params, serialized):
        thread_id = _thread_id_from_payload(payload)
        if thread_id != "":
            return thread_id
    return ""


def _thread_id_from_payload(payload: Any) -> str:
    candidates = (
        _as_str(_read(payload, "thread_id")),
        _as_str(_read(payload, "threadId")),
        _as_str(_read(_read(payload, "metadata"), "thread_id")),
        _as_str(_read(_read(payload, "metadata"), "threadId")),
        _as_str(_read(_read(payload, "configurable"), "thread_id")),
        _as_str(_read(_read(payload, "configurable"), "threadId")),
        _as_str(_read(_read(payload, "config"), "thread_id")),
        _as_str(_read(_read(payload, "config"), "threadId")),
        _as_str(_read(_read(_read(payload, "config"), "metadata"), "thread_id")),
        _as_str(_read(_read(_read(payload, "config"), "metadata"), "threadId")),
        _as_str(_read(_read(_read(payload, "config"), "configurable"), "thread_id")),
        _as_str(_read(_read(_read(payload, "config"), "configurable"), "threadId")),
    )
    for candidate in candidates:
        if candidate != "":
            return candidate
    return ""


def _map_chat_inputs(messages: list[list[Any]]) -> list[Message]:
    output: list[Message] = []
    for batch in messages:
        for message in batch:
            text = _extract_message_text(message)
            if text == "":
                continue
            output.append(
                Message(
                    role=_normalize_role(_extract_message_role(message)),
                    parts=[Part(kind=PartKind.TEXT, text=text)],
                )
            )
    return output


def _map_output_messages(response: Any) -> list[Message]:
    texts: list[str] = []
    for candidates in _as_list(_read(response, "generations")):
        for candidate in _as_list(candidates):
            text = _extract_generation_text(candidate)
            if text != "":
                texts.append(text)

    if not texts:
        fallback_text = _as_str(_read(response, "text"))
        if fallback_text != "":
            texts.append(fallback_text)

    if not texts:
        return []

    return [
        Message(
            role=MessageRole.ASSISTANT,
            parts=[Part(kind=PartKind.TEXT, text="\n".join(texts))],
        )
    ]


def _extract_generation_text(candidate: Any) -> str:
    text = _as_str(_read(candidate, "text"))
    if text != "":
        return text

    message = _read(candidate, "message")
    text = _extract_message_text(message)
    if text != "":
        return text

    return ""


def _extract_message_text(message: Any) -> str:
    content = _read(message, "content")
    if isinstance(content, str):
        return content.strip()

    if isinstance(content, list):
        parts: list[str] = []
        for item in content:
            if isinstance(item, str):
                trimmed = item.strip()
                if trimmed != "":
                    parts.append(trimmed)
            else:
                text = _as_str(_read(item, "text"))
                if text != "":
                    parts.append(text)
        return " ".join(parts).strip()

    if isinstance(content, dict):
        text = _as_str(_read(content, "text"))
        if text != "":
            return text

    return ""


def _extract_message_role(message: Any) -> str:
    role = _as_str(_read(message, "role"))
    if role != "":
        return role

    role = _as_str(_read(message, "type"))
    if role != "":
        return role

    return "user"


def _normalize_role(role: str) -> MessageRole:
    normalized = role.strip().lower()
    if normalized in {"assistant", "ai"}:
        return MessageRole.ASSISTANT
    if normalized == "tool":
        return MessageRole.TOOL
    return MessageRole.USER


def _map_usage(raw_usage: Any) -> TokenUsage:
    if raw_usage is None:
        return TokenUsage()

    input_tokens = _as_int(_read(raw_usage, "prompt_tokens"))
    if input_tokens == 0:
        input_tokens = _as_int(_read(raw_usage, "input_tokens"))

    output_tokens = _as_int(_read(raw_usage, "completion_tokens"))
    if output_tokens == 0:
        output_tokens = _as_int(_read(raw_usage, "output_tokens"))

    total_tokens = _as_int(_read(raw_usage, "total_tokens"))
    if total_tokens == 0:
        total_tokens = input_tokens + output_tokens

    return TokenUsage(
        input_tokens=input_tokens,
        output_tokens=output_tokens,
        total_tokens=total_tokens,
    )


def _read(value: Any, key: str) -> Any:
    if value is None:
        return None
    if isinstance(value, dict):
        return value.get(key)
    return getattr(value, key, None)


def _as_list(value: Any) -> list[Any]:
    if isinstance(value, list):
        return value
    return []


def _as_str(value: Any) -> str:
    if isinstance(value, str):
        return value.strip()
    return ""


def _as_int(value: Any) -> int:
    if isinstance(value, bool):
        return int(value)
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        return int(value)
    if isinstance(value, str):
        stripped = value.strip()
        if stripped == "":
            return 0
        try:
            return int(stripped)
        except ValueError:
            return 0
    return 0


def _as_bool(value: Any) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        normalized = value.strip().lower()
        return normalized in {"1", "true", "yes", "on"}
    if isinstance(value, int):
        return value != 0
    return False
