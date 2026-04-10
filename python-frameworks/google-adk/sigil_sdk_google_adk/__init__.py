"""Public exports for Sigil Google ADK callback handlers."""

from __future__ import annotations

import inspect
import json
from typing import Any
from uuid import UUID, uuid4

from sigil_sdk import Client

from .handler import SigilAsyncGoogleAdkHandler, SigilGoogleAdkHandler

try:  # pragma: no cover - imported from google-adk at runtime
    from google.adk.plugins import BasePlugin
except Exception:  # pragma: no cover - lightweight fallback for local unit tests

    class BasePlugin:  # type: ignore[no-redef]
        """Fallback BasePlugin shape used when google-adk isn't installed."""

        async def before_run_callback(self, *, invocation_context: Any) -> Any:
            del invocation_context
            return None

        async def on_event_callback(self, *, invocation_context: Any, event: Any) -> Any:
            del invocation_context, event
            return None

        async def after_run_callback(self, *, invocation_context: Any) -> None:
            del invocation_context
            return None

        async def before_model_callback(self, *, callback_context: Any, llm_request: Any) -> Any:
            del callback_context, llm_request
            return None

        async def after_model_callback(self, *, callback_context: Any, llm_response: Any) -> Any:
            del callback_context, llm_response
            return None

        async def before_tool_callback(self, *, tool: Any, tool_args: dict[str, Any], tool_context: Any) -> Any:
            del tool, tool_args, tool_context
            return None

        async def after_tool_callback(
            self,
            *,
            tool: Any,
            tool_args: dict[str, Any],
            tool_context: Any,
            result: dict[str, Any],
        ) -> Any:
            del tool, tool_args, tool_context, result
            return None


_adk_callback_fields = (
    "before_model_callback",
    "after_model_callback",
    "on_model_error_callback",
    "before_tool_callback",
    "after_tool_callback",
    "on_tool_error_callback",
)


def create_sigil_google_adk_handler(
    *,
    client: Client,
    async_handler: bool = False,
    **handler_kwargs: Any,
) -> SigilGoogleAdkHandler | SigilAsyncGoogleAdkHandler:
    """Create a Google ADK Sigil callback handler for sync or async flows."""
    if async_handler:
        return SigilAsyncGoogleAdkHandler(client=client, **handler_kwargs)
    return SigilGoogleAdkHandler(client=client, **handler_kwargs)


