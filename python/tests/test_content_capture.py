"""Tests for ContentCaptureMode: resolution, stripping, context propagation, tool spans."""

from __future__ import annotations

import json
import threading
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, HTTPServer

import pytest
from conftest import CapturingGenerationExporter
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from sigil_sdk import (
    ApiConfig,
    Client,
    ClientConfig,
    ContentCaptureMode,
    ConversationRatingInput,
    ConversationRatingValue,
    Generation,
    GenerationExportConfig,
    GenerationStart,
    Message,
    MessageRole,
    ModelRef,
    Part,
    PartKind,
    TokenUsage,
    ToolCall,
    ToolDefinition,
    ToolExecutionStart,
    ToolResult,
    validate_generation,
)
from sigil_sdk.context import content_capture_mode_from_context, with_content_capture_mode

_METADATA_KEY = "sigil.sdk.content_capture_mode"


def _new_client(exporter: CapturingGenerationExporter, tracer=None, **overrides) -> Client:
    generation_export = GenerationExportConfig(
        batch_size=overrides.get("batch_size", 10),
        flush_interval=overrides.get("flush_interval", timedelta(seconds=60)),
        queue_size=overrides.get("queue_size", 10),
        max_retries=overrides.get("max_retries", 1),
        initial_backoff=overrides.get("initial_backoff", timedelta(milliseconds=1)),
        max_backoff=overrides.get("max_backoff", timedelta(milliseconds=1)),
    )
    return Client(
        ClientConfig(
            tracer=tracer,
            generation_export=generation_export,
            generation_exporter=exporter,
            content_capture=overrides.get("content_capture", ContentCaptureMode.DEFAULT),
            content_capture_resolver=overrides.get("content_capture_resolver", None),
        )
    )


def _seed(content_capture: ContentCaptureMode = ContentCaptureMode.DEFAULT) -> GenerationStart:
    return GenerationStart(
        model=ModelRef(provider="anthropic", name="claude-sonnet-4-5"),
        content_capture=content_capture,
    )


def _full_generation() -> Generation:
    return Generation(
        system_prompt="You are helpful.",
        input=[
            Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="What is the weather?")]),
            Message(
                role=MessageRole.TOOL,
                parts=[
                    Part(
                        kind=PartKind.TOOL_RESULT,
                        tool_result=ToolResult(
                            tool_call_id="call_1",
                            name="weather",
                            content="sunny 18C",
                            content_json=b'{"temp":18}',
                        ),
                    )
                ],
            ),
        ],
        output=[
            Message(
                role=MessageRole.ASSISTANT,
                parts=[
                    Part(kind=PartKind.THINKING, thinking="let me think about weather"),
                    Part(
                        kind=PartKind.TOOL_CALL,
                        tool_call=ToolCall(name="weather", id="call_1", input_json=b'{"city":"Paris"}'),
                    ),
                    Part(kind=PartKind.TEXT, text="It's 18C and sunny in Paris."),
                ],
            )
        ],
        tools=[
            ToolDefinition(
                name="weather", description="Get weather info", type="function", input_schema_json=b'{"type":"object"}'
            ),
        ],
        usage=TokenUsage(input_tokens=120, output_tokens=42),
        stop_reason="end_turn",
        conversation_title="Weather chat",
        call_error="rate limit exceeded",
        artifacts=[],
        metadata={
            "sigil.sdk.name": "sdk-python",
            "call_error": "rate limit exceeded",
            "sigil.conversation.title": "Weather chat",
        },
    )


# ---------------------------------------------------------------------------
# Mode resolution
# ---------------------------------------------------------------------------


