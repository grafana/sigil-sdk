"""LangChain callback handlers for Sigil generation recording."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any, Callable
from uuid import UUID

from opentelemetry import trace as otel_trace
from opentelemetry.trace import Span, SpanKind, Status, StatusCode
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
    ToolExecutionStart,
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

_framework_name = "langchain"
_framework_source = "handler"
_framework_language = "python"
_framework_instrumentation_name = "github.com/grafana/sigil/sdks/python-frameworks/langchain"
_span_attr_operation_name = "gen_ai.operation.name"
_span_attr_conversation_id = "gen_ai.conversation.id"
_span_attr_framework_name = "sigil.framework.name"
_span_attr_framework_source = "sigil.framework.source"
_span_attr_framework_language = "sigil.framework.language"
_span_attr_framework_run_id = "sigil.framework.run_id"
_span_attr_framework_thread_id = "sigil.framework.thread_id"
_span_attr_framework_parent_run_id = "sigil.framework.parent_run_id"
_span_attr_framework_component_name = "sigil.framework.component_name"
_span_attr_framework_run_type = "sigil.framework.run_type"
_span_attr_framework_tags = "sigil.framework.tags"
_span_attr_framework_retry_attempt = "sigil.framework.retry_attempt"
_span_attr_framework_langgraph_node = "sigil.framework.langgraph.node"
_span_attr_error_type = "error.type"
_span_attr_error_category = "error.category"


@dataclass(slots=True)
class _RunState:
    recorder: Any
    input_messages: list[Message]
    capture_outputs: bool
    output_chunks: list[str] = field(default_factory=list)


@dataclass(slots=True)
class _ToolRunState:
    recorder: Any
    arguments: Any
    capture_outputs: bool


class _SigilLangChainBase:
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
        self._tool_runs: dict[str, _ToolRunState] = {}
        self._chain_spans: dict[str, Span] = {}
        self._retriever_spans: dict[str, Span] = {}

    def _on_llm_start(
        self,
        *,
        serialized: dict[str, Any] | None,
        prompts: list[str],
        run_id: UUID,
        parent_run_id: UUID | None,
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
        parent_run_key = _normalize_run_key(parent_run_id)
        component_name = _resolve_component_name(serialized, callback_kwargs)
        callback_tags = _normalize_framework_tags(_read(callback_kwargs, "tags"))
        retry_attempt = _resolve_framework_retry_attempt(callback_kwargs, invocation_params, serialized)
        langgraph_node = _resolve_langgraph_node(callback_kwargs, invocation_params, serialized)
        conversation_id = thread_id if thread_id != "" else run_key

        metadata: dict[str, Any] = dict(self._extra_metadata)
        metadata[_span_attr_framework_run_id] = run_key
        metadata[_span_attr_framework_run_type] = "llm"
        if thread_id != "":
            metadata[_span_attr_framework_thread_id] = thread_id
        if parent_run_key != "":
            metadata[_span_attr_framework_parent_run_id] = parent_run_key
        if component_name != "":
            metadata[_span_attr_framework_component_name] = component_name
        if callback_tags:
            metadata[_span_attr_framework_tags] = callback_tags
        if retry_attempt is not None:
            metadata[_span_attr_framework_retry_attempt] = retry_attempt
        if langgraph_node != "":
            metadata[_span_attr_framework_langgraph_node] = langgraph_node

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
        parent_run_id: UUID | None,
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
        parent_run_key = _normalize_run_key(parent_run_id)
        component_name = _resolve_component_name(serialized, callback_kwargs)
        callback_tags = _normalize_framework_tags(_read(callback_kwargs, "tags"))
        retry_attempt = _resolve_framework_retry_attempt(callback_kwargs, invocation_params, serialized)
        langgraph_node = _resolve_langgraph_node(callback_kwargs, invocation_params, serialized)
        conversation_id = thread_id if thread_id != "" else run_key

        metadata: dict[str, Any] = dict(self._extra_metadata)
        metadata[_span_attr_framework_run_id] = run_key
        metadata[_span_attr_framework_run_type] = "chat"
        if thread_id != "":
            metadata[_span_attr_framework_thread_id] = thread_id
        if parent_run_key != "":
            metadata[_span_attr_framework_parent_run_id] = parent_run_key
        if component_name != "":
            metadata[_span_attr_framework_component_name] = component_name
        if callback_tags:
            metadata[_span_attr_framework_tags] = callback_tags
        if retry_attempt is not None:
            metadata[_span_attr_framework_retry_attempt] = retry_attempt
        if langgraph_node != "":
            metadata[_span_attr_framework_langgraph_node] = langgraph_node

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

    def _on_tool_start(
        self,
        *,
        serialized: dict[str, Any] | None,
        input_str: str,
        run_id: UUID,
        parent_run_id: UUID | None,
        callback_kwargs: dict[str, Any] | None = None,
    ) -> None:
        run_key = str(run_id)
        if run_key in self._tool_runs:
            return

        thread_id = _resolve_framework_thread_id(
            serialized=serialized,
            invocation_params=None,
            callback_kwargs=callback_kwargs,
        )
        conversation_id = thread_id if thread_id != "" else run_key
        tool_name = _resolve_tool_name(serialized, callback_kwargs)
        include_content = self._capture_inputs or self._capture_outputs

        recorder = self._client.start_tool_execution(
            ToolExecutionStart(
                tool_name=tool_name,
                tool_description=_resolve_tool_description(serialized),
                conversation_id=conversation_id,
                agent_name=self._agent_name,
                agent_version=self._agent_version,
                include_content=include_content,
            )
        )
        arguments: Any = None
        if self._capture_inputs:
            arguments = _resolve_tool_arguments(input_str, callback_kwargs)
        self._tool_runs[run_key] = _ToolRunState(
            recorder=recorder,
            arguments=arguments,
            capture_outputs=self._capture_outputs,
        )

    def _on_tool_end(self, *, output: Any, run_id: UUID) -> None:
        run_state = self._tool_runs.pop(str(run_id), None)
        if run_state is None:
            return

        try:
            payload: dict[str, Any] = {}
            if run_state.arguments is not None:
                payload["arguments"] = run_state.arguments
            if run_state.capture_outputs:
                payload["result"] = output
            run_state.recorder.set_result(**payload)
        finally:
            run_state.recorder.end()

        recorder_error = run_state.recorder.err()
        if recorder_error is not None:
            raise recorder_error

    def _on_tool_error(self, *, error: BaseException, run_id: UUID) -> None:
        run_state = self._tool_runs.pop(str(run_id), None)
        if run_state is None:
            return

        try:
            run_state.recorder.set_exec_error(Exception(str(error)))
        finally:
            run_state.recorder.end()

        recorder_error = run_state.recorder.err()
        if recorder_error is not None:
            raise recorder_error

    def _on_chain_start(
        self,
        *,
        serialized: dict[str, Any] | None,
        run_id: UUID,
        parent_run_id: UUID | None,
        run_type: str = "chain",
        callback_kwargs: dict[str, Any] | None = None,
    ) -> None:
        run_key = str(run_id)
        if run_key in self._chain_spans:
            return

        span = self._start_framework_span(
            run_key=run_key,
            parent_run_id=parent_run_id,
            serialized=serialized,
            invocation_params=None,
            callback_kwargs=callback_kwargs,
            run_type=run_type,
            operation_name="framework_chain",
            fallback_component="chain",
        )
        self._chain_spans[run_key] = span

    def _on_chain_end(self, *, run_id: UUID) -> None:
        self._end_framework_span(self._chain_spans, run_id=run_id, error=None)

    def _on_chain_error(self, *, error: BaseException, run_id: UUID) -> None:
        self._end_framework_span(self._chain_spans, run_id=run_id, error=error)

    def _on_retriever_start(
        self,
        *,
        serialized: dict[str, Any] | None,
        run_id: UUID,
        parent_run_id: UUID | None,
        callback_kwargs: dict[str, Any] | None = None,
    ) -> None:
        run_key = str(run_id)
        if run_key in self._retriever_spans:
            return

        span = self._start_framework_span(
            run_key=run_key,
            parent_run_id=parent_run_id,
            serialized=serialized,
            invocation_params=None,
            callback_kwargs=callback_kwargs,
            run_type="retriever",
            operation_name="framework_retriever",
            fallback_component="retriever",
        )
        self._retriever_spans[run_key] = span

    def _on_retriever_end(self, *, run_id: UUID) -> None:
        self._end_framework_span(self._retriever_spans, run_id=run_id, error=None)

    def _on_retriever_error(self, *, error: BaseException, run_id: UUID) -> None:
        self._end_framework_span(self._retriever_spans, run_id=run_id, error=error)

    def _start_framework_span(
        self,
        *,
        run_key: str,
        parent_run_id: UUID | None,
        serialized: dict[str, Any] | None,
        invocation_params: dict[str, Any] | None,
        callback_kwargs: dict[str, Any] | None,
        run_type: str,
        operation_name: str,
        fallback_component: str,
    ) -> Span:
        thread_id = _resolve_framework_thread_id(
            serialized=serialized,
            invocation_params=invocation_params,
            callback_kwargs=callback_kwargs,
        )
        parent_run_key = _normalize_run_key(parent_run_id)
        conversation_id = thread_id if thread_id != "" else run_key
        component_name = _resolve_component_name(serialized, callback_kwargs)
        retry_attempt = _resolve_framework_retry_attempt(callback_kwargs, invocation_params, serialized)
        langgraph_node = _resolve_langgraph_node(callback_kwargs, invocation_params, serialized)

        span_name = f"{_framework_name}.{fallback_component}"
        component = component_name if component_name != "" else fallback_component
        if component != "":
            span_name = f"{_framework_name}.{fallback_component} {component}"

        span = self._framework_tracer().start_span(span_name, kind=SpanKind.INTERNAL)
        span.set_attribute(_span_attr_operation_name, operation_name)
        span.set_attribute(_span_attr_framework_name, _framework_name)
        span.set_attribute(_span_attr_framework_source, _framework_source)
        span.set_attribute(_span_attr_framework_language, _framework_language)
        span.set_attribute(_span_attr_framework_run_id, run_key)
        span.set_attribute(_span_attr_framework_run_type, run_type)
        if thread_id != "":
            span.set_attribute(_span_attr_framework_thread_id, thread_id)
        if conversation_id != "":
            span.set_attribute(_span_attr_conversation_id, conversation_id)
        if parent_run_key != "":
            span.set_attribute(_span_attr_framework_parent_run_id, parent_run_key)
        if component_name != "":
            span.set_attribute(_span_attr_framework_component_name, component_name)
        if retry_attempt is not None:
            span.set_attribute(_span_attr_framework_retry_attempt, retry_attempt)
        if langgraph_node != "":
            span.set_attribute(_span_attr_framework_langgraph_node, langgraph_node)

        return span

    def _end_framework_span(
        self,
        span_map: dict[str, Span],
        *,
        run_id: UUID,
        error: BaseException | None,
    ) -> None:
        span = span_map.pop(str(run_id), None)
        if span is None:
            return

        if error is None:
            span.set_status(Status(StatusCode.OK))
            span.end()
            return

        span.set_attribute(_span_attr_error_type, "framework_error")
        span.set_attribute(_span_attr_error_category, "sdk_error")
        span.record_exception(error)
        span.set_status(Status(StatusCode.ERROR, str(error)))
        span.end()

    def _framework_tracer(self):
        tracer = getattr(self._client, "_tracer", None)
        if tracer is not None:
            return tracer
        return otel_trace.get_tracer(_framework_instrumentation_name)


class SigilLangChainHandler(_SigilLangChainBase, BaseCallbackHandler):
    """Sync LangChain callback handler that records Sigil generations."""

    def on_llm_start(
        self,
        serialized: dict[str, Any] | None,
        prompts: list[str],
        *,
        run_id: UUID,
        parent_run_id: UUID | None = None,
        tags: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
        invocation_params: dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> None:
        callback_kwargs = dict(kwargs)
        if tags is not None:
            callback_kwargs["tags"] = tags
        if metadata is not None:
            callback_kwargs["metadata"] = metadata
        self._on_llm_start(
            serialized=serialized,
            prompts=prompts,
            run_id=run_id,
            parent_run_id=parent_run_id,
            invocation_params=invocation_params,
            callback_kwargs=callback_kwargs,
        )

    def on_chat_model_start(
        self,
        serialized: dict[str, Any] | None,
        messages: list[list[Any]],
        *,
        run_id: UUID,
        parent_run_id: UUID | None = None,
        tags: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
        invocation_params: dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> None:
        callback_kwargs = dict(kwargs)
        if tags is not None:
            callback_kwargs["tags"] = tags
        if metadata is not None:
            callback_kwargs["metadata"] = metadata
        self._on_chat_model_start(
            serialized=serialized,
            messages=messages,
            run_id=run_id,
            parent_run_id=parent_run_id,
            invocation_params=invocation_params,
            callback_kwargs=callback_kwargs,
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

    def on_tool_start(
        self,
        serialized: dict[str, Any] | None,
        input_str: str,
        *,
        run_id: UUID,
        parent_run_id: UUID | None = None,
        tags: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> None:
        callback_kwargs = dict(kwargs)
        if tags is not None:
            callback_kwargs["tags"] = tags
        if metadata is not None:
            callback_kwargs["metadata"] = metadata
        self._on_tool_start(
            serialized=serialized,
            input_str=input_str,
            run_id=run_id,
            parent_run_id=parent_run_id,
            callback_kwargs=callback_kwargs,
        )

    def on_tool_end(
        self,
        output: Any,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_tool_end(output=output, run_id=run_id)

    def on_tool_error(
        self,
        error: BaseException,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_tool_error(error=error, run_id=run_id)

    def on_chain_start(
        self,
        serialized: dict[str, Any] | None,
        _inputs: dict[str, Any] | None,
        *,
        run_id: UUID,
        parent_run_id: UUID | None = None,
        tags: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
        run_type: str | None = None,
        **kwargs: Any,
    ) -> None:
        callback_kwargs = dict(kwargs)
        if tags is not None:
            callback_kwargs["tags"] = tags
        if metadata is not None:
            callback_kwargs["metadata"] = metadata
        self._on_chain_start(
            serialized=serialized,
            run_id=run_id,
            parent_run_id=parent_run_id,
            run_type=run_type or "chain",
            callback_kwargs=callback_kwargs,
        )

    def on_chain_end(
        self,
        _outputs: dict[str, Any] | None,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_chain_end(run_id=run_id)

    def on_chain_error(
        self,
        error: BaseException,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_chain_error(error=error, run_id=run_id)

    def on_retriever_start(
        self,
        serialized: dict[str, Any] | None,
        _query: str,
        *,
        run_id: UUID,
        parent_run_id: UUID | None = None,
        tags: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> None:
        callback_kwargs = dict(kwargs)
        if tags is not None:
            callback_kwargs["tags"] = tags
        if metadata is not None:
            callback_kwargs["metadata"] = metadata
        self._on_retriever_start(
            serialized=serialized,
            run_id=run_id,
            parent_run_id=parent_run_id,
            callback_kwargs=callback_kwargs,
        )

    def on_retriever_end(
        self,
        _documents: list[Any],
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_retriever_end(run_id=run_id)

    def on_retriever_error(
        self,
        error: BaseException,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_retriever_error(error=error, run_id=run_id)


class SigilAsyncLangChainHandler(_SigilLangChainBase, AsyncCallbackHandler):
    """Async LangChain callback handler that records Sigil generations."""

    async def on_llm_start(
        self,
        serialized: dict[str, Any] | None,
        prompts: list[str],
        *,
        run_id: UUID,
        parent_run_id: UUID | None = None,
        tags: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
        invocation_params: dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> None:
        callback_kwargs = dict(kwargs)
        if tags is not None:
            callback_kwargs["tags"] = tags
        if metadata is not None:
            callback_kwargs["metadata"] = metadata
        self._on_llm_start(
            serialized=serialized,
            prompts=prompts,
            run_id=run_id,
            parent_run_id=parent_run_id,
            invocation_params=invocation_params,
            callback_kwargs=callback_kwargs,
        )

    async def on_chat_model_start(
        self,
        serialized: dict[str, Any] | None,
        messages: list[list[Any]],
        *,
        run_id: UUID,
        parent_run_id: UUID | None = None,
        tags: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
        invocation_params: dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> None:
        callback_kwargs = dict(kwargs)
        if tags is not None:
            callback_kwargs["tags"] = tags
        if metadata is not None:
            callback_kwargs["metadata"] = metadata
        self._on_chat_model_start(
            serialized=serialized,
            messages=messages,
            run_id=run_id,
            parent_run_id=parent_run_id,
            invocation_params=invocation_params,
            callback_kwargs=callback_kwargs,
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

    async def on_tool_start(
        self,
        serialized: dict[str, Any] | None,
        input_str: str,
        *,
        run_id: UUID,
        parent_run_id: UUID | None = None,
        tags: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> None:
        callback_kwargs = dict(kwargs)
        if tags is not None:
            callback_kwargs["tags"] = tags
        if metadata is not None:
            callback_kwargs["metadata"] = metadata
        self._on_tool_start(
            serialized=serialized,
            input_str=input_str,
            run_id=run_id,
            parent_run_id=parent_run_id,
            callback_kwargs=callback_kwargs,
        )

    async def on_tool_end(
        self,
        output: Any,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_tool_end(output=output, run_id=run_id)

    async def on_tool_error(
        self,
        error: BaseException,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_tool_error(error=error, run_id=run_id)

    async def on_chain_start(
        self,
        serialized: dict[str, Any] | None,
        _inputs: dict[str, Any] | None,
        *,
        run_id: UUID,
        parent_run_id: UUID | None = None,
        tags: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
        run_type: str | None = None,
        **kwargs: Any,
    ) -> None:
        callback_kwargs = dict(kwargs)
        if tags is not None:
            callback_kwargs["tags"] = tags
        if metadata is not None:
            callback_kwargs["metadata"] = metadata
        self._on_chain_start(
            serialized=serialized,
            run_id=run_id,
            parent_run_id=parent_run_id,
            run_type=run_type or "chain",
            callback_kwargs=callback_kwargs,
        )

    async def on_chain_end(
        self,
        _outputs: dict[str, Any] | None,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_chain_end(run_id=run_id)

    async def on_chain_error(
        self,
        error: BaseException,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_chain_error(error=error, run_id=run_id)

    async def on_retriever_start(
        self,
        serialized: dict[str, Any] | None,
        _query: str,
        *,
        run_id: UUID,
        parent_run_id: UUID | None = None,
        tags: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> None:
        callback_kwargs = dict(kwargs)
        if tags is not None:
            callback_kwargs["tags"] = tags
        if metadata is not None:
            callback_kwargs["metadata"] = metadata
        self._on_retriever_start(
            serialized=serialized,
            run_id=run_id,
            parent_run_id=parent_run_id,
            callback_kwargs=callback_kwargs,
        )

    async def on_retriever_end(
        self,
        _documents: list[Any],
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_retriever_end(run_id=run_id)

    async def on_retriever_error(
        self,
        error: BaseException,
        *,
        run_id: UUID,
        **_kwargs: Any,
    ) -> None:
        self._on_retriever_error(error=error, run_id=run_id)


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


def _resolve_component_name(serialized: dict[str, Any] | None, callback_kwargs: dict[str, Any] | None) -> str:
    candidates = (
        _as_str(_read(serialized, "name")),
        _id_path(_read(serialized, "id")),
        _id_path(_read(serialized, "lc_id")),
        _as_str(_read(_read(serialized, "kwargs"), "name")),
        _as_str(_read(callback_kwargs, "component_name")),
        _as_str(_read(callback_kwargs, "run_name")),
    )
    for candidate in candidates:
        if candidate != "":
            return candidate
    return ""


def _resolve_langgraph_node(
    callback_kwargs: dict[str, Any] | None,
    invocation_params: dict[str, Any] | None,
    serialized: dict[str, Any] | None,
) -> str:
    for payload in (callback_kwargs, invocation_params, serialized):
        candidate = _langgraph_node_from_payload(payload)
        if candidate != "":
            return candidate
    return ""


def _langgraph_node_from_payload(payload: Any) -> str:
    candidates = (
        _as_str(_read(payload, "langgraph_node")),
        _as_str(_read(payload, "langgraph_node_name")),
        _as_str(_read(payload, "node_name")),
        _as_str(_read(payload, "node")),
        _as_str(_read(_read(payload, "metadata"), "langgraph_node")),
        _as_str(_read(_read(payload, "metadata"), "langgraph_node_name")),
        _as_str(_read(_read(payload, "configurable"), "langgraph_node")),
        _as_str(_read(_read(payload, "configurable"), "langgraph_node_name")),
        _as_str(_read(_read(_read(payload, "config"), "configurable"), "__pregel_node")),
    )
    for candidate in candidates:
        if candidate != "":
            return candidate
    return ""


def _normalize_framework_tags(value: Any) -> list[str]:
    raw_values = value if isinstance(value, list) else [value]
    output: list[str] = []
    seen: set[str] = set()
    for raw in raw_values:
        if not isinstance(raw, str):
            continue
        trimmed = raw.strip()
        if trimmed == "" or trimmed in seen:
            continue
        seen.add(trimmed)
        output.append(trimmed)
    return output


def _resolve_framework_retry_attempt(*payloads: Any) -> int | None:
    for payload in payloads:
        value = _retry_attempt_from_payload(payload)
        if value is not None:
            return value
    return None


def _retry_attempt_from_payload(payload: Any) -> int | None:
    candidates = (
        _as_optional_int(_read(payload, "retry_attempt")),
        _as_optional_int(_read(payload, "retryAttempt")),
        _as_optional_int(_read(payload, "attempt")),
        _as_optional_int(_read(payload, "retry")),
        _as_optional_int(_read(_read(payload, "metadata"), "retry_attempt")),
        _as_optional_int(_read(_read(payload, "metadata"), "retryAttempt")),
        _as_optional_int(_read(_read(payload, "configurable"), "retry_attempt")),
        _as_optional_int(_read(_read(payload, "configurable"), "retryAttempt")),
    )
    for candidate in candidates:
        if candidate is not None:
            return candidate
    return None


def _normalize_run_key(run_id: UUID | None) -> str:
    if run_id is None:
        return ""
    return str(run_id).strip()


def _resolve_tool_name(serialized: dict[str, Any] | None, callback_kwargs: dict[str, Any] | None) -> str:
    candidates = (
        _as_str(_read(serialized, "name")),
        _as_str(_read(serialized, "tool_name")),
        _as_str(_read(callback_kwargs, "name")),
        _as_str(_read(callback_kwargs, "run_name")),
        "framework_tool",
    )
    for candidate in candidates:
        if candidate != "":
            return candidate
    return "framework_tool"


def _resolve_tool_description(serialized: dict[str, Any] | None) -> str:
    return _as_str(_read(serialized, "description"))


def _resolve_tool_arguments(input_str: str, callback_kwargs: dict[str, Any] | None) -> Any:
    explicit_inputs = _read(callback_kwargs, "inputs")
    if explicit_inputs is not None:
        return explicit_inputs
    return input_str.strip()


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


def _id_path(value: Any) -> str:
    if not isinstance(value, list):
        return ""
    parts: list[str] = []
    for item in value:
        if not isinstance(item, str):
            continue
        trimmed = item.strip()
        if trimmed != "":
            parts.append(trimmed)
    return ".".join(parts)


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


def _as_optional_int(value: Any) -> int | None:
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
        stripped = value.strip()
        if stripped == "":
            return None
        try:
            return int(stripped)
        except ValueError:
            return None
    return None


def _as_bool(value: Any) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        normalized = value.strip().lower()
        return normalized in {"1", "true", "yes", "on"}
    if isinstance(value, int):
        return value != 0
    return False
