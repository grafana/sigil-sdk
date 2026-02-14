package com.grafana.sigil.sdk;

import static org.assertj.core.api.Assertions.assertThat;

import io.opentelemetry.sdk.metrics.SdkMeterProvider;
import io.opentelemetry.sdk.metrics.data.MetricData;
import io.opentelemetry.sdk.testing.exporter.InMemoryMetricReader;
import io.opentelemetry.sdk.trace.SdkTracerProvider;
import java.time.Duration;
import java.util.List;
import org.junit.jupiter.api.Test;

class SigilClientMetricsTest {
    @Test
    void generationRecorderEmitsAllClientMetrics() {
        InMemoryMetricReader metricReader = InMemoryMetricReader.create();
        SdkMeterProvider meterProvider = SdkMeterProvider.builder()
                .registerMetricReader(metricReader)
                .build();
        SdkTracerProvider tracerProvider = SdkTracerProvider.builder().build();

        TestFixtures.CapturingExporter exporter = new TestFixtures.CapturingExporter();
        SigilClientConfig config = new SigilClientConfig()
                .setTracer(tracerProvider.get("test"))
                .setMeter(meterProvider.get("test"))
                .setGenerationExporter(exporter)
                .setGenerationExport(new GenerationExportConfig()
                        .setBatchSize(100)
                        .setQueueSize(100)
                        .setFlushInterval(Duration.ofMinutes(10))
                        .setMaxRetries(0));

        GenerationStart start = TestFixtures.startFixture()
                .setMode(null)
                .setOperationName("");

        try (SigilClient client = new SigilClient(config)) {
            GenerationRecorder recorder = client.startStreamingGeneration(start);
            recorder.setFirstTokenAt(start.getStartedAt().plusMillis(250));

            GenerationResult result = TestFixtures.resultFixture();
            result.setMode(GenerationMode.STREAM);
            result.setOperationName("streamText");
            result.getUsage().setReasoningTokens(5);
            result.getUsage().setCacheCreationInputTokens(3);
            recorder.setResult(result);
            recorder.end();
        }

        List<String> metricNames = metricReader.collectAllMetrics().stream()
                .map(MetricData::getName)
                .toList();

        assertThat(metricNames).contains(
                SigilClient.METRIC_OPERATION_DURATION,
                SigilClient.METRIC_TOKEN_USAGE,
                SigilClient.METRIC_TTFT,
                SigilClient.METRIC_TOOL_CALLS_PER_OPERATION
        );

        tracerProvider.shutdown();
        meterProvider.shutdown();
    }
}