class SigilGoogleAdkCallbacks:
    """Google ADK callback bridge that forwards agent callback fields to Sigil."""

    def __init__(self, sigil_handler: SigilGoogleAdkHandler | SigilAsyncGoogleAdkHandler) -> None:
        self._sigil_handler = sigil_handler
        self._llm_run_stacks: dict[str, list[UUID]] = {}
        self._tool_run_ids: dict[str, UUID] = {}
        self._tool_fallback_run_stacks: dict[str, list[UUID]] = {}

    async def before_model_callback(
        self,
        callback_context: Any,
        llm_request: Any,
        *,
        parent_run_id: UUID | None = None,
    ) -> None:
        invocation_key = self._invocation_key(callback_context)
        run_id = uuid4()
        self._llm_run_stacks.setdefault(invocation_key, []).append(run_id)

        model_name = _first_non_empty(
            _as_string(_read(llm_request, "model")),
            _as_string(_read(_read(llm_request, "config"), "model")),
        )
        invocation_params: dict[str, Any] = {}
        if model_name != "":
            invocation_params["model"] = model_name

        messages = _adk_messages(_read(llm_request, "contents"))
        await _invoke_handler(
            self._sigil_handler,
            "on_chat_model_start",
            _serialized_llm_payload(callback_context, model_name),
            [messages],
            run_id=run_id,
            parent_run_id=parent_run_id,
            invocation_params=invocation_params,
            metadata=_adk_context_metadata(callback_context),
            run_name=_agent_name(callback_context),
        )
        return None

    async def after_model_callback(self, callback_context: Any, llm_response: Any) -> None:
        invocation_key = self._invocation_key(callback_context)
        run_id = self._pop_llm_run_id(invocation_key)
        if run_id is None:
            return None

        await _invoke_handler(
            self._sigil_handler,
            "on_llm_end",
            _adk_llm_end_payload(llm_response),
            run_id=run_id,
        )
        return None

    async def on_model_error_callback(self, callback_context: Any, llm_request: Any, error: Exception) -> None:
        del llm_request
        invocation_key = self._invocation_key(callback_context)
        run_id = self._pop_llm_run_id(invocation_key)
        if run_id is None:
            return None

        await _invoke_handler(self._sigil_handler, "on_llm_error", error, run_id=run_id)
        return None

    async def before_tool_callback(self, tool: Any, args: dict[str, Any], tool_context: Any) -> None:
        run_id = uuid4()
        invocation_key = self._invocation_key(tool_context)
        function_call_id = _as_string(_read(tool_context, "function_call_id"))
        if function_call_id != "":
            self._tool_run_ids[f"{invocation_key}:{function_call_id}"] = run_id
        else:
            self._tool_fallback_run_stacks.setdefault(invocation_key, []).append(run_id)

        llm_stack = self._llm_run_stacks.get(invocation_key, [])
        parent_run_id = llm_stack[-1] if llm_stack else None

        tool_name = _first_non_empty(_as_string(_read(tool, "name")), "framework_tool")
        metadata = _adk_context_metadata(tool_context)
        if function_call_id != "":
            metadata["event_id"] = function_call_id

        await _invoke_handler(
            self._sigil_handler,
            "on_tool_start",
            {"name": tool_name, "description": _as_string(_read(tool, "description"))},
            _json_string(args),
            run_id=run_id,
            parent_run_id=parent_run_id,
            metadata=metadata,
            run_name=tool_name,
            inputs=args,
        )
        return None

    async def after_tool_callback(
        self, tool: Any, args: dict[str, Any], tool_context: Any, result: dict[str, Any]
    ) -> None:
        del tool, args
        invocation_key = self._invocation_key(tool_context)
        function_call_id = _as_string(_read(tool_context, "function_call_id"))
        if function_call_id != "":
            run_id = self._tool_run_ids.pop(f"{invocation_key}:{function_call_id}", None)
        else:
            run_id = self._pop_fallback_tool_run_id(invocation_key)
        if run_id is None:
            return None

        await _invoke_handler(self._sigil_handler, "on_tool_end", result, run_id=run_id)
        return None

    async def on_tool_error_callback(
        self, tool: Any, args: dict[str, Any], tool_context: Any, error: Exception
    ) -> None:
        del tool, args
        invocation_key = self._invocation_key(tool_context)
        function_call_id = _as_string(_read(tool_context, "function_call_id"))
        if function_call_id != "":
            run_id = self._tool_run_ids.pop(f"{invocation_key}:{function_call_id}", None)
        else:
            run_id = self._pop_fallback_tool_run_id(invocation_key)
        if run_id is None:
            return None

        await _invoke_handler(self._sigil_handler, "on_tool_error", error, run_id=run_id)
        return None

    def _invocation_key(self, callback_context: Any) -> str:
        return _invocation_key(callback_context)

    def _pop_llm_run_id(self, invocation_key: str) -> UUID | None:
        stack = self._llm_run_stacks.get(invocation_key, [])
        run_id = stack.pop() if stack else None
        if not stack:
            self._llm_run_stacks.pop(invocation_key, None)
        return run_id

    def _pop_fallback_tool_run_id(self, invocation_key: str) -> UUID | None:
        stack = self._tool_fallback_run_stacks.get(invocation_key, [])
        run_id = stack.pop() if stack else None
        if not stack:
            self._tool_fallback_run_stacks.pop(invocation_key, None)
        return run_id

    def _peek_llm_run_id(self, invocation_key: str) -> UUID | None:
        stack = self._llm_run_stacks.get(invocation_key, [])
        if not stack:
            return None
        return stack[-1]


