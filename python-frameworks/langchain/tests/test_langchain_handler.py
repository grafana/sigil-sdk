"""LangChain handler lifecycle and provider-mapping tests."""

from __future__ import annotations

import asyncio
from datetime import timedelta
from uuid import uuid4

from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from sigil_sdk import Client, ClientConfig, GenerationExportConfig
from sigil_sdk.client import GenerationRecorder
from sigil_sdk.models import ExportGenerationResult, ExportGenerationsResponse
from sigil_sdk_langchain import (
    SigilAsyncLangChainHandler,
    SigilLangChainHandler,
    create_sigil_langchain_handler,
    with_sigil_langchain_callbacks,
)
from sigil_sdk_langchain.handler import _extract_tool_output


class _CapturingExporter:
    def __init__(self) -> None:
        self.requests = []

    def export_generations(self, request):
        self.requests.append(request)
        return ExportGenerationsResponse(
            results=[
                ExportGenerationResult(generation_id=generation.id, accepted=True)
                for generation in request.generations
            ]
        )

    def shutdown(self) -> None:
        return


def _new_client(exporter: _CapturingExporter, tracer=None) -> Client:
    return Client(
        ClientConfig(
            tracer=tracer,
            generation_export=GenerationExportConfig(batch_size=10, flush_interval=timedelta(seconds=60)),
            generation_exporter=exporter,
        )
    )


def test_langchain_sync_lifecycle_sets_framework_tags_and_metadata() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)

    try:
        run_id = uuid4()
        parent_run_id = uuid4()
        handler = SigilLangChainHandler(
            client=client,
            agent_name="agent-langchain",
            agent_version="v1",
            provider_resolver="auto",
            extra_tags={"env": "test", "sigil.framework.name": "override"},
            extra_metadata={"seed": 7, "sigil.framework.run_id": "override-run", "sigil.framework.thread_id": "override-thread"},
        )

        handler.on_chat_model_start(
            {"name": "ChatOpenAI"},
            [[{"type": "human", "content": "hello"}]],
            run_id=run_id,
            parent_run_id=parent_run_id,
            tags=["prod", "blue"],
            invocation_params={"model": "gpt-5", "retry_attempt": 2},
            metadata={"thread_id": "chain-thread-42"},
        )
        handler.on_llm_end(
            {
                "generations": [[{"text": "world"}]],
                "llm_output": {
                    "model_name": "gpt-5",
                    "finish_reason": "stop",
                    "token_usage": {
                        "prompt_tokens": 10,
                        "completion_tokens": 5,
                        "total_tokens": 15,
                    },
                },
            },
            run_id=run_id,
        )

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.mode.value == "SYNC"
        assert generation.model.provider == "openai"
        assert generation.model.name == "gpt-5"
        assert generation.tags["sigil.framework.name"] == "langchain"
        assert generation.tags["sigil.framework.source"] == "handler"
        assert generation.tags["sigil.framework.language"] == "python"
        assert generation.tags["env"] == "test"
        assert generation.conversation_id == "chain-thread-42"
        assert generation.metadata["sigil.framework.run_id"] == str(run_id)
        assert generation.metadata["sigil.framework.thread_id"] == "chain-thread-42"
        assert generation.metadata["sigil.framework.parent_run_id"] == str(parent_run_id)
        assert generation.metadata["sigil.framework.component_name"] == "ChatOpenAI"
        assert generation.metadata["sigil.framework.run_type"] == "chat"
        assert generation.metadata["sigil.framework.retry_attempt"] == 2
        assert generation.metadata["sigil.framework.tags"] == ["prod", "blue"]
        assert generation.metadata["seed"] == 7
        assert generation.usage.input_tokens == 10
        assert generation.usage.output_tokens == 5
        assert generation.usage.total_tokens == 15
        assert generation.stop_reason == "stop"
        assert generation.output[0].parts[0].text == "world"
    finally:
        client.shutdown()


