"""LangChain handler lifecycle and provider-mapping tests."""

from __future__ import annotations

import asyncio
from datetime import timedelta
from uuid import uuid4

from sigil_sdk import Client, ClientConfig, GenerationExportConfig
from sigil_sdk.models import ExportGenerationResult, ExportGenerationsResponse
from sigil_sdk_langchain import SigilAsyncLangChainHandler, SigilLangChainHandler


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


def _new_client(exporter: _CapturingExporter) -> Client:
    return Client(
        ClientConfig(
            generation_export=GenerationExportConfig(batch_size=10, flush_interval=timedelta(seconds=60)),
            generation_exporter=exporter,
        )
    )


def test_langchain_sync_lifecycle_sets_framework_tags_and_metadata() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)

    try:
        run_id = uuid4()
        handler = SigilLangChainHandler(
            client=client,
            agent_name="agent-langchain",
            agent_version="v1",
            provider_resolver="auto",
            extra_tags={"env": "test"},
            extra_metadata={"seed": 7},
        )

        handler.on_chat_model_start(
            {"name": "ChatOpenAI"},
            [[{"type": "human", "content": "hello"}]],
            run_id=run_id,
            invocation_params={"model": "gpt-5"},
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
        assert generation.metadata["sigil.framework.run_id"] == str(run_id)
        assert generation.metadata["seed"] == 7
        assert generation.usage.input_tokens == 10
        assert generation.usage.output_tokens == 5
        assert generation.usage.total_tokens == 15
        assert generation.stop_reason == "stop"
        assert generation.output[0].parts[0].text == "world"
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