class TestContentCaptureModeResolution:
    @pytest.mark.parametrize(
        "client_mode, gen_mode, want_marker",
        [
            (ContentCaptureMode.DEFAULT, ContentCaptureMode.DEFAULT, "no_tool_content"),
            (ContentCaptureMode.METADATA_ONLY, ContentCaptureMode.DEFAULT, "metadata_only"),
            (ContentCaptureMode.FULL, ContentCaptureMode.METADATA_ONLY, "metadata_only"),
            (ContentCaptureMode.METADATA_ONLY, ContentCaptureMode.FULL, "full"),
            (ContentCaptureMode.FULL, ContentCaptureMode.DEFAULT, "full"),
        ],
    )
    def test_generation_content_capture(self, client_mode, gen_mode, want_marker):
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, content_capture=client_mode)
        try:
            rec = client.start_generation(_seed(gen_mode))
            rec.set_result(
                Generation(
                    input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="Hello")])],
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="Hi")])],
                    usage=TokenUsage(input_tokens=10, output_tokens=5),
                )
            )
            rec.end()
            assert rec.err() is None

            gen = rec.last_generation
            assert gen.metadata[_METADATA_KEY] == want_marker
        finally:
            client.shutdown()

    def test_default_resolution_is_no_tool_content(self):
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter)
        try:
            rec = client.start_generation(_seed())
            rec.set_result(
                output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="ok")])],
                usage=TokenUsage(input_tokens=1, output_tokens=1),
            )
            rec.end()
            gen = rec.last_generation
            assert gen.metadata[_METADATA_KEY] == "no_tool_content"
            # Content should NOT be stripped under no_tool_content
            assert gen.output[0].parts[0].text == "ok"
        finally:
            client.shutdown()


# ---------------------------------------------------------------------------
# Content stripping (METADATA_ONLY)
# ---------------------------------------------------------------------------


class TestContentStripping:
    def test_metadata_only_strips_sensitive_content(self):
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, content_capture=ContentCaptureMode.METADATA_ONLY)
        try:
            rec = client.start_generation(_seed())
            rec.set_result(_full_generation())
            rec.end()
            assert rec.err() is None

            gen = rec.last_generation

            # Stripped
            assert gen.system_prompt == ""
            assert gen.input[0].parts[0].text == ""
            assert gen.output[0].parts[0].thinking == ""
            assert gen.output[0].parts[1].tool_call.input_json == b""
            assert gen.output[0].parts[2].text == ""
            assert gen.input[1].parts[0].tool_result.content == ""
            assert gen.input[1].parts[0].tool_result.content_json == b""
            assert gen.tools[0].description == ""
            assert gen.tools[0].input_schema_json == b""
            assert gen.conversation_title == ""
            assert "sigil.conversation.title" not in gen.metadata

            # Preserved
            assert len(gen.input) == 2
            assert len(gen.output) == 1
            assert len(gen.output[0].parts) == 3
            assert gen.input[0].role == MessageRole.USER
            assert gen.output[0].parts[0].kind == PartKind.THINKING
            assert gen.output[0].parts[1].tool_call.name == "weather"
            assert gen.output[0].parts[1].tool_call.id == "call_1"
            assert gen.input[1].parts[0].tool_result.tool_call_id == "call_1"
            assert gen.input[1].parts[0].tool_result.name == "weather"
            assert gen.tools[0].name == "weather"
            assert gen.usage.input_tokens == 120
            assert gen.usage.output_tokens == 42
            assert gen.stop_reason == "end_turn"
            assert gen.metadata["sigil.sdk.name"] == "sdk-python"
        finally:
            client.shutdown()

    def test_metadata_only_replaces_call_error_with_category(self):
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, content_capture=ContentCaptureMode.METADATA_ONLY)
        try:
            rec = client.start_generation(_seed())
            rec.set_call_error(RuntimeError("429 rate limit exceeded"))
            rec.set_result(
                Generation(
                    input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="hello")])],
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="world")])],
                    usage=TokenUsage(input_tokens=1, output_tokens=1),
                )
            )
            rec.end()

            gen = rec.last_generation
            assert gen.call_error == "rate_limit"
            assert "call_error" not in gen.metadata
        finally:
            client.shutdown()

    def test_metadata_only_falls_back_to_sdk_error(self):
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, content_capture=ContentCaptureMode.METADATA_ONLY)
        try:
            rec = client.start_generation(_seed())
            rec.set_call_error(RuntimeError("something went wrong"))
            rec.set_result(
                Generation(
                    input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="hello")])],
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="world")])],
                    usage=TokenUsage(input_tokens=1, output_tokens=1),
                )
            )
            rec.end()

            gen = rec.last_generation
            assert gen.call_error == "sdk_error"
        finally:
            client.shutdown()

    def test_metadata_only_strips_conversation_title_from_span(self):
        span_exporter = InMemorySpanExporter()
        provider = TracerProvider()
        provider.add_span_processor(SimpleSpanProcessor(span_exporter))
        tracer = provider.get_tracer("sigil-test")
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, tracer=tracer, content_capture=ContentCaptureMode.METADATA_ONLY)
        try:
            rec = client.start_generation(
                GenerationStart(
                    model=ModelRef(provider="anthropic", name="claude-sonnet-4-5"),
                    conversation_title="Sensitive topic",
                )
            )
            rec.set_result(
                Generation(
                    input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="Hello")])],
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="Hi")])],
                    usage=TokenUsage(input_tokens=10, output_tokens=5),
                )
            )
            rec.end()

            gen_span = next(s for s in span_exporter.get_finished_spans() if s.name.startswith("generate"))
            assert "sigil.conversation.title" not in gen_span.attributes
        finally:
            client.shutdown()
            provider.shutdown()

    def test_full_mode_preserves_all_content(self):
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, content_capture=ContentCaptureMode.FULL)
        try:
            rec = client.start_generation(_seed())
            rec.set_result(
                Generation(
                    system_prompt="You are helpful.",
                    input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="Hello")])],
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="Hi")])],
                    usage=TokenUsage(input_tokens=10, output_tokens=5),
                )
            )
            rec.end()
            gen = rec.last_generation
            assert gen.metadata[_METADATA_KEY] == "full"
            assert gen.system_prompt == "You are helpful."
            assert gen.input[0].parts[0].text == "Hello"
            assert gen.output[0].parts[0].text == "Hi"
        finally:
            client.shutdown()


