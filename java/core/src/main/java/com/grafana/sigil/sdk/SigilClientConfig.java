package com.grafana.sigil.sdk;

import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Tracer;
import java.time.Clock;
import java.util.logging.Logger;

/** Top-level runtime configuration for {@link SigilClient}. */
public final class SigilClientConfig {
    private GenerationExportConfig generationExport = new GenerationExportConfig();
    private ApiConfig api = new ApiConfig();
    private EmbeddingCaptureConfig embeddingCapture = new EmbeddingCaptureConfig();
    private ContentCaptureMode contentCapture = ContentCaptureMode.DEFAULT;
    private ContentCaptureResolver contentCaptureResolver;
    private GenerationExporter generationExporter;
    private Tracer tracer;
    private Meter meter;
    private Logger logger = Logger.getLogger("com.grafana.sigil.sdk");
    private Clock clock = Clock.systemUTC();

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

    public EmbeddingCaptureConfig getEmbeddingCapture() {
        return embeddingCapture;
    }

    public SigilClientConfig setEmbeddingCapture(EmbeddingCaptureConfig embeddingCapture) {
        this.embeddingCapture = embeddingCapture == null ? new EmbeddingCaptureConfig() : embeddingCapture;
        return this;
    }

    public ContentCaptureMode getContentCapture() {
        return contentCapture;
    }

    /**
     * Sets the client-level {@link ContentCaptureMode}.
     *
     * <p>Resolution order for each recording: per-recording override on the
     * {@code Start} object &gt; {@link ContentCaptureResolver} result &gt; OTel
     * context inherited from the parent generation (tool executions only) &gt;
     * this client-level mode. {@link ContentCaptureMode#DEFAULT} resolves to
     * {@link ContentCaptureMode#NO_TOOL_CONTENT} for backward compatibility.</p>
     *
     * <p>{@code null} is treated as {@link ContentCaptureMode#DEFAULT}.</p>
     */
    public SigilClientConfig setContentCapture(ContentCaptureMode contentCapture) {
        this.contentCapture = contentCapture == null ? ContentCaptureMode.DEFAULT : contentCapture;
        return this;
    }

    public ContentCaptureResolver getContentCaptureResolver() {
        return contentCaptureResolver;
    }

    /**
     * Sets a callback invoked for each generation, tool execution, and conversation
     * rating to dynamically choose a {@link ContentCaptureMode} based on request
     * metadata (e.g. tenant id, feature flag).
     *
     * <p>The resolver fails closed: any thrown exception is logged at WARNING and
     * the request is recorded as {@link ContentCaptureMode#METADATA_ONLY}.</p>
     *
     * <p>Pass {@code null} to clear the resolver.</p>
     */
    public SigilClientConfig setContentCaptureResolver(ContentCaptureResolver contentCaptureResolver) {
        this.contentCaptureResolver = contentCaptureResolver;
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
                .setGenerationExport(generationExport.copy())
                .setApi(api.copy())
                .setEmbeddingCapture(embeddingCapture.copy())
                .setContentCapture(contentCapture)
                .setContentCaptureResolver(contentCaptureResolver)
                .setGenerationExporter(generationExporter)
                .setTracer(tracer)
                .setMeter(meter)
                .setLogger(logger)
                .setClock(clock);
    }
}