class SigilGoogleAdkPlugin(BasePlugin):
    """Google ADK BasePlugin-compatible bridge that forwards plugin callbacks to Sigil."""

    name = "sigil_google_adk_plugin"

    def __init__(self, sigil_handler: SigilGoogleAdkHandler | SigilAsyncGoogleAdkHandler) -> None:
        self._sigil_handler = sigil_handler
        self._callbacks = SigilGoogleAdkCallbacks(sigil_handler)
        self._run_ids: dict[str, UUID] = {}
        self._agent_run_stacks: dict[str, list[UUID]] = {}

    async def before_run_callback(self, *, invocation_context: Any) -> None:
        invocation_key = _invocation_key(invocation_context)
        run_id = uuid4()
        self._run_ids[invocation_key] = run_id
        run_name = _agent_name(invocation_context)
        await _invoke_handler(
            self._sigil_handler,
            "on_chain_start",
            {"name": run_name},
            {},
            run_id=run_id,
            parent_run_id=None,
            metadata=_adk_context_metadata(invocation_context),
            run_type="invocation",
            run_name=run_name,
        )
        return None

    async def on_event_callback(self, *, invocation_context: Any, event: Any) -> None:
        invocation_key = _first_non_empty(
            _as_string(_read(event, "invocation_id")),
            _as_string(_read(event, "invocationId")),
            _invocation_key(invocation_context),
        )
        llm_run_id = self._callbacks._peek_llm_run_id(invocation_key)
        if llm_run_id is None:
            return None

        is_partial = _is_true(_read(event, "partial"))
        is_final = _is_true(_read(event, "turn_complete")) or _is_true(_read(event, "turnComplete"))
        if is_final or not is_partial:
            return None

        token = _adk_event_text(event)
        if token == "":
            return None

        await _invoke_handler(self._sigil_handler, "on_llm_new_token", token, run_id=llm_run_id)
        return None

    async def after_run_callback(self, *, invocation_context: Any) -> None:
        invocation_key = _invocation_key(invocation_context)
        run_id = self._run_ids.pop(invocation_key, None)
        if run_id is None:
            return None
        self._callbacks._llm_run_stacks.pop(invocation_key, None)
        self._agent_run_stacks.pop(invocation_key, None)
        await _invoke_handler(self._sigil_handler, "on_chain_end", {"status": "completed"}, run_id=run_id)
        return None

    async def before_agent_callback(self, *, callback_context: Any) -> None:
        invocation_context = _adk_invocation_context(callback_context)
        invocation_key = _invocation_key(invocation_context)
        run_id = uuid4()
        stack = self._agent_run_stacks.setdefault(invocation_key, [])
        parent_run_id = stack[-1] if stack else self._run_ids.get(invocation_key)
        stack.append(run_id)

        run_name = _agent_name(callback_context)
        await _invoke_handler(
            self._sigil_handler,
            "on_chain_start",
            {"name": run_name},
            {},
            run_id=run_id,
            parent_run_id=parent_run_id,
            metadata=_adk_context_metadata(callback_context),
            run_type="agent",
            run_name=run_name,
        )
        return None

    async def after_agent_callback(self, *, callback_context: Any) -> None:
        invocation_key = _invocation_key(_adk_invocation_context(callback_context))
        stack = self._agent_run_stacks.get(invocation_key, [])
        run_id = stack.pop() if stack else None
        if run_id is None:
            return None
        if not stack:
            self._agent_run_stacks.pop(invocation_key, None)

        await _invoke_handler(self._sigil_handler, "on_chain_end", {"status": "completed"}, run_id=run_id)
        return None

    async def before_model_callback(self, *, callback_context: Any, llm_request: Any) -> None:
        invocation_key = _invocation_key(_adk_invocation_context(callback_context))
        agent_stack = self._agent_run_stacks.get(invocation_key, [])
        parent_run_id = agent_stack[-1] if agent_stack else self._run_ids.get(invocation_key)
        await self._callbacks.before_model_callback(callback_context, llm_request, parent_run_id=parent_run_id)
        return None

    async def after_model_callback(self, *, callback_context: Any, llm_response: Any) -> None:
        await self._callbacks.after_model_callback(callback_context, llm_response)
        return None

    async def on_model_error_callback(self, *, callback_context: Any, llm_request: Any, error: Exception) -> None:
        await self._callbacks.on_model_error_callback(callback_context, llm_request, error)
        return None

    async def before_tool_callback(self, *, tool: Any, tool_args: dict[str, Any], tool_context: Any) -> None:
        await self._callbacks.before_tool_callback(tool, tool_args, tool_context)
        return None

    async def after_tool_callback(
        self,
        *,
        tool: Any,
        tool_args: dict[str, Any],
        tool_context: Any,
        result: dict[str, Any],
    ) -> None:
        await self._callbacks.after_tool_callback(tool, tool_args, tool_context, result)
        return None

    async def on_tool_error_callback(
        self,
        *,
        tool: Any,
        tool_args: dict[str, Any],
        tool_context: Any,
        error: Exception,
    ) -> None:
        await self._callbacks.on_tool_error_callback(tool, tool_args, tool_context, error)
        return None


