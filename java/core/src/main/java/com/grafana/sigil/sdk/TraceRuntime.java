package com.grafana.sigil.sdk;

import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Tracer;

interface TraceRuntime extends AutoCloseable {
    Tracer tracer();

    Meter meter();

    void flush();

    void shutdown();

    @Override
    default void close() {
        shutdown();
    }
}