def test_langchain_sync_lifecycle_extracts_anthropic_style_usage_and_stop_reason() -> None:
    """ChatAnthropic puts token usage under 'usage' (not 'token_usage') and
    stop reason under 'stop_reason' (not 'finish_reason')."""
    exporter = _CapturingExporter()
    client = _new_client(exporter)

    try:
        run_id = uuid4()
        handler = SigilLangChainHandler(
            client=client,
            agent_name="agent-langchain",
            agent_version="v1",
            provider_resolver="auto",
        )

        handler.on_chat_model_start(
            {"name": "ChatAnthropic"},
            [[{"type": "human", "content": "hello"}]],
            run_id=run_id,
            invocation_params={"model": "claude-haiku-4-5-20251001"},
        )
        handler.on_llm_end(
            {
                "generations": [[{"text": "world"}]],
                "llm_output": {
                    "id": "msg_01ABC",
                    "model": "claude-haiku-4-5-20251001",
                    "model_name": "claude-haiku-4-5-20251001",
                    "stop_reason": "end_turn",
                    "usage": {
                        "input_tokens": 42,
                        "output_tokens": 17,
                    },
                },
            },
            run_id=run_id,
        )

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.model.provider == "anthropic"
        assert generation.model.name == "claude-haiku-4-5-20251001"
        assert generation.usage.input_tokens == 42
        assert generation.usage.output_tokens == 17
        assert generation.usage.total_tokens == 59
        assert generation.stop_reason == "end_turn"
    finally:
        client.shutdown()


def test_langchain_stream_lifecycle_uses_stream_mode_and_chunk_fallback() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)

    try:
        run_id = uuid4()
        handler = SigilLangChainHandler(client=client)

        handler.on_llm_start(
            {"kwargs": {"model": "claude-sonnet-4-5"}},
            ["stream this"],
            run_id=run_id,
            invocation_params={"stream": True, "model": "claude-sonnet-4-5"},
        )
        handler.on_llm_new_token("hello", run_id=run_id)
        handler.on_llm_new_token(" world", run_id=run_id)
        handler.on_llm_end({"llm_output": {"model_name": "claude-sonnet-4-5"}}, run_id=run_id)

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.mode.value == "STREAM"
        assert generation.model.provider == "anthropic"
        assert generation.output[0].parts[0].text == "hello world"
    finally:
        client.shutdown()


def test_langchain_stream_records_first_token_timestamp_once() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)
    first_token_calls = 0
    original_set_first_token_at = GenerationRecorder.set_first_token_at

    def _tracking_set_first_token_at(self, first_token_at):
        nonlocal first_token_calls
        first_token_calls += 1
        return original_set_first_token_at(self, first_token_at)

    GenerationRecorder.set_first_token_at = _tracking_set_first_token_at

    try:
        run_id = uuid4()
        handler = SigilLangChainHandler(client=client)

        handler.on_llm_start(
            {"kwargs": {"model": "gpt-5"}},
            ["stream this"],
            run_id=run_id,
            invocation_params={"stream": True, "model": "gpt-5"},
        )
        handler.on_llm_new_token("hello", run_id=run_id)
        handler.on_llm_new_token(" world", run_id=run_id)
        handler.on_llm_end({"llm_output": {"model_name": "gpt-5"}}, run_id=run_id)

        assert first_token_calls == 1
    finally:
        GenerationRecorder.set_first_token_at = original_set_first_token_at
        client.shutdown()


def test_langchain_generation_span_tracks_active_parent_span_and_export_lineage() -> None:
    exporter = _CapturingExporter()
    span_exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(span_exporter))
    tracer = provider.get_tracer("sigil-framework-test")
    client = _new_client(exporter, tracer=tracer)

    try:
        run_id = uuid4()
        with tracer.start_as_current_span("framework.request"):
            handler = SigilLangChainHandler(client=client)
            handler.on_chat_model_start(
                {"name": "ChatOpenAI"},
                [[{"type": "human", "content": "hello"}]],
                run_id=run_id,
                parent_run_id=uuid4(),
                invocation_params={"model": "gpt-5"},
                metadata={"thread_id": "chain-thread-lineage-42"},
            )
            handler.on_llm_end(
                {"generations": [[{"text": "world"}]], "llm_output": {"model_name": "gpt-5", "finish_reason": "stop"}},
                run_id=run_id,
            )

        client.flush()
        generation = exporter.requests[0].generations[0]
        spans = span_exporter.get_finished_spans()
        parent_span = next(span for span in spans if span.name == "framework.request")
        generation_span = next(span for span in spans if span.attributes.get("gen_ai.operation.name") == "generateText")

        assert generation_span.parent is not None
        assert generation_span.parent.span_id == parent_span.context.span_id
        assert generation_span.context.trace_id == parent_span.context.trace_id
        assert generation.trace_id == generation_span.context.trace_id.to_bytes(16, "big").hex()
        assert generation.span_id == generation_span.context.span_id.to_bytes(8, "big").hex()
    finally:
        client.shutdown()
        provider.shutdown()