# ---------------------------------------------------------------------------
# Per-generation override
# ---------------------------------------------------------------------------


class TestPerGenerationOverride:
    def test_per_generation_full_overrides_client_metadata_only(self):
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, content_capture=ContentCaptureMode.METADATA_ONLY)
        try:
            rec = client.start_generation(_seed(ContentCaptureMode.FULL))
            rec.set_result(
                Generation(
                    input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="Hello")])],
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="Hi")])],
                    usage=TokenUsage(input_tokens=10, output_tokens=5),
                )
            )
            rec.end()
            gen = rec.last_generation
            assert gen.metadata[_METADATA_KEY] == "full"
            assert gen.input[0].parts[0].text == "Hello"
        finally:
            client.shutdown()

    def test_per_generation_metadata_only_overrides_client_full(self):
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, content_capture=ContentCaptureMode.FULL)
        try:
            rec = client.start_generation(_seed(ContentCaptureMode.METADATA_ONLY))
            rec.set_result(
                Generation(
                    input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="Hello")])],
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="Hi")])],
                    usage=TokenUsage(input_tokens=10, output_tokens=5),
                )
            )
            rec.end()
            gen = rec.last_generation
            assert gen.metadata[_METADATA_KEY] == "metadata_only"
            assert gen.input[0].parts[0].text == ""
        finally:
            client.shutdown()


# ---------------------------------------------------------------------------
# Resolver callback
# ---------------------------------------------------------------------------


