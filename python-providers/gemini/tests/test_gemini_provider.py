"""Gemini provider helper tests."""

from __future__ import annotations

from datetime import timedelta

import pytest

from sigil_sdk import Client, ClientConfig, GenerationExportConfig, TraceConfig
from sigil_sdk.models import ExportGenerationResult, ExportGenerationsResponse
from sigil_sdk_gemini import GeminiOptions, GeminiStreamSummary, models


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


def _contents() -> list[dict]:
    return [
        {
            "role": "user",
            "parts": [
                {"text": "hello"},
            ],
        },
        {
            "role": "user",
            "parts": [
                {
                    "function_response": {
                        "id": "call-weather",
                        "name": "weather",
                        "response": {"ok": True},
                    }
                }
            ],
        },
    ]


def _config() -> dict:
    return {
        "max_output_tokens": 444,
        "temperature": 0.15,
        "top_p": 0.8,
        "tool_config": {
            "function_calling_config": {
                "mode": "ANY",
            }
        },
        "thinking_config": {
            "include_thoughts": True,
            "thinking_budget": 1536,
        },
        "system_instruction": {
            "role": "user",
            "parts": [{"text": "be concise"}],
        },
        "tools": [
            {
                "function_declarations": [
                    {
                        "name": "weather",
                        "description": "Get weather",
                        "parameters_json_schema": {"type": "object"},
                    }
                ]
            }
        ],
    }


def _response() -> dict:
    return {
        "response_id": "resp-1",
        "model_version": "gemini-2.5-pro-001",
        "candidates": [
            {
                "finish_reason": "STOP",
                "content": {
                    "role": "model",
                    "parts": [
                        {
                            "function_call": {
                                "id": "call-weather",
                                "name": "weather",
                                "args": {"city": "Paris"},
                            }
                        },
                        {"text": "world"},
                    ],
                },
            }
        ],
        "usage_metadata": {
            "prompt_token_count": 120,
            "candidates_token_count": 40,
            "total_token_count": 170,
            "cached_content_token_count": 12,
            "thoughts_token_count": 10,
        },
    }


def test_gemini_sync_wrapper_sets_sync_mode_and_raw_artifacts_default_off() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)
    try:
        models.generate_content(
            client,
            "gemini-2.5-pro",
            _contents(),
            _config(),
            lambda _model, _contents, _config: _response(),
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
        models.generate_content_stream(
            client,
            "gemini-2.5-pro",
            _contents(),
            _config(),
            lambda _model, _contents, _config: GeminiStreamSummary(
                output_text="stream-output",
                responses=[_response()],
            ),
            GeminiOptions(raw_artifacts=True),
        )

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.mode.value == "STREAM"
        assert [artifact.kind.value for artifact in generation.artifacts] == ["request", "response", "tools", "provider_event"]
    finally:
        client.shutdown()


def test_gemini_wrapper_propagates_provider_error_and_sets_call_error() -> None:
    exporter = _CapturingExporter()
    client = _new_client(exporter)

    try:
        with pytest.raises(RuntimeError, match="provider failure"):
            models.generate_content(
                client,
                "gemini-2.5-pro",
                _contents(),
                _config(),
                lambda _model, _contents, _config: (_ for _ in ()).throw(RuntimeError("provider failure")),
            )

        client.flush()
        generation = exporter.requests[0].generations[0]
        assert generation.model.provider == "gemini"
        assert generation.call_error == "provider failure"
    finally:
        client.shutdown()


def test_gemini_mappers_use_strict_payloads_and_support_raw_artifacts() -> None:
    model = "gemini-2.5-pro"
    contents = _contents()
    config = _config()
    response = _response()

    mapped_default = models.from_request_response(model, contents, config, response)
    assert mapped_default.response_model == "gemini-2.5-pro-001"
    assert len(mapped_default.input) == 2
    assert mapped_default.input[0].role.value == "user"
    assert mapped_default.input[1].role.value == "tool"
    assert mapped_default.max_tokens == 444
    assert mapped_default.temperature == 0.15
    assert mapped_default.top_p == 0.8
    assert mapped_default.tool_choice == "any"
    assert mapped_default.thinking_enabled is True
    assert mapped_default.metadata["sigil.gen_ai.request.thinking.budget_tokens"] == 1536
    assert mapped_default.usage.total_tokens == 170
    assert mapped_default.usage.cache_read_input_tokens == 12
    assert mapped_default.usage.reasoning_tokens == 10
    assert mapped_default.artifacts == []

    mapped_with_artifacts = models.from_request_response(model, contents, config, response, GeminiOptions(raw_artifacts=True))
    assert [artifact.kind.value for artifact in mapped_with_artifacts.artifacts] == ["request", "response", "tools"]

    stream_mapped = models.from_stream(
        model,
        contents,
        config,
        GeminiStreamSummary(output_text="stream-output", responses=[_response()]),
        GeminiOptions(raw_artifacts=True),
    )
    assert stream_mapped.response_model == "gemini-2.5-pro-001"
    assert stream_mapped.output[0].parts[0].text == "stream-output"
    assert stream_mapped.max_tokens == 444
    assert stream_mapped.tool_choice == "any"
    assert stream_mapped.thinking_enabled is True
    assert stream_mapped.metadata["sigil.gen_ai.request.thinking.budget_tokens"] == 1536
    assert [artifact.kind.value for artifact in stream_mapped.artifacts] == ["request", "response", "tools", "provider_event"]


def test_gemini_mapper_maps_thinking_disabled() -> None:
    mapped = models.from_request_response(
        "gemini-2.5-pro",
        [{"role": "user", "parts": [{"text": "hello"}]}],
        {
            "thinking_config": {
                "include_thoughts": False,
            }
        },
        {
            "response_id": "resp-1",
            "model_version": "gemini-2.5-pro",
            "candidates": [{"content": {"role": "model", "parts": [{"text": "ok"}]}}],
        },
    )

    assert mapped.thinking_enabled is False