def create_sigil_google_adk_callbacks(
    *,
    client: Client,
    async_handler: bool = False,
    **handler_kwargs: Any,
) -> SigilGoogleAdkCallbacks:
    """Create callback functions compatible with Google ADK agent callback fields."""
    sigil_handler = create_sigil_google_adk_handler(
        client=client,
        async_handler=async_handler,
        **handler_kwargs,
    )
    return SigilGoogleAdkCallbacks(sigil_handler)


def create_sigil_google_adk_plugin(
    *,
    client: Client,
    async_handler: bool = False,
    **handler_kwargs: Any,
) -> SigilGoogleAdkPlugin:
    """Create a Google ADK plugin instance wired to Sigil instrumentation."""
    sigil_handler = create_sigil_google_adk_handler(
        client=client,
        async_handler=async_handler,
        **handler_kwargs,
    )
    return SigilGoogleAdkPlugin(sigil_handler)


def with_sigil_google_adk_callbacks(
    config_or_agent: dict[str, Any] | Any | None,
    *,
    client: Client,
    async_handler: bool = False,
    **handler_kwargs: Any,
) -> dict[str, Any] | Any:
    """Attach Sigil to Google ADK callback fields on either config dicts or agent objects."""
    callbacks = create_sigil_google_adk_callbacks(
        client=client,
        async_handler=async_handler,
        **handler_kwargs,
    )

    if config_or_agent is None or isinstance(config_or_agent, dict):
        merged = dict(config_or_agent or {})
        for field_name in _adk_callback_fields:
            merged[field_name] = _merge_callback_value(
                existing=merged.get(field_name),
                callback=getattr(callbacks, field_name),
                field_name=field_name,
            )
        return merged

    target = config_or_agent
    for field_name in _adk_callback_fields:
        existing = getattr(target, field_name, None)
        setattr(
            target,
            field_name,
            _merge_callback_value(
                existing=existing,
                callback=getattr(callbacks, field_name),
                field_name=field_name,
            ),
        )
    return target


def with_sigil_google_adk_plugins(
    config_or_agent: dict[str, Any] | Any | None,
    *,
    client: Client,
    async_handler: bool = False,
    **handler_kwargs: Any,
) -> dict[str, Any] | Any:
    """Attach Sigil as a Google ADK plugin on either config dicts or agent-like objects."""
    plugin = create_sigil_google_adk_plugin(
        client=client,
        async_handler=async_handler,
        **handler_kwargs,
    )

    if config_or_agent is None or isinstance(config_or_agent, dict):
        merged = dict(config_or_agent or {})
        plugins = _as_list(merged.get("plugins"))
        if not _contains_sigil_plugin(plugins):
            plugins.append(plugin)
        merged["plugins"] = plugins
        return merged

    target = config_or_agent
    plugins = _as_list(getattr(target, "plugins", None))
    if not _contains_sigil_plugin(plugins):
        plugins.append(plugin)
    target.plugins = plugins
    return target


async def _invoke_handler(handler: Any, method_name: str, *args: Any, **kwargs: Any) -> None:
    method = getattr(handler, method_name)
    result = method(*args, **kwargs)
    if inspect.isawaitable(result):
        await result


