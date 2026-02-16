package com.grafana.sigil.sdk;

import static org.assertj.core.api.Assertions.assertThat;

import io.opentelemetry.api.common.AttributeKey;
import io.opentelemetry.api.trace.StatusCode;
import io.opentelemetry.sdk.trace.SdkTracerProvider;
import io.opentelemetry.sdk.trace.data.SpanData;
import io.opentelemetry.sdk.trace.export.SimpleSpanProcessor;
import io.opentelemetry.sdk.testing.exporter.InMemorySpanExporter;
import java.time.Duration;
import java.util.List;
import org.junit.jupiter.api.Test;

class SigilClientSpansTest {
    @Test
    void generationSpanHasRequiredAttributesAndErrorTyping() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        SdkTracerProvider provider = SdkTracerProvider.builder()
                .addSpanProcessor(SimpleSpanProcessor.create(spanExporter))
                .build();

        TestFixtures.CapturingExporter exporter = new TestFixtures.CapturingExporter();
        SigilClientConfig config = new SigilClientConfig()
                .setTracer(provider.get("test"))
                .setGenerationExporter(exporter)
                .setGenerationExport(new GenerationExportConfig()
                        .setBatchSize(1)
                        .setFlushInterval(Duration.ofMinutes(10))
                        .setMaxRetries(0));

        try (SigilClient client = new SigilClient(config)) {
            GenerationRecorder recorder = client.startGeneration(TestFixtures.startFixture());
            GenerationResult result = TestFixtures.resultFixture();
            result.getUsage().setReasoningTokens(5);
            result.getUsage().setCacheCreationInputTokens(3);
            recorder.setResult(result);
            recorder.setCallError(new RuntimeException("provider exploded"));
            recorder.end();
        }

        List<SpanData> spans = spanExporter.getFinishedSpanItems();
        assertThat(spans).hasSize(1);
        SpanData span = spans.get(0);
        assertThat(span.getName()).startsWith("streamText ");
        assertThat(span.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_SDK_NAME))).isEqualTo("sdk-java");
        assertThat(span.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_PROVIDER_NAME))).isEqualTo("anthropic");
        assertThat(span.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_REQUEST_MODEL))).isEqualTo("claude-sonnet-4-5");
        assertThat(span.getAttributes().get(AttributeKey.longKey(SigilClient.SPAN_ATTR_REQUEST_MAX_TOKENS))).isEqualTo(256L);
        assertThat(span.getAttributes().get(AttributeKey.doubleKey(SigilClient.SPAN_ATTR_REQUEST_TEMPERATURE))).isEqualTo(0.25d);
        assertThat(span.getAttributes().get(AttributeKey.doubleKey(SigilClient.SPAN_ATTR_REQUEST_TOP_P))).isEqualTo(0.85d);
        assertThat(span.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_REQUEST_TOOL_CHOICE))).isEqualTo("required");
        assertThat(span.getAttributes().get(AttributeKey.booleanKey(SigilClient.SPAN_ATTR_REQUEST_THINKING_ENABLED))).isEqualTo(false);
        assertThat(span.getAttributes().get(AttributeKey.longKey(SigilClient.SPAN_ATTR_REQUEST_THINKING_BUDGET))).isEqualTo(2048L);
        assertThat(span.getAttributes().get(AttributeKey.stringArrayKey(SigilClient.SPAN_ATTR_FINISH_REASONS))).containsExactly("stop");
        assertThat(span.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_ERROR_TYPE))).isEqualTo("provider_call_error");
        assertThat(span.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_ERROR_CATEGORY))).isEqualTo("sdk_error");
        assertThat(span.getAttributes().get(AttributeKey.longKey(SigilClient.SPAN_ATTR_REASONING_TOKENS))).isEqualTo(5L);
        assertThat(span.getAttributes().get(AttributeKey.longKey(SigilClient.SPAN_ATTR_CACHE_CREATION_TOKENS))).isEqualTo(3L);
        assertThat(span.getStatus().getStatusCode()).isEqualTo(StatusCode.ERROR);

        provider.shutdown();
    }

    @Test
    void toolSpanNameAndAttributesMatchContract() {
        InMemorySpanExporter spanExporter = InMemorySpanExporter.create();
        SdkTracerProvider provider = SdkTracerProvider.builder()
                .addSpanProcessor(SimpleSpanProcessor.create(spanExporter))
                .build();

        TestFixtures.CapturingExporter exporter = new TestFixtures.CapturingExporter();
        SigilClientConfig config = new SigilClientConfig()
                .setTracer(provider.get("test"))
                .setGenerationExporter(exporter)
                .setGenerationExport(new GenerationExportConfig()
                        .setBatchSize(100)
                        .setFlushInterval(Duration.ofMinutes(10))
                        .setMaxRetries(0));

        try (SigilClient client = new SigilClient(config)) {
            ToolExecutionRecorder recorder = client.startToolExecution(new ToolExecutionStart()
                    .setToolName("weather")
                    .setToolCallId("call-1")
                    .setToolType("function")
                    .setToolDescription("Get weather"));
            recorder.setResult(new ToolExecutionResult().setArguments(java.util.Map.of("city", "Paris")).setResult("18C"));
            recorder.end();
        }

        List<SpanData> spans = spanExporter.getFinishedSpanItems();
        assertThat(spans).hasSize(1);
        SpanData span = spans.get(0);
        assertThat(span.getName()).isEqualTo("execute_tool weather");
        assertThat(span.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_SDK_NAME))).isEqualTo("sdk-java");
        assertThat(span.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_TOOL_NAME))).isEqualTo("weather");
        assertThat(span.getAttributes().get(AttributeKey.stringKey(SigilClient.SPAN_ATTR_TOOL_CALL_ID))).isEqualTo("call-1");

        provider.shutdown();
    }
}
