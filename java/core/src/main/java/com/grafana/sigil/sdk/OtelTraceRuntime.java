package com.grafana.sigil.sdk;

import io.opentelemetry.api.trace.Tracer;
import io.opentelemetry.exporter.otlp.http.trace.OtlpHttpSpanExporter;
import io.opentelemetry.exporter.otlp.trace.OtlpGrpcSpanExporter;
import io.opentelemetry.sdk.trace.SdkTracerProvider;
import io.opentelemetry.sdk.trace.export.BatchSpanProcessor;
import io.opentelemetry.sdk.trace.export.SpanExporter;
import java.util.Map;
import java.util.concurrent.TimeUnit;

final class OtelTraceRuntime implements TraceRuntime {
    private static final String INSTRUMENTATION_NAME = "github.com/grafana/sigil/sdks/java";

    private final SdkTracerProvider provider;
    private final Tracer tracer;

    private OtelTraceRuntime(SdkTracerProvider provider) {
        this.provider = provider;
        this.tracer = provider.get(INSTRUMENTATION_NAME);
    }

    static TraceRuntime fromConfig(TraceConfig config) {
        SpanExporter exporter = newExporter(config);
        SdkTracerProvider provider = SdkTracerProvider.builder()
                .addSpanProcessor(BatchSpanProcessor.builder(exporter).build())
                .build();
        return new OtelTraceRuntime(provider);
    }

    @Override
    public Tracer tracer() {
        return tracer;
    }

    @Override
    public void flush() {
        provider.forceFlush().join(10_000, TimeUnit.MILLISECONDS);
    }

    @Override
    public void shutdown() {
        provider.shutdown().join(10_000, TimeUnit.MILLISECONDS);
    }

    private static SpanExporter newExporter(TraceConfig config) {
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
}