def _merge_callback_value(existing: Any, callback: Any, *, field_name: str) -> Any:
    if _contains_sigil_callback(existing, field_name):
        return existing
    if existing is None:
        return callback
    if isinstance(existing, list):
        return [*existing, callback]
    if callable(existing):
        return [existing, callback]
    raise TypeError(f"google-adk `{field_name}` must be a callback or list of callbacks.")


def _contains_sigil_callback(existing: Any, field_name: str) -> bool:
    callbacks = existing if isinstance(existing, list) else [existing]
    for callback in callbacks:
        if callback is None:
            continue
        instance = getattr(callback, "__self__", None)
        name = getattr(callback, "__name__", "")
        if isinstance(instance, SigilGoogleAdkCallbacks) and name == field_name:
            return True
    return False


def _contains_sigil_plugin(plugins: list[Any]) -> bool:
    return any(isinstance(plugin, SigilGoogleAdkPlugin) for plugin in plugins)


def _as_list(value: Any) -> list[Any]:
    if isinstance(value, list):
        return list(value)
    if value is None:
        return []
    return [value]


def _invocation_key(callback_context: Any) -> str:
    invocation_context = _adk_invocation_context(callback_context)
    return _first_non_empty(
        _as_string(_read(callback_context, "invocation_id")),
        _as_string(_read(callback_context, "invocationId")),
        _as_string(_read(_read(callback_context, "session"), "id")),
        _as_string(_read(invocation_context, "invocation_id")),
        _as_string(_read(invocation_context, "invocationId")),
        _as_string(_read(_read(invocation_context, "session"), "id")),
        f"sigil-google-adk-invocation:{id(callback_context)}",
    )


def _serialized_llm_payload(callback_context: Any, model_name: str) -> dict[str, Any]:
    serialized: dict[str, Any] = {"name": _agent_name(callback_context)}
    if model_name != "":
        serialized["kwargs"] = {"model": model_name}
    return serialized


def _adk_messages(contents: Any) -> list[dict[str, Any]]:
    if not isinstance(contents, list):
        return []
    messages: list[dict[str, Any]] = []
    for content in contents:
        role = _first_non_empty(_as_string(_read(content, "role")), "user")
        parts = _read(content, "parts")
        text = _content_parts_text(parts)
        if text == "":
            continue
        messages.append({"role": role, "content": text})
    return messages


def _content_parts_text(parts: Any) -> str:
    if not isinstance(parts, list):
        return ""
    text_parts: list[str] = []
    for part in parts:
        text = _first_non_empty(
            _as_string(_read(part, "text")),
            _as_string(_read(_read(part, "inline_data"), "display_name")),
            _as_string(_read(_read(part, "file_data"), "file_uri")),
        )
        if text != "":
            text_parts.append(text)
    return " ".join(text_parts).strip()


def _adk_llm_end_payload(llm_response: Any) -> dict[str, Any]:
    llm_output: dict[str, Any] = {}
    model_name = _first_non_empty(
        _as_string(_read(llm_response, "model_version")),
        _as_string(_read(_read(llm_response, "custom_metadata"), "model")),
    )
    if model_name != "":
        llm_output["model_name"] = model_name

    finish_reason = _as_string(_read(llm_response, "finish_reason"))
    if finish_reason != "":
        llm_output["finish_reason"] = finish_reason

    usage_metadata = _read(llm_response, "usage_metadata")
    token_usage = _adk_token_usage(usage_metadata)
    if token_usage:
        llm_output["token_usage"] = token_usage

    text = _content_parts_text(_read(_read(llm_response, "content"), "parts"))
    payload: dict[str, Any] = {"llm_output": llm_output}
    if text != "":
        payload["generations"] = [[{"text": text}]]
    return payload


def _adk_token_usage(usage_metadata: Any) -> dict[str, int]:
    prompt_tokens = _int_or_none(_read(usage_metadata, "prompt_token_count"))
    completion_tokens = _int_or_none(_read(usage_metadata, "candidates_token_count"))
    total_tokens = _int_or_none(_read(usage_metadata, "total_token_count"))
    token_usage: dict[str, int] = {}
    if prompt_tokens is not None:
        token_usage["prompt_tokens"] = prompt_tokens
    if completion_tokens is not None:
        token_usage["completion_tokens"] = completion_tokens
    if total_tokens is not None:
        token_usage["total_tokens"] = total_tokens
    return token_usage


