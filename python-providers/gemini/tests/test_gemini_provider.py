"""Gemini provider helper tests."""

from __future__ import annotations

from datetime import timedelta

import pytest

from sigil_sdk import Client, ClientConfig, GenerationExportConfig, TraceConfig
from sigil_sdk.models import ExportGenerationResult, ExportGenerationsResponse
from sigil_sdk_gemini import (
    GeminiMessage,
    GeminiOptions,
    GeminiRequest,
    GeminiResponse,
    GeminiStreamSummary,
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


def test_gemini_sync_wrapper_sets_sync_mode_and_raw_artifacts_default_off() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)
    try:
        completion(
            client,
            GeminiRequest(
                model="gemini-2.5-pro",
                messages=[GeminiMessage(role="user", content="hello")],
            ),
            lambda _request: GeminiResponse(id="resp-1", output_text="world"),
        )

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.mode.value == "SYNC"
        assert generation.model.provider == "gemini"
        assert generation.artifacts == []
    finally:
        client.shutdown()


def test_gemini_stream_wrapper_sets_stream_mode_and_opt_in_artifacts() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)
    try:
        completion_stream(
            client,
            GeminiRequest(
                model="gemini-2.5-pro",
                messages=[GeminiMessage(role="user", content="hello")],
            ),
            lambda _request: GeminiStreamSummary(output_text="stream-output", events=[{"delta": "x"}]),
            GeminiOptions(raw_artifacts=True),
        )

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.mode.value == "STREAM"
        assert [artifact.kind.value for artifact in generation.artifacts] == ["request", "provider_event"]
    finally:
        client.shutdown()


def test_gemini_wrapper_propagates_provider_error_and_sets_call_error() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)

    try:
        with pytest.raises(RuntimeError, match="provider failure"):
            completion(
                client,
                GeminiRequest(model="gemini-2.5-pro", messages=[GeminiMessage(role="user", content="hello")]),
                lambda _request: (_ for _ in ()).throw(RuntimeError("provider failure")),
            )

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.model.provider == "gemini"
        assert generation.call_error == "provider failure"
    finally:
        client.shutdown()


def test_gemini_mappers_filter_system_messages_and_support_raw_artifacts() -> None:
    request = GeminiRequest(
        model="gemini-2.5-pro",
        messages=[
            GeminiMessage(role="system", content="system"),
            GeminiMessage(role="user", content="hello"),
            GeminiMessage(role="tool", content='{"ok":true}', name="tool-weather"),
        ],
    )
    response = GeminiResponse(id="resp-1", output_text="world", stop_reason="STOP")

    mapped_default = from_request_response(request, response)
    assert mapped_default.response_model == "gemini-2.5-pro"
    assert len(mapped_default.input) == 2
    assert mapped_default.input[0].role.value == "user"
    assert mapped_default.input[1].role.value == "tool"
    assert mapped_default.artifacts == []

    mapped_with_artifacts = from_request_response(request, response, GeminiOptions(raw_artifacts=True))
    assert [artifact.kind.value for artifact in mapped_with_artifacts.artifacts] == ["request", "response"]

    stream_mapped = from_stream(
        request,
        GeminiStreamSummary(output_text="stream-output", events=[{"delta": "x"}]),
        GeminiOptions(raw_artifacts=True),
    )
    assert stream_mapped.response_model == "gemini-2.5-pro"
    assert stream_mapped.output[0].parts[0].text == "stream-output"
    assert [artifact.kind.value for artifact in stream_mapped.artifacts] == ["request", "provider_event"]
