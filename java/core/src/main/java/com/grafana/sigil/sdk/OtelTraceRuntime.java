package com.grafana.sigil.sdk;

import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Tracer;
import io.opentelemetry.exporter.otlp.http.metrics.OtlpHttpMetricExporter;
import io.opentelemetry.exporter.otlp.http.trace.OtlpHttpSpanExporter;
import io.opentelemetry.exporter.otlp.metrics.OtlpGrpcMetricExporter;
import io.opentelemetry.exporter.otlp.trace.OtlpGrpcSpanExporter;
import io.opentelemetry.sdk.metrics.Aggregation;
import io.opentelemetry.sdk.metrics.InstrumentSelector;
import io.opentelemetry.sdk.metrics.SdkMeterProvider;
import io.opentelemetry.sdk.metrics.View;
import io.opentelemetry.sdk.metrics.export.MetricExporter;
import io.opentelemetry.sdk.metrics.export.PeriodicMetricReader;
import io.opentelemetry.sdk.trace.SdkTracerProvider;
import io.opentelemetry.sdk.trace.export.BatchSpanProcessor;
import io.opentelemetry.sdk.trace.export.SpanExporter;
import java.net.URI;
import java.net.URISyntaxException;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;

final class OtelTraceRuntime implements TraceRuntime {
    private static final String INSTRUMENTATION_NAME = "github.com/grafana/sigil/sdks/java";

    private static final String METRIC_OPERATION_DURATION = "gen_ai.client.operation.duration";
    private static final String METRIC_TOKEN_USAGE = "gen_ai.client.token.usage";
    private static final String METRIC_TTFT = "gen_ai.client.time_to_first_token";
    private static final String METRIC_TOOL_CALLS_PER_OPERATION = "gen_ai.client.tool_calls_per_operation";

    private final SdkTracerProvider traceProvider;
    private final SdkMeterProvider meterProvider;
    private final Tracer tracer;
    private final Meter meter;

    private OtelTraceRuntime(SdkTracerProvider traceProvider, SdkMeterProvider meterProvider) {
        this.traceProvider = traceProvider;
        this.meterProvider = meterProvider;
        this.tracer = traceProvider.get(INSTRUMENTATION_NAME);
        this.meter = meterProvider.get(INSTRUMENTATION_NAME);
    }

    static TraceRuntime fromConfig(TraceConfig config) {
        SpanExporter traceExporter = newTraceExporter(config);
        SdkTracerProvider traceProvider = SdkTracerProvider.builder()
                .addSpanProcessor(BatchSpanProcessor.builder(traceExporter).build())
                .build();

        MetricExporter metricExporter = newMetricExporter(config);
        PeriodicMetricReader metricReader = PeriodicMetricReader.builder(metricExporter).build();
        SdkMeterProvider meterProvider = SdkMeterProvider.builder()
                .registerMetricReader(metricReader)
                .registerView(
                        InstrumentSelector.builder().setName(METRIC_OPERATION_DURATION).build(),
                        View.builder().setAggregation(Aggregation.explicitBucketHistogram(List.of(
                                0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0, 120.0
                        ))).build())
                .registerView(
                        InstrumentSelector.builder().setName(METRIC_TOKEN_USAGE).build(),
                        View.builder().setAggregation(Aggregation.explicitBucketHistogram(List.of(
                                1.0, 10.0, 50.0, 100.0, 250.0, 500.0, 1_000.0, 2_500.0, 5_000.0, 10_000.0, 50_000.0, 100_000.0
                        ))).build())
                .registerView(
                        InstrumentSelector.builder().setName(METRIC_TTFT).build(),
                        View.builder().setAggregation(Aggregation.explicitBucketHistogram(List.of(
                                0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0
                        ))).build())
                .registerView(
                        InstrumentSelector.builder().setName(METRIC_TOOL_CALLS_PER_OPERATION).build(),
                        View.builder().setAggregation(Aggregation.explicitBucketHistogram(List.of(
                                0.0, 1.0, 2.0, 3.0, 5.0, 10.0, 20.0, 50.0
                        ))).build())
                .build();

        return new OtelTraceRuntime(traceProvider, meterProvider);
    }

    @Override
    public Tracer tracer() {
        return tracer;
    }

    @Override
    public Meter meter() {
        return meter;
    }

    @Override
    public void flush() {
        traceProvider.forceFlush().join(10_000, TimeUnit.MILLISECONDS);
        meterProvider.forceFlush().join(10_000, TimeUnit.MILLISECONDS);
    }

    @Override
    public void shutdown() {
        traceProvider.shutdown().join(10_000, TimeUnit.MILLISECONDS);
        meterProvider.shutdown().join(10_000, TimeUnit.MILLISECONDS);
    }

    private static SpanExporter newTraceExporter(TraceConfig config) {
        if (config.getProtocol() == TraceProtocol.OTLP_GRPC) {
            var builder = OtlpGrpcSpanExporter.builder().setEndpoint(config.getEndpoint());
            for (Map.Entry<String, String> entry : config.getHeaders().entrySet()) {
                builder.addHeader(entry.getKey(), entry.getValue());
            }
            return builder.build();
        }

        var builder = OtlpHttpSpanExporter.builder().setEndpoint(config.getEndpoint());
        for (Map.Entry<String, String> entry : config.getHeaders().entrySet()) {
            builder.addHeader(entry.getKey(), entry.getValue());
        }
        return builder.build();
    }

    private static MetricExporter newMetricExporter(TraceConfig config) {
        if (config.getProtocol() == TraceProtocol.OTLP_GRPC) {
            var builder = OtlpGrpcMetricExporter.builder().setEndpoint(config.getEndpoint());
            for (Map.Entry<String, String> entry : config.getHeaders().entrySet()) {
                builder.addHeader(entry.getKey(), entry.getValue());
            }
            return builder.build();
        }

        String endpoint = metricEndpointFromTraceEndpoint(config.getEndpoint());
        var builder = OtlpHttpMetricExporter.builder().setEndpoint(endpoint);
        for (Map.Entry<String, String> entry : config.getHeaders().entrySet()) {
            builder.addHeader(entry.getKey(), entry.getValue());
        }
        return builder.build();
    }

    private static String metricEndpointFromTraceEndpoint(String endpoint) {
        try {
            URI parsed = new URI(endpoint);
            String path = parsed.getPath() == null ? "" : parsed.getPath();
            if (path.isBlank() || "/".equals(path) || "/v1/traces".equals(path)) {
                URI updated = new URI(
                        parsed.getScheme(),
                        parsed.getUserInfo(),
                        parsed.getHost(),
                        parsed.getPort(),
                        "/v1/metrics",
                        parsed.getQuery(),
                        parsed.getFragment());
                return updated.toString();
            }
            return endpoint;
        } catch (URISyntaxException ignored) {
            return endpoint;
        }
    }
}