class TestResolverCallback:
    def test_resolver_metadata_only_overrides_client_full(self):
        exporter = CapturingGenerationExporter()
        client = _new_client(
            exporter,
            content_capture=ContentCaptureMode.FULL,
            content_capture_resolver=lambda _meta: ContentCaptureMode.METADATA_ONLY,
        )
        try:
            rec = client.start_generation(_seed())
            rec.set_result(
                Generation(
                    input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="hello")])],
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="world")])],
                    usage=TokenUsage(input_tokens=10, output_tokens=5),
                )
            )
            rec.end()
            gen = rec.last_generation
            assert gen.metadata[_METADATA_KEY] == "metadata_only"
            assert gen.input[0].parts[0].text == ""
        finally:
            client.shutdown()

    def test_per_generation_overrides_resolver(self):
        exporter = CapturingGenerationExporter()
        client = _new_client(
            exporter,
            content_capture=ContentCaptureMode.DEFAULT,
            content_capture_resolver=lambda _meta: ContentCaptureMode.METADATA_ONLY,
        )
        try:
            rec = client.start_generation(_seed(ContentCaptureMode.FULL))
            rec.set_result(
                Generation(
                    input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="hello")])],
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="world")])],
                    usage=TokenUsage(input_tokens=10, output_tokens=5),
                )
            )
            rec.end()
            gen = rec.last_generation
            assert gen.metadata[_METADATA_KEY] == "full"
            assert gen.input[0].parts[0].text == "hello"
        finally:
            client.shutdown()

    def test_resolver_default_defers_to_client(self):
        exporter = CapturingGenerationExporter()
        client = _new_client(
            exporter,
            content_capture=ContentCaptureMode.METADATA_ONLY,
            content_capture_resolver=lambda _meta: ContentCaptureMode.DEFAULT,
        )
        try:
            rec = client.start_generation(_seed())
            rec.set_result(
                Generation(
                    input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="hello")])],
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="world")])],
                    usage=TokenUsage(input_tokens=10, output_tokens=5),
                )
            )
            rec.end()
            gen = rec.last_generation
            assert gen.metadata[_METADATA_KEY] == "metadata_only"
            assert gen.input[0].parts[0].text == ""
        finally:
            client.shutdown()

    def test_resolver_exception_fails_closed_to_metadata_only(self):
        def bad_resolver(_meta):
            raise RuntimeError("resolver bug")

        exporter = CapturingGenerationExporter()
        client = _new_client(
            exporter,
            content_capture=ContentCaptureMode.FULL,
            content_capture_resolver=bad_resolver,
        )
        try:
            rec = client.start_generation(_seed())
            rec.set_result(
                Generation(
                    input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="hello")])],
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="world")])],
                    usage=TokenUsage(input_tokens=10, output_tokens=5),
                )
            )
            rec.end()
            gen = rec.last_generation
            assert gen.metadata[_METADATA_KEY] == "metadata_only"
            assert gen.input[0].parts[0].text == ""
        finally:
            client.shutdown()

    def test_resolver_returning_wrong_type_fails_closed(self):
        """A resolver returning a plain string instead of ContentCaptureMode should fail closed."""
        exporter = CapturingGenerationExporter()
        client = _new_client(
            exporter,
            content_capture=ContentCaptureMode.FULL,
            content_capture_resolver=lambda _meta: "metadata_only",
        )
        try:
            rec = client.start_generation(_seed())
            rec.set_result(
                Generation(
                    input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="hello")])],
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="world")])],
                    usage=TokenUsage(input_tokens=10, output_tokens=5),
                )
            )
            rec.end()
            gen = rec.last_generation
            assert gen.metadata[_METADATA_KEY] == "metadata_only"
            assert gen.input[0].parts[0].text == ""
        finally:
            client.shutdown()

    def test_resolver_full_overrides_client_metadata_only(self):
        exporter = CapturingGenerationExporter()
        client = _new_client(
            exporter,
            content_capture=ContentCaptureMode.METADATA_ONLY,
            content_capture_resolver=lambda _meta: ContentCaptureMode.FULL,
        )
        try:
            rec = client.start_generation(_seed())
            rec.set_result(
                Generation(
                    input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="hello")])],
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="world")])],
                    usage=TokenUsage(input_tokens=10, output_tokens=5),
                )
            )
            rec.end()
            gen = rec.last_generation
            assert gen.metadata[_METADATA_KEY] == "full"
            assert gen.input[0].parts[0].text == "hello"
        finally:
            client.shutdown()


# ---------------------------------------------------------------------------
# Tool span content capture
# ---------------------------------------------------------------------------


