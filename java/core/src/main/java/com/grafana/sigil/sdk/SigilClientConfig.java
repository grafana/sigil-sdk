package com.grafana.sigil.sdk;

import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Tracer;
import java.time.Clock;
import java.util.logging.Logger;

/** Top-level runtime configuration for {@link SigilClient}. */
public final class SigilClientConfig {
    private TraceConfig trace = new TraceConfig();
    private GenerationExportConfig generationExport = new GenerationExportConfig();
    private ApiConfig api = new ApiConfig();
    private GenerationExporter generationExporter;
    private Tracer tracer;
    private Meter meter;
    private Logger logger = Logger.getLogger("com.grafana.sigil.sdk");
    private Clock clock = Clock.systemUTC();

    public TraceConfig getTrace() {
        return trace;
    }

    public SigilClientConfig setTrace(TraceConfig trace) {
        this.trace = trace == null ? new TraceConfig() : trace;
        return this;
    }

    public GenerationExportConfig getGenerationExport() {
        return generationExport;
    }

    public SigilClientConfig setGenerationExport(GenerationExportConfig generationExport) {
        this.generationExport = generationExport == null ? new GenerationExportConfig() : generationExport;
        return this;
    }

    public ApiConfig getApi() {
        return api;
    }

    public SigilClientConfig setApi(ApiConfig api) {
        this.api = api == null ? new ApiConfig() : api;
        return this;
    }

    public Tracer getTracer() {
        return tracer;
    }

    public Meter getMeter() {
        return meter;
    }

    public GenerationExporter getGenerationExporter() {
        return generationExporter;
    }

    public SigilClientConfig setGenerationExporter(GenerationExporter generationExporter) {
        this.generationExporter = generationExporter;
        return this;
    }

    public SigilClientConfig setTracer(Tracer tracer) {
        this.tracer = tracer;
        return this;
    }

    public SigilClientConfig setMeter(Meter meter) {
        this.meter = meter;
        return this;
    }

    public Logger getLogger() {
        return logger;
    }

    public SigilClientConfig setLogger(Logger logger) {
        this.logger = logger == null ? Logger.getLogger("com.grafana.sigil.sdk") : logger;
        return this;
    }

    public Clock getClock() {
        return clock;
    }

    public SigilClientConfig setClock(Clock clock) {
        this.clock = clock == null ? Clock.systemUTC() : clock;
        return this;
    }

    public SigilClientConfig copy() {
        return new SigilClientConfig()
                .setTrace(trace.copy())
                .setGenerationExport(generationExport.copy())
                .setApi(api.copy())
                .setGenerationExporter(generationExporter)
                .setTracer(tracer)
                .setMeter(meter)
                .setLogger(logger)
                .setClock(clock);
    }
}