def _adk_context_metadata(callback_context: Any) -> dict[str, Any]:
    invocation_context = _adk_invocation_context(callback_context)
    metadata: dict[str, Any] = {}
    session_id = _first_non_empty(
        _as_string(_read(callback_context, "session_id")),
        _as_string(_read(_read(callback_context, "session"), "id")),
        _as_string(_read(invocation_context, "session_id")),
        _as_string(_read(_read(invocation_context, "session"), "id")),
    )
    if session_id != "":
        metadata["conversation_id"] = session_id
        metadata["session_id"] = session_id

    thread_id = _first_non_empty(
        _as_string(_read(callback_context, "thread_id")),
        _as_string(_read(_read(_read(callback_context, "session"), "state"), "thread_id")),
        _as_string(_read(invocation_context, "thread_id")),
        _as_string(_read(_read(_read(invocation_context, "session"), "state"), "thread_id")),
    )
    if thread_id != "":
        metadata["thread_id"] = thread_id

    invocation_id = _first_non_empty(
        _as_string(_read(callback_context, "invocation_id")),
        _as_string(_read(callback_context, "invocationId")),
        _as_string(_read(invocation_context, "invocation_id")),
        _as_string(_read(invocation_context, "invocationId")),
    )
    if invocation_id != "":
        metadata["event_id"] = invocation_id
    return metadata


def _agent_name(callback_context: Any) -> str:
    invocation_context = _adk_invocation_context(callback_context)
    return _first_non_empty(
        _as_string(_read(callback_context, "agent_name")),
        _as_string(_read(callback_context, "agentName")),
        _as_string(_read(_read(callback_context, "agent"), "name")),
        _as_string(_read(invocation_context, "agent_name")),
        _as_string(_read(invocation_context, "agentName")),
        _as_string(_read(_read(invocation_context, "agent"), "name")),
        "google_adk_agent",
    )


def _adk_invocation_context(callback_context: Any) -> Any:
    return _first_non_none(
        _read(callback_context, "invocation_context"),
        _read(callback_context, "invocationContext"),
        callback_context,
    )


def _adk_event_text(event: Any) -> str:
    return _first_non_empty(
        _adk_content_text(_read(event, "content")),
        _adk_content_text(_read(event, "partial")),
        _as_string(_read(event, "text")),
        _as_string(_read(event, "delta")),
    )


def _adk_content_text(content: Any) -> str:
    if isinstance(content, str):
        return content.strip()
    parts_text = _content_parts_text(_read(content, "parts"))
    if parts_text != "":
        return parts_text
    return _as_string(_read(content, "text"))


def _json_string(value: Any) -> str:
    try:
        return json.dumps(value, default=str, sort_keys=True)
    except Exception:
        return str(value)


def _read(value: Any, key: str) -> Any:
    if value is None:
        return None
    if isinstance(value, dict):
        return value.get(key)
    return getattr(value, key, None)


def _as_string(value: Any) -> str:
    if isinstance(value, str):
        return value.strip()
    return "" if value is None else str(value).strip()


def _int_or_none(value: Any) -> int | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, int):
        return value
    return None


def _is_true(value: Any) -> bool:
    return value is True


def _first_non_none(*values: Any) -> Any:
    for value in values:
        if value is not None:
            return value
    return None


def _first_non_empty(*values: str) -> str:
    for value in values:
        if value != "":
            return value
    return ""


__all__ = [
    "SigilGoogleAdkHandler",
    "SigilAsyncGoogleAdkHandler",
    "SigilGoogleAdkCallbacks",
    "SigilGoogleAdkPlugin",
    "create_sigil_google_adk_handler",
    "create_sigil_google_adk_callbacks",
    "create_sigil_google_adk_plugin",
    "with_sigil_google_adk_callbacks",
    "with_sigil_google_adk_plugins",
]