class TestToolContentCapture:
    def _make_tool_client(self, content_capture=ContentCaptureMode.DEFAULT, **kw):
        span_exporter = InMemorySpanExporter()
        provider = TracerProvider()
        provider.add_span_processor(SimpleSpanProcessor(span_exporter))
        tracer = provider.get_tracer("sigil-test")
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, tracer=tracer, content_capture=content_capture, **kw)
        return client, span_exporter, provider

    def _get_tool_span(self, span_exporter):
        for span in span_exporter.get_finished_spans():
            if span.name.startswith("execute_tool"):
                return span
        raise AssertionError("tool span not found")

    def test_client_full_includes_content(self):
        client, span_exporter, provider = self._make_tool_client(ContentCaptureMode.FULL)
        try:
            with client.start_tool_execution(ToolExecutionStart(tool_name="test_tool", include_content=False)) as rec:
                rec.set_result(arguments="args", result="result")

            span = self._get_tool_span(span_exporter)
            assert span.attributes.get("gen_ai.tool.call.arguments") is not None
        finally:
            client.shutdown()
            provider.shutdown()

    def test_client_metadata_only_suppresses_content(self):
        client, span_exporter, provider = self._make_tool_client(ContentCaptureMode.METADATA_ONLY)
        try:
            with client.start_tool_execution(
                ToolExecutionStart(tool_name="test_tool", include_content=True, conversation_title="Sensitive topic")
            ) as rec:
                rec.set_result(arguments="args", result="result")

            span = self._get_tool_span(span_exporter)
            assert span.attributes.get("gen_ai.tool.call.arguments") is None
            assert "sigil.conversation.title" not in span.attributes
        finally:
            client.shutdown()
            provider.shutdown()

    def test_client_default_legacy_false_suppresses(self):
        client, span_exporter, provider = self._make_tool_client(ContentCaptureMode.DEFAULT)
        try:
            with client.start_tool_execution(ToolExecutionStart(tool_name="test_tool", include_content=False)) as rec:
                rec.set_result(arguments="args", result="result")

            span = self._get_tool_span(span_exporter)
            assert span.attributes.get("gen_ai.tool.call.arguments") is None
        finally:
            client.shutdown()
            provider.shutdown()

    def test_client_default_legacy_true_includes(self):
        client, span_exporter, provider = self._make_tool_client(ContentCaptureMode.DEFAULT)
        try:
            with client.start_tool_execution(ToolExecutionStart(tool_name="test_tool", include_content=True)) as rec:
                rec.set_result(arguments="args", result="result")

            span = self._get_tool_span(span_exporter)
            assert span.attributes.get("gen_ai.tool.call.arguments") is not None
        finally:
            client.shutdown()
            provider.shutdown()

    def test_per_tool_full_overrides_client_metadata_only(self):
        client, span_exporter, provider = self._make_tool_client(ContentCaptureMode.METADATA_ONLY)
        try:
            with client.start_tool_execution(
                ToolExecutionStart(
                    tool_name="test_tool",
                    content_capture=ContentCaptureMode.FULL,
                    include_content=True,
                )
            ) as rec:
                rec.set_result(arguments="args", result="result")

            span = self._get_tool_span(span_exporter)
            assert span.attributes.get("gen_ai.tool.call.arguments") is not None
        finally:
            client.shutdown()
            provider.shutdown()

    def test_per_tool_metadata_only_overrides_client_full(self):
        client, span_exporter, provider = self._make_tool_client(ContentCaptureMode.FULL)
        try:
            with client.start_tool_execution(
                ToolExecutionStart(
                    tool_name="test_tool",
                    content_capture=ContentCaptureMode.METADATA_ONLY,
                    include_content=True,
                )
            ) as rec:
                rec.set_result(arguments="args", result="result")

            span = self._get_tool_span(span_exporter)
            assert span.attributes.get("gen_ai.tool.call.arguments") is None
        finally:
            client.shutdown()
            provider.shutdown()

    def test_include_content_ignored_under_metadata_only(self):
        client, span_exporter, provider = self._make_tool_client(ContentCaptureMode.METADATA_ONLY)
        try:
            with client.start_tool_execution(ToolExecutionStart(tool_name="test_tool", include_content=True)) as rec:
                rec.set_result(arguments="args", result="result")

            span = self._get_tool_span(span_exporter)
            assert span.attributes.get("gen_ai.tool.call.arguments") is None
        finally:
            client.shutdown()
            provider.shutdown()

    def test_context_default_defers_to_client_full(self):
        """with_content_capture_mode(DEFAULT) should fall through to client FULL, not suppress content."""
        client, span_exporter, provider = self._make_tool_client(ContentCaptureMode.FULL)
        try:
            with with_content_capture_mode(ContentCaptureMode.DEFAULT):
                with client.start_tool_execution(
                    ToolExecutionStart(tool_name="test_tool", include_content=False)
                ) as rec:
                    rec.set_result(arguments="args", result="result")

            span = self._get_tool_span(span_exporter)
            assert span.attributes.get("gen_ai.tool.call.arguments") is not None
        finally:
            client.shutdown()
            provider.shutdown()

    def test_context_default_defers_to_client_metadata_only(self):
        """with_content_capture_mode(DEFAULT) should fall through to client METADATA_ONLY, not re-enable via legacy."""
        client, span_exporter, provider = self._make_tool_client(ContentCaptureMode.METADATA_ONLY)
        try:
            with with_content_capture_mode(ContentCaptureMode.DEFAULT):
                with client.start_tool_execution(
                    ToolExecutionStart(tool_name="test_tool", include_content=True)
                ) as rec:
                    rec.set_result(arguments="args", result="result")

            span = self._get_tool_span(span_exporter)
            assert span.attributes.get("gen_ai.tool.call.arguments") is None
        finally:
            client.shutdown()
            provider.shutdown()


# ---------------------------------------------------------------------------
# Context propagation (parent generation → child tool)
# ---------------------------------------------------------------------------


