"""Anthropic provider helper tests."""

from __future__ import annotations

from datetime import timedelta

import pytest

from sigil_sdk import Client, ClientConfig, GenerationExportConfig, TraceConfig
from sigil_sdk.models import ExportGenerationResult, ExportGenerationsResponse
from sigil_sdk_anthropic import (
    AnthropicMessage,
    AnthropicOptions,
    AnthropicRequest,
    AnthropicResponse,
    AnthropicStreamSummary,
    completion,
    completion_stream,
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


def test_anthropic_sync_wrapper_sets_sync_mode_and_raw_artifacts_default_off() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)
    try:
        completion(
            client,
            AnthropicRequest(
                model="claude-sonnet-4-5",
                messages=[AnthropicMessage(role="user", content="hello")],
            ),
            lambda _request: AnthropicResponse(id="resp-1", output_text="world"),
        )

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.mode.value == "SYNC"
        assert generation.model.provider == "anthropic"
        assert generation.artifacts == []
    finally:
        client.shutdown()


def test_anthropic_stream_wrapper_sets_stream_mode_and_opt_in_artifacts() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)
    try:
        completion_stream(
            client,
            AnthropicRequest(
                model="claude-sonnet-4-5",
                messages=[AnthropicMessage(role="user", content="hello")],
            ),
            lambda _request: AnthropicStreamSummary(output_text="stream-output", events=[{"delta": "x"}]),
            AnthropicOptions(raw_artifacts=True),
        )

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.mode.value == "STREAM"
        assert [artifact.kind.value for artifact in generation.artifacts] == ["request", "provider_event"]
    finally:
        client.shutdown()


def test_anthropic_wrapper_propagates_provider_error_and_sets_call_error() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)

    try:
        with pytest.raises(RuntimeError, match="provider failure"):
            completion(
                client,
                AnthropicRequest(model="claude-sonnet-4-5", messages=[AnthropicMessage(role="user", content="hello")]),
                lambda _request: (_ for _ in ()).throw(RuntimeError("provider failure")),
            )

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.model.provider == "anthropic"
        assert generation.call_error == "provider failure"
    finally:
        client.shutdown()


def test_anthropic_mappers_filter_system_messages_and_support_raw_artifacts() -> None:
    request = AnthropicRequest(
        model="claude-sonnet-4-5",
        max_tokens=512,
        temperature=0.3,
        top_p=0.75,
        tool_choice={"type": "tool", "name": "weather"},
        thinking={"type": "adaptive", "budget_tokens": 2048},
        messages=[
            AnthropicMessage(role="system", content="system"),
            AnthropicMessage(role="user", content="hello"),
            AnthropicMessage(role="tool", content='{"ok":true}', name="tool-weather"),
        ],
    )
    response = AnthropicResponse(id="resp-1", output_text="world", stop_reason="stop")

    mapped_default = from_request_response(request, response)
    assert mapped_default.response_model == "claude-sonnet-4-5"
    assert len(mapped_default.input) == 2
    assert mapped_default.input[0].role.value == "user"
    assert mapped_default.input[1].role.value == "tool"
    assert mapped_default.max_tokens == 512
    assert mapped_default.temperature == 0.3
    assert mapped_default.top_p == 0.75
    assert mapped_default.tool_choice == '{"name":"weather","type":"tool"}'
    assert mapped_default.thinking_enabled is True
    assert mapped_default.metadata["sigil.gen_ai.request.thinking.budget_tokens"] == 2048
    assert mapped_default.artifacts == []

    mapped_with_artifacts = from_request_response(request, response, AnthropicOptions(raw_artifacts=True))
    assert [artifact.kind.value for artifact in mapped_with_artifacts.artifacts] == ["request", "response"]

    stream_mapped = from_stream(
        request,
        AnthropicStreamSummary(output_text="stream-output", events=[{"delta": "x"}]),
        AnthropicOptions(raw_artifacts=True),
    )
    assert stream_mapped.response_model == "claude-sonnet-4-5"
    assert stream_mapped.output[0].parts[0].text == "stream-output"
    assert stream_mapped.max_tokens == 512
    assert stream_mapped.tool_choice == '{"name":"weather","type":"tool"}'
    assert stream_mapped.thinking_enabled is True
    assert stream_mapped.metadata["sigil.gen_ai.request.thinking.budget_tokens"] == 2048
    assert [artifact.kind.value for artifact in stream_mapped.artifacts] == ["request", "provider_event"]


def test_anthropic_mapper_maps_thinking_disabled() -> None:
    request = AnthropicRequest(
        model="claude-sonnet-4-5",
        thinking="disabled",
        messages=[AnthropicMessage(role="user", content="hello")],
    )
    response = AnthropicResponse(id="resp-1", output_text="ok")
    mapped = from_request_response(request, response)

    assert mapped.thinking_enabled is False