def test_langchain_provider_resolution_supports_known_models_and_fallback() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)

    try:
        handler = SigilLangChainHandler(client=client)

        run_openai = uuid4()
        handler.on_llm_start({}, ["x"], run_id=run_openai, invocation_params={"model": "gpt-5"})
        handler.on_llm_end({"generations": [[{"text": "ok"}]]}, run_id=run_openai)

        run_anthropic = uuid4()
        handler.on_llm_start({}, ["x"], run_id=run_anthropic, invocation_params={"model": "claude-sonnet-4-5"})
        handler.on_llm_end({"generations": [[{"text": "ok"}]]}, run_id=run_anthropic)

        run_gemini = uuid4()
        handler.on_llm_start({}, ["x"], run_id=run_gemini, invocation_params={"model": "gemini-2.5-pro"})
        handler.on_llm_end({"generations": [[{"text": "ok"}]]}, run_id=run_gemini)

        run_custom = uuid4()
        handler.on_llm_start({}, ["x"], run_id=run_custom, invocation_params={"model": "mistral-large"})
        handler.on_llm_end({"generations": [[{"text": "ok"}]]}, run_id=run_custom)

        client.flush()
        providers = [generation.model.provider for request in exporter.requests for generation in request.generations]
        assert providers == ["openai", "anthropic", "gemini", "custom"]
    finally:
        client.shutdown()


def test_langchain_error_sets_call_error_and_preserves_framework_tags() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)

    try:
        run_id = uuid4()
        handler = SigilLangChainHandler(client=client)

        handler.on_llm_start({}, ["x"], run_id=run_id, invocation_params={"model": "gpt-5"})
        handler.on_llm_error(RuntimeError("provider unavailable"), run_id=run_id)

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert "provider unavailable" in generation.call_error
        assert generation.tags["sigil.framework.name"] == "langchain"
    finally:
        client.shutdown()


def test_langchain_async_handler_records_generation() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)

    async def _run() -> None:
        run_id = uuid4()
        handler = SigilAsyncLangChainHandler(client=client)
        await handler.on_llm_start({}, ["hello"], run_id=run_id, invocation_params={"model": "gpt-5"})
        await handler.on_llm_end({"generations": [[{"text": "world"}]]}, run_id=run_id)

    try:
        asyncio.run(_run())
        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.tags["sigil.framework.name"] == "langchain"
        assert generation.model.provider == "openai"
    finally:
        client.shutdown()


def test_extract_tool_output_unwraps_message_content_and_preserves_plain_values() -> None:
    class _FakeToolMessage:
        def __init__(self, content):
            self.content = content

    payload = {"temp_c": 18}

    assert _extract_tool_output(_FakeToolMessage("tool result text")) == "tool result text"
    assert _extract_tool_output("plain string") == "plain string"
    assert _extract_tool_output(None) is None
    assert _extract_tool_output(payload) is payload


def test_langchain_tool_end_extracts_message_content_before_recording() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)

    class _FakeToolMessage:
        def __init__(self, content):
            self.content = content

    try:
        handler = SigilLangChainHandler(client=client)
        captured: dict[str, object] = {}

        def _capture_tool_end(*, output, run_id) -> None:
            captured["output"] = output
            captured["run_id"] = run_id

        handler._on_tool_end = _capture_tool_end  # type: ignore[method-assign]

        run_id = uuid4()
        handler.on_tool_end(_FakeToolMessage("tool result text"), run_id=run_id)

        assert captured == {"output": "tool result text", "run_id": run_id}
    finally:
        client.shutdown()