class TestContextPropagation:
    def test_generation_context_manager_sets_content_capture_mode(self):
        exporter = CapturingGenerationExporter()
        span_exporter = InMemorySpanExporter()
        provider = TracerProvider()
        provider.add_span_processor(SimpleSpanProcessor(span_exporter))
        tracer = provider.get_tracer("sigil-test")
        client = _new_client(exporter, tracer=tracer, content_capture=ContentCaptureMode.METADATA_ONLY)

        try:
            with client.start_generation(_seed()) as gen_rec:
                # Within the generation context, content capture mode should be set
                mode = content_capture_mode_from_context()
                assert mode == ContentCaptureMode.METADATA_ONLY

                # Tool execution within this context should inherit the mode
                with client.start_tool_execution(
                    ToolExecutionStart(tool_name="test_tool", include_content=True)
                ) as tool_rec:
                    tool_rec.set_result(arguments="args", result="result")

                gen_rec.set_result(
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="ok")])],
                    usage=TokenUsage(input_tokens=1, output_tokens=1),
                )

            # Tool span should NOT have content (inherited MetadataOnly suppresses)
            for span in span_exporter.get_finished_spans():
                if span.name.startswith("execute_tool"):
                    assert span.attributes.get("gen_ai.tool.call.arguments") is None
                    break
            else:
                raise AssertionError("tool span not found")
        finally:
            client.shutdown()
            provider.shutdown()

    def test_generation_full_context_allows_tool_content(self):
        exporter = CapturingGenerationExporter()
        span_exporter = InMemorySpanExporter()
        provider = TracerProvider()
        provider.add_span_processor(SimpleSpanProcessor(span_exporter))
        tracer = provider.get_tracer("sigil-test")
        client = _new_client(exporter, tracer=tracer, content_capture=ContentCaptureMode.METADATA_ONLY)

        try:
            # Per-generation override to FULL
            with client.start_generation(_seed(ContentCaptureMode.FULL)) as gen_rec:
                with client.start_tool_execution(
                    ToolExecutionStart(tool_name="test_tool", include_content=True)
                ) as tool_rec:
                    tool_rec.set_result(arguments="args", result="result")

                gen_rec.set_result(
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="ok")])],
                    usage=TokenUsage(input_tokens=1, output_tokens=1),
                )

            for span in span_exporter.get_finished_spans():
                if span.name.startswith("execute_tool"):
                    assert span.attributes.get("gen_ai.tool.call.arguments") is not None
                    break
            else:
                raise AssertionError("tool span not found")
        finally:
            client.shutdown()
            provider.shutdown()

    def test_with_content_capture_mode_context_manager(self):
        assert content_capture_mode_from_context() is None

        with with_content_capture_mode(ContentCaptureMode.FULL):
            assert content_capture_mode_from_context() == ContentCaptureMode.FULL

        assert content_capture_mode_from_context() is None

    def test_recorder_inside_with_content_capture_mode_preserves_override(self):
        """A recorder starting and ending inside with_content_capture_mode must not clobber the user override."""
        exporter = CapturingGenerationExporter()
        span_exporter = InMemorySpanExporter()
        provider = TracerProvider()
        provider.add_span_processor(SimpleSpanProcessor(span_exporter))
        tracer = provider.get_tracer("sigil-test")
        client = _new_client(exporter, tracer=tracer, content_capture=ContentCaptureMode.METADATA_ONLY)

        try:
            with with_content_capture_mode(ContentCaptureMode.FULL):
                assert content_capture_mode_from_context() == ContentCaptureMode.FULL

                with client.start_generation(_seed()) as gen_rec:
                    gen_rec.set_result(
                        output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="ok")])],
                        usage=TokenUsage(input_tokens=1, output_tokens=1),
                    )

                # After the recorder ends, the context override must still be FULL
                assert content_capture_mode_from_context() == ContentCaptureMode.FULL

                # Tool execution here should still see FULL from the context
                with client.start_tool_execution(
                    ToolExecutionStart(tool_name="test_tool", include_content=False)
                ) as tool_rec:
                    tool_rec.set_result(arguments="args", result="result")

                for span in span_exporter.get_finished_spans():
                    if span.name.startswith("execute_tool"):
                        assert span.attributes.get("gen_ai.tool.call.arguments") is not None
                        break
                else:
                    raise AssertionError("tool span not found")

            assert content_capture_mode_from_context() is None
        finally:
            client.shutdown()
            provider.shutdown()

    def test_with_content_capture_mode_overrides_generation_to_metadata_only(self):
        """with_content_capture_mode(METADATA_ONLY) should strip generation content even when client is FULL."""
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, content_capture=ContentCaptureMode.FULL)
        try:
            with with_content_capture_mode(ContentCaptureMode.METADATA_ONLY):
                rec = client.start_generation(_seed())
                rec.set_result(
                    Generation(
                        system_prompt="secret system prompt",
                        input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="Hello")])],
                        output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="Hi")])],
                        usage=TokenUsage(input_tokens=10, output_tokens=5),
                    )
                )
                rec.end()
                assert rec.err() is None

                gen = rec.last_generation
                assert gen.metadata[_METADATA_KEY] == "metadata_only"
                assert gen.system_prompt == ""
                assert gen.input[0].parts[0].text == ""
                assert gen.output[0].parts[0].text == ""
        finally:
            client.shutdown()

    def test_with_content_capture_mode_overrides_generation_to_full(self):
        """with_content_capture_mode(FULL) should preserve generation content even when client is METADATA_ONLY."""
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, content_capture=ContentCaptureMode.METADATA_ONLY)
        try:
            with with_content_capture_mode(ContentCaptureMode.FULL):
                rec = client.start_generation(_seed())
                rec.set_result(
                    output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="Hello")])],
                    usage=TokenUsage(input_tokens=10, output_tokens=5),
                )
                rec.end()
                assert rec.err() is None

                gen = rec.last_generation
                assert gen.metadata[_METADATA_KEY] == "full"
                assert gen.output[0].parts[0].text == "Hello"
        finally:
            client.shutdown()

    def test_per_recording_override_takes_priority_over_context(self):
        """GenerationStart.content_capture should override with_content_capture_mode."""
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, content_capture=ContentCaptureMode.FULL)
        try:
            with with_content_capture_mode(ContentCaptureMode.FULL):
                rec = client.start_generation(_seed(ContentCaptureMode.METADATA_ONLY))
                rec.set_result(
                    Generation(
                        system_prompt="secret",
                        input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="Hello")])],
                        output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="Hi")])],
                        usage=TokenUsage(input_tokens=10, output_tokens=5),
                    )
                )
                rec.end()
                assert rec.err() is None

                gen = rec.last_generation
                assert gen.metadata[_METADATA_KEY] == "metadata_only"
                assert gen.system_prompt == ""
        finally:
            client.shutdown()


