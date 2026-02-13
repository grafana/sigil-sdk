"""OpenAI provider helper tests."""

from __future__ import annotations

from datetime import timedelta

import pytest

from sigil_sdk import Client, ClientConfig, GenerationExportConfig, TraceConfig
from sigil_sdk.models import ExportGenerationResult, ExportGenerationsResponse
from sigil_sdk_openai import (
    OpenAIChatRequest,
    OpenAIChatResponse,
    OpenAIMessage,
    OpenAIOptions,
    OpenAIStreamSummary,
    chat_completion,
    chat_completion_stream,
    from_request_response,
    from_stream,
)


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


def _new_client(exporter):
    return Client(
        ClientConfig(
            trace=TraceConfig(protocol="http", endpoint="http://localhost:4318/v1/traces"),
            generation_export=GenerationExportConfig(batch_size=10, flush_interval=timedelta(seconds=60)),
            generation_exporter=exporter,
        )
    )


def test_openai_sync_wrapper_sets_sync_mode_and_raw_artifacts_default_off() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)

    try:
        response = chat_completion(
            client,
            OpenAIChatRequest(
                model="gpt-5",
                system_prompt="you are concise",
                messages=[
                    OpenAIMessage(role="system", content="system"),
                    OpenAIMessage(role="user", content="hello"),
                ],
            ),
            lambda _request: OpenAIChatResponse(id="resp-1", output_text="world"),
        )
        assert response.output_text == "world"

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.mode.value == "SYNC"
        assert generation.model.provider == "openai"
        assert generation.artifacts == []
    finally:
        client.shutdown()


def test_openai_stream_wrapper_sets_stream_mode_and_opt_in_artifacts() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)

    try:
        summary = chat_completion_stream(
            client,
            OpenAIChatRequest(
                model="gpt-5",
                messages=[OpenAIMessage(role="user", content="hello")],
            ),
            lambda _request: OpenAIStreamSummary(output_text="stream-output", chunks=[{"delta": "x"}]),
            OpenAIOptions(raw_artifacts=True),
        )
        assert summary.output_text == "stream-output"

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.mode.value == "STREAM"
        assert [artifact.kind.value for artifact in generation.artifacts] == ["request", "provider_event"]
    finally:
        client.shutdown()


def test_openai_wrapper_propagates_provider_error_and_sets_call_error() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)

    try:
        with pytest.raises(RuntimeError, match="provider failure"):
            chat_completion(
                client,
                OpenAIChatRequest(model="gpt-5", messages=[OpenAIMessage(role="user", content="hello")]),
                lambda _request: (_ for _ in ()).throw(RuntimeError("provider failure")),
            )

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.model.provider == "openai"
        assert generation.call_error == "provider failure"
    finally:
        client.shutdown()


def test_openai_mappers_filter_system_messages_and_support_raw_artifacts() -> None:
    request = OpenAIChatRequest(
        model="gpt-5",
        max_completion_tokens=320,
        max_tokens=999,
        temperature=0.2,
        top_p=0.85,
        tool_choice={"type": "function", "name": "weather"},
        reasoning={"effort": "high", "max_output_tokens": 2048},
        messages=[
            OpenAIMessage(role="system", content="system"),
            OpenAIMessage(role="user", content="hello"),
            OpenAIMessage(role="tool", content='{"ok":true}', name="tool-weather"),
        ],
    )
    response = OpenAIChatResponse(id="resp-openai", output_text="world", stop_reason="stop")

    mapped_default = from_request_response(request, response)
    assert mapped_default.response_model == "gpt-5"
    assert len(mapped_default.input) == 2
    assert mapped_default.input[0].role.value == "user"
    assert mapped_default.input[1].role.value == "tool"
    assert mapped_default.max_tokens == 320
    assert mapped_default.temperature == 0.2
    assert mapped_default.top_p == 0.85
    assert mapped_default.tool_choice == '{"name":"weather","type":"function"}'
    assert mapped_default.thinking_enabled is True
    assert mapped_default.metadata["sigil.gen_ai.request.thinking.budget_tokens"] == 2048
    assert mapped_default.artifacts == []

    mapped_with_artifacts = from_request_response(request, response, OpenAIOptions(raw_artifacts=True))
    assert [artifact.kind.value for artifact in mapped_with_artifacts.artifacts] == ["request", "response"]

    stream_mapped = from_stream(
        request,
        OpenAIStreamSummary(output_text="stream-output", chunks=[{"token": "x"}]),
        OpenAIOptions(raw_artifacts=True),
    )
    assert stream_mapped.response_model == "gpt-5"
    assert stream_mapped.output[0].parts[0].text == "stream-output"
    assert stream_mapped.max_tokens == 320
    assert stream_mapped.tool_choice == '{"name":"weather","type":"function"}'
    assert stream_mapped.thinking_enabled is True
    assert stream_mapped.metadata["sigil.gen_ai.request.thinking.budget_tokens"] == 2048
    assert [artifact.kind.value for artifact in stream_mapped.artifacts] == ["request", "provider_event"]


def test_openai_mapper_max_tokens_fallback_and_thinking_unset() -> None:
    request = OpenAIChatRequest(
        model="gpt-5",
        max_tokens=111,
        messages=[OpenAIMessage(role="user", content="hello")],
    )
    response = OpenAIChatResponse(id="resp-openai", output_text="ok")
    mapped = from_request_response(request, response)

    assert mapped.max_tokens == 111
    assert mapped.thinking_enabled is None