def test_langchain_tool_chain_and_retriever_callbacks_emit_spans() -> None:
    exporter = _CapturingExporter()
    span_exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(span_exporter))
    tracer = provider.get_tracer("sigil-test")
    client = _new_client(exporter, tracer=tracer)

    try:
        handler = SigilLangChainHandler(client=client)
        parent_run_id = uuid4()

        tool_run_id = uuid4()
        handler.on_tool_start(
            {"name": "weather", "description": "Get weather"},
            '{"city":"Paris"}',
            run_id=tool_run_id,
            parent_run_id=parent_run_id,
            metadata={"thread_id": "chain-thread-42"},
        )
        handler.on_tool_end({"temp_c": 18}, run_id=tool_run_id)

        chain_run_id = uuid4()
        handler.on_chain_start(
            {"name": "PlanChain"},
            {},
            run_id=chain_run_id,
            parent_run_id=parent_run_id,
            tags=["workflow"],
            metadata={"thread_id": "chain-thread-42"},
            run_type="chain",
        )
        handler.on_chain_end({}, run_id=chain_run_id)

        retriever_run_id = uuid4()
        handler.on_retriever_start(
            {"name": "VectorRetriever"},
            "where is my data",
            run_id=retriever_run_id,
            parent_run_id=parent_run_id,
            metadata={"thread_id": "chain-thread-42"},
        )
        handler.on_retriever_error(RuntimeError("retriever failed"), run_id=retriever_run_id)

        spans = span_exporter.get_finished_spans()
        tool_span = next(span for span in spans if span.attributes.get("gen_ai.operation.name") == "execute_tool")
        chain_span = next(span for span in spans if span.attributes.get("gen_ai.operation.name") == "framework_chain")
        retriever_span = next(span for span in spans if span.attributes.get("gen_ai.operation.name") == "framework_retriever")

        assert tool_span.attributes.get("gen_ai.tool.name") == "weather"
        assert tool_span.attributes.get("gen_ai.conversation.id") == "chain-thread-42"

        assert chain_span.attributes.get("sigil.framework.run_type") == "chain"
        assert chain_span.attributes.get("sigil.framework.component_name") == "PlanChain"
        assert chain_span.attributes.get("sigil.framework.parent_run_id") == str(parent_run_id)
        assert chain_span.status.status_code.name == "OK"

        assert retriever_span.attributes.get("sigil.framework.run_type") == "retriever"
        assert retriever_span.attributes.get("sigil.framework.component_name") == "VectorRetriever"
        assert retriever_span.status.status_code.name == "ERROR"
        assert retriever_span.attributes.get("error.type") == "framework_error"
    finally:
        client.shutdown()
        provider.shutdown()


def test_langchain_attach_helpers_preserve_existing_callbacks() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)
    try:
        created = create_sigil_langchain_handler(client=client)
        assert isinstance(created, SigilLangChainHandler)

        existing = object()
        config = with_sigil_langchain_callbacks(
            {"callbacks": [existing], "retry": 2},
            client=client,
            agent_name="langchain-helper",
        )

        assert config["retry"] == 2
        callbacks = config["callbacks"]
        assert isinstance(callbacks, list)
        assert callbacks[0] is existing
        assert isinstance(callbacks[1], SigilLangChainHandler)
    finally:
        client.shutdown()


def test_langchain_handler_explicitly_has_no_embedding_lifecycle() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)
    try:
        handler = SigilLangChainHandler(client=client)
        assert not hasattr(handler, "on_embedding_start")
        assert not hasattr(handler, "on_embedding_end")
        assert not hasattr(handler, "on_embedding_error")
    finally:
        client.shutdown()


def test_langchain_attach_helpers_do_not_duplicate_existing_sigil_handler() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)
    try:
        existing = SigilLangChainHandler(client=client)
        config = with_sigil_langchain_callbacks({"callbacks": [existing]}, client=client)
        callbacks = config["callbacks"]
        assert isinstance(callbacks, list)
        assert len(callbacks) == 1
        assert callbacks[0] is existing
    finally:
        client.shutdown()