# ---------------------------------------------------------------------------
# Validation accepts stripped content
# ---------------------------------------------------------------------------


class TestValidationWithStrippedContent:
    def test_validation_accepts_stripped_generation(self):
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, content_capture=ContentCaptureMode.METADATA_ONLY)
        try:
            rec = client.start_generation(_seed())
            rec.set_result(_full_generation())
            rec.end()
            # No validation error
            assert rec.err() is None
        finally:
            client.shutdown()

    def test_validation_accepts_stripped_text_and_thinking(self):
        """Directly test that validate_generation accepts empty text/thinking when metadata marker is set."""
        gen = Generation(
            model=ModelRef(provider="anthropic", name="claude-sonnet-4-5"),
            input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="")])],
            output=[
                Message(
                    role=MessageRole.ASSISTANT,
                    parts=[
                        Part(kind=PartKind.THINKING, thinking=""),
                        Part(kind=PartKind.TEXT, text=""),
                    ],
                )
            ],
            usage=TokenUsage(input_tokens=1, output_tokens=1),
            metadata={_METADATA_KEY: "metadata_only"},
        )
        # Should not raise
        validate_generation(gen)

    def test_validation_rejects_empty_text_without_stripped_marker(self):
        gen = Generation(
            model=ModelRef(provider="anthropic", name="claude-sonnet-4-5"),
            input=[Message(role=MessageRole.USER, parts=[Part(kind=PartKind.TEXT, text="")])],
            output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="ok")])],
            usage=TokenUsage(input_tokens=1, output_tokens=1),
            metadata={},
        )
        with pytest.raises(ValueError):
            validate_generation(gen)


# ---------------------------------------------------------------------------
# Backward compatibility: include_content
# ---------------------------------------------------------------------------


class TestBackwardCompatibility:
    def test_include_content_still_works_without_content_capture(self):
        """When no ContentCaptureMode is set, include_content=True should still include content."""
        span_exporter = InMemorySpanExporter()
        provider = TracerProvider()
        provider.add_span_processor(SimpleSpanProcessor(span_exporter))
        tracer = provider.get_tracer("sigil-test")
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, tracer=tracer)

        try:
            with client.start_tool_execution(ToolExecutionStart(tool_name="test_tool", include_content=True)) as rec:
                rec.set_result(arguments="some args", result="some result")

            span = None
            for s in span_exporter.get_finished_spans():
                if s.name.startswith("execute_tool"):
                    span = s
                    break
            assert span is not None
            assert span.attributes.get("gen_ai.tool.call.arguments") is not None
            assert span.attributes.get("gen_ai.tool.call.result") is not None
        finally:
            client.shutdown()
            provider.shutdown()

    def test_include_content_false_without_content_capture(self):
        """Default client + include_content=False → content suppressed."""
        span_exporter = InMemorySpanExporter()
        provider = TracerProvider()
        provider.add_span_processor(SimpleSpanProcessor(span_exporter))
        tracer = provider.get_tracer("sigil-test")
        exporter = CapturingGenerationExporter()
        client = _new_client(exporter, tracer=tracer)

        try:
            with client.start_tool_execution(ToolExecutionStart(tool_name="test_tool", include_content=False)) as rec:
                rec.set_result(arguments="some args", result="some result")

            span = None
            for s in span_exporter.get_finished_spans():
                if s.name.startswith("execute_tool"):
                    span = s
                    break
            assert span is not None
            assert span.attributes.get("gen_ai.tool.call.arguments") is None
        finally:
            client.shutdown()
            provider.shutdown()


# ---------------------------------------------------------------------------
# Rating comment stripping
# ---------------------------------------------------------------------------


class TestRatingCommentStripping:
    def _make_rating_handler(self, captured):
        class _Handler(BaseHTTPRequestHandler):
            def do_POST(self):  # noqa: N802
                length = int(self.headers.get("Content-Length", "0"))
                body = self.rfile.read(length)
                captured["payload"] = json.loads(body.decode("utf-8"))

                response = {
                    "rating": {
                        "rating_id": "rat-1",
                        "conversation_id": "conv-1",
                        "rating": "CONVERSATION_RATING_VALUE_BAD",
                        "created_at": "2026-04-10T12:00:00Z",
                    },
                    "summary": {
                        "total_count": 1,
                        "good_count": 0,
                        "bad_count": 1,
                        "latest_rating": "CONVERSATION_RATING_VALUE_BAD",
                        "latest_rated_at": "2026-04-10T12:00:00Z",
                        "has_bad_rating": True,
                    },
                }
                encoded = json.dumps(response).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(encoded)))
                self.end_headers()
                self.wfile.write(encoded)

            def log_message(self, _format, *_args):  # noqa: A003
                return

        return _Handler

    def test_metadata_only_strips_rating_comment(self):
        captured: dict = {}
        handler = self._make_rating_handler(captured)
        server = HTTPServer(("127.0.0.1", 0), handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()

        client = Client(
            ClientConfig(
                content_capture=ContentCaptureMode.METADATA_ONLY,
                generation_export=GenerationExportConfig(
                    protocol="none",
                    batch_size=1,
                    flush_interval=timedelta(seconds=60),
                ),
                api=ApiConfig(endpoint=f"http://127.0.0.1:{server.server_address[1]}"),
            )
        )

        try:
            client.submit_conversation_rating(
                "conv-1",
                ConversationRatingInput(
                    rating_id="rat-1",
                    rating=ConversationRatingValue.BAD,
                    comment="this is sensitive feedback",
                ),
            )
            # Comment should have been stripped before sending
            assert "comment" not in captured["payload"] or captured["payload"].get("comment", "") == ""
        finally:
            client.shutdown()
            server.shutdown()
            server.server_close()

    def test_full_mode_preserves_rating_comment(self):
        captured: dict = {}
        handler = self._make_rating_handler(captured)
        server = HTTPServer(("127.0.0.1", 0), handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()

        client = Client(
            ClientConfig(
                content_capture=ContentCaptureMode.FULL,
                generation_export=GenerationExportConfig(
                    protocol="none",
                    batch_size=1,
                    flush_interval=timedelta(seconds=60),
                ),
                api=ApiConfig(endpoint=f"http://127.0.0.1:{server.server_address[1]}"),
            )
        )

        try:
            client.submit_conversation_rating(
                "conv-1",
                ConversationRatingInput(
                    rating_id="rat-1",
                    rating=ConversationRatingValue.BAD,
                    comment="this should be preserved",
                ),
            )
            assert captured["payload"]["comment"] == "this should be preserved"
        finally:
            client.shutdown()
            server.shutdown()
            server.server_close()
