package com.grafana.sigil.sdk;

import com.fasterxml.jackson.databind.JsonNode;
import io.opentelemetry.api.GlobalOpenTelemetry;
import io.opentelemetry.api.common.AttributeKey;
import io.opentelemetry.api.common.Attributes;
import io.opentelemetry.api.metrics.DoubleHistogram;
import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.SpanKind;
import io.opentelemetry.api.trace.Tracer;
import io.opentelemetry.context.Scope;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Clock;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.UUID;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.logging.Level;
import java.util.logging.Logger;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** Sigil Java SDK runtime client. */
public final class SigilClient implements AutoCloseable {
    static final String SPAN_ATTR_GENERATION_ID = "sigil.generation.id";
    static final String SPAN_ATTR_CONVERSATION_ID = "gen_ai.conversation.id";
    static final String SPAN_ATTR_AGENT_NAME = "gen_ai.agent.name";
    static final String SPAN_ATTR_AGENT_VERSION = "gen_ai.agent.version";
    static final String SPAN_ATTR_ERROR_TYPE = "error.type";
    static final String SPAN_ATTR_ERROR_CATEGORY = "error.category";
    static final String SPAN_ATTR_OPERATION_NAME = "gen_ai.operation.name";
    static final String SPAN_ATTR_PROVIDER_NAME = "gen_ai.provider.name";
    static final String SPAN_ATTR_REQUEST_MODEL = "gen_ai.request.model";
    static final String SPAN_ATTR_REQUEST_MAX_TOKENS = "gen_ai.request.max_tokens";
    static final String SPAN_ATTR_REQUEST_TEMPERATURE = "gen_ai.request.temperature";
    static final String SPAN_ATTR_REQUEST_TOP_P = "gen_ai.request.top_p";
    static final String SPAN_ATTR_REQUEST_TOOL_CHOICE = "sigil.gen_ai.request.tool_choice";
    static final String SPAN_ATTR_REQUEST_THINKING_ENABLED = "sigil.gen_ai.request.thinking.enabled";
    static final String SPAN_ATTR_REQUEST_THINKING_BUDGET = "sigil.gen_ai.request.thinking.budget_tokens";
    static final String SPAN_ATTR_RESPONSE_ID = "gen_ai.response.id";
    static final String SPAN_ATTR_RESPONSE_MODEL = "gen_ai.response.model";
    static final String SPAN_ATTR_FINISH_REASONS = "gen_ai.response.finish_reasons";
    static final String SPAN_ATTR_INPUT_TOKENS = "gen_ai.usage.input_tokens";
    static final String SPAN_ATTR_OUTPUT_TOKENS = "gen_ai.usage.output_tokens";
    static final String SPAN_ATTR_CACHE_READ_TOKENS = "gen_ai.usage.cache_read_input_tokens";
    static final String SPAN_ATTR_CACHE_WRITE_TOKENS = "gen_ai.usage.cache_write_input_tokens";
    static final String SPAN_ATTR_CACHE_CREATION_TOKENS = "gen_ai.usage.cache_creation_input_tokens";
    static final String SPAN_ATTR_REASONING_TOKENS = "gen_ai.usage.reasoning_tokens";
    static final String SPAN_ATTR_TOOL_NAME = "gen_ai.tool.name";
    static final String SPAN_ATTR_TOOL_CALL_ID = "gen_ai.tool.call.id";
    static final String SPAN_ATTR_TOOL_TYPE = "gen_ai.tool.type";
    static final String SPAN_ATTR_TOOL_DESCRIPTION = "gen_ai.tool.description";
    static final String SPAN_ATTR_TOOL_CALL_ARGUMENTS = "gen_ai.tool.call.arguments";
    static final String SPAN_ATTR_TOOL_CALL_RESULT = "gen_ai.tool.call.result";
    private static final int MAX_RATING_CONVERSATION_ID_LEN = 255;
    private static final int MAX_RATING_ID_LEN = 128;
    private static final int MAX_RATING_GENERATION_ID_LEN = 255;
    private static final int MAX_RATING_ACTOR_ID_LEN = 255;
    private static final int MAX_RATING_SOURCE_LEN = 64;
    private static final int MAX_RATING_COMMENT_BYTES = 4096;
    private static final int MAX_RATING_METADATA_BYTES = 16 * 1024;

    static final String METRIC_OPERATION_DURATION = "gen_ai.client.operation.duration";
    static final String METRIC_TOKEN_USAGE = "gen_ai.client.token.usage";
    static final String METRIC_TTFT = "gen_ai.client.time_to_first_token";
    static final String METRIC_TOOL_CALLS_PER_OPERATION = "gen_ai.client.tool_calls_per_operation";
    static final String METRIC_ATTR_TOKEN_TYPE = "gen_ai.token.type";
    static final String METRIC_TOKEN_TYPE_INPUT = "input";
    static final String METRIC_TOKEN_TYPE_OUTPUT = "output";
    static final String METRIC_TOKEN_TYPE_CACHE_READ = "cache_read";
    static final String METRIC_TOKEN_TYPE_CACHE_WRITE = "cache_write";
    static final String METRIC_TOKEN_TYPE_CACHE_CREATION = "cache_creation";
    static final String METRIC_TOKEN_TYPE_REASONING = "reasoning";

    private static final Pattern STATUS_CODE_PATTERN = Pattern.compile("\\b([1-5][0-9][0-9])\\b");

    private final SigilClientConfig config;
    private final GenerationExporter generationExporter;
    private final TraceRuntime traceRuntime;
    private final Tracer tracer;
    private final Meter meter;
    private final DoubleHistogram operationDurationHistogram;
    private final DoubleHistogram tokenUsageHistogram;
    private final DoubleHistogram ttftHistogram;
    private final DoubleHistogram toolCallsHistogram;
    private final Logger logger;
    private final Clock clock;
    private final HttpClient ratingHttpClient = HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(10)).build();

    private final List<Generation> generations = new ArrayList<>();
    private final List<ToolExecution> toolExecutions = new ArrayList<>();
    private final Object debugLock = new Object();

    private final Object queueLock = new Object();
    private final List<Generation> pendingGenerations = new ArrayList<>();

    private final Object flushLock = new Object();
    private final ExecutorService flushExecutor = Executors.newSingleThreadExecutor(r -> {
        Thread thread = new Thread(r, "sigil-java-flush");
        thread.setDaemon(true);
        return thread;
    });
    private final AtomicBoolean flushScheduled = new AtomicBoolean(false);

    private final ScheduledExecutorService flushTimer = Executors.newSingleThreadScheduledExecutor(r -> {
        Thread thread = new Thread(r, "sigil-java-flush-timer");
        thread.setDaemon(true);
        return thread;
    });

    private final Object lifecycleLock = new Object();
    private volatile boolean shuttingDown;
    private volatile boolean closed;

    /** Creates a client with default runtime configuration. */
    public SigilClient() {
        this(new SigilClientConfig());
    }

    /**
     * Creates a client from caller-provided configuration.
     *
     * <p>Auth headers are resolved and validated during construction. Invalid auth combinations
     * fail fast at this point.</p>
     */
    public SigilClient(SigilClientConfig inputConfig) {
        this.config = inputConfig == null ? new SigilClientConfig() : inputConfig.copy();
        this.logger = config.getLogger();
        this.clock = config.getClock();

        GenerationExportConfig exportConfig = config.getGenerationExport();
        exportConfig.setHeaders(AuthHeaders.resolve(exportConfig.getHeaders(), exportConfig.getAuth(), "generation export"));
        ApiConfig apiConfig = config.getApi();
        if (apiConfig.getEndpoint().trim().isEmpty()) {
            apiConfig.setEndpoint("http://localhost:8080");
        }

        TraceConfig traceConfig = config.getTrace();
        traceConfig.setHeaders(AuthHeaders.resolve(traceConfig.getHeaders(), traceConfig.getAuth(), "trace"));

        this.generationExporter = config.getGenerationExporter() == null
                ? createGenerationExporter(exportConfig)
                : config.getGenerationExporter();

        if (config.getTracer() != null) {
            Tracer configuredTracer = config.getTracer();
            Meter configuredMeter = config.getMeter() != null
                    ? config.getMeter()
                    : GlobalOpenTelemetry.getMeter("github.com/grafana/sigil/sdks/java");
            this.traceRuntime = new TraceRuntime() {
                @Override
                public Tracer tracer() {
                    return configuredTracer;
                }

                @Override
                public Meter meter() {
                    return configuredMeter;
                }

                @Override
                public void flush() {
                }

                @Override
                public void shutdown() {
                }
            };
            this.tracer = configuredTracer;
            this.meter = configuredMeter;
        } else {
            TraceRuntime runtime;
            try {
                runtime = OtelTraceRuntime.fromConfig(traceConfig);
            } catch (Exception exception) {
                logWarn("sigil trace exporter init failed", exception);
                runtime = new TraceRuntime() {
                    private final Tracer fallback = GlobalOpenTelemetry.getTracer("github.com/grafana/sigil/sdks/java");
                    private final Meter fallbackMeter = GlobalOpenTelemetry.getMeter("github.com/grafana/sigil/sdks/java");

                    @Override
                    public Tracer tracer() {
                        return fallback;
                    }

                    @Override
                    public Meter meter() {
                        return fallbackMeter;
                    }

                    @Override
                    public void flush() {
                    }

                    @Override
                    public void shutdown() {
                    }
                };
            }
            this.traceRuntime = runtime;
            this.tracer = config.getTracer() != null ? config.getTracer() : runtime.tracer();
            this.meter = config.getMeter() != null ? config.getMeter() : runtime.meter();
        }

        this.operationDurationHistogram = meter.histogramBuilder(METRIC_OPERATION_DURATION)
                .setUnit("s")
                .build();
        this.tokenUsageHistogram = meter.histogramBuilder(METRIC_TOKEN_USAGE)
                .setUnit("token")
                .build();
        this.ttftHistogram = meter.histogramBuilder(METRIC_TTFT)
                .setUnit("s")
                .build();
        this.toolCallsHistogram = meter.histogramBuilder(METRIC_TOOL_CALLS_PER_OPERATION)
                .setUnit("count")
                .build();

        Duration interval = exportConfig.getFlushInterval();
        if (!interval.isNegative() && !interval.isZero()) {
            flushTimer.scheduleAtFixedRate(this::triggerAsyncFlush, interval.toMillis(), interval.toMillis(), TimeUnit.MILLISECONDS);
        }
    }

    /** Starts a generation recorder with {@link GenerationMode#SYNC} default mode. */
    public GenerationRecorder startGeneration(GenerationStart start) {
        return startGenerationInternal(start, GenerationMode.SYNC);
    }

    /** Starts a generation recorder with {@link GenerationMode#STREAM} default mode. */
    public GenerationRecorder startStreamingGeneration(GenerationStart start) {
        return startGenerationInternal(start, GenerationMode.STREAM);
    }

    /**
     * Runs a callback within a generation recorder lifecycle.
     *
     * <p>The recorder is always ended. Callback exceptions are propagated and also captured via
     * {@link GenerationRecorder#setCallError(Throwable)}.</p>
     */
    public <T> T withGeneration(GenerationStart start, ThrowingFunction<GenerationRecorder, T> fn) throws Exception {
        try (GenerationRecorder recorder = startGeneration(start)) {
            try {
                return fn.apply(recorder);
            } catch (Exception exception) {
                recorder.setCallError(exception);
                throw exception;
            } catch (Throwable throwable) {
                recorder.setCallError(throwable);
                throw new RuntimeException(throwable);
            }
        }
    }

    /**
     * Runs a callback within a streaming generation recorder lifecycle.
     *
     * <p>The recorder is always ended. Callback exceptions are propagated and also captured via
     * {@link GenerationRecorder#setCallError(Throwable)}.</p>
     */
    public <T> T withStreamingGeneration(GenerationStart start, ThrowingFunction<GenerationRecorder, T> fn) throws Exception {
        try (GenerationRecorder recorder = startStreamingGeneration(start)) {
            try {
                return fn.apply(recorder);
            } catch (Exception exception) {
                recorder.setCallError(exception);
                throw exception;
            } catch (Throwable throwable) {
                recorder.setCallError(throwable);
                throw new RuntimeException(throwable);
            }
        }
    }

    /**
     * Starts a tool execution recorder.
     *
     * <p>Empty tool names return a no-op recorder for instrumentation safety.</p>
     */
    public ToolExecutionRecorder startToolExecution(ToolExecutionStart start) {
        assertOpen();

        ToolExecutionStart seed = start == null ? new ToolExecutionStart() : start.copy();
        seed.setToolName(seed.getToolName().trim());
        if (seed.getToolName().isBlank()) {
            return ToolExecutionRecorder.INSTANCE_NOOP;
        }

        if (seed.getConversationId().isBlank()) {
            seed.setConversationId(SigilContext.conversationIdFromContext());
        }
        if (seed.getAgentName().isBlank()) {
            seed.setAgentName(SigilContext.agentNameFromContext());
        }
        if (seed.getAgentVersion().isBlank()) {
            seed.setAgentVersion(SigilContext.agentVersionFromContext());
        }

        Instant startedAt = seed.getStartedAt() == null ? now() : seed.getStartedAt();
        seed.setStartedAt(startedAt);

        Span span = tracer.spanBuilder(toolSpanName(seed.getToolName()))
                .setSpanKind(SpanKind.INTERNAL)
                .setStartTimestamp(startedAt)
                .startSpan();
        setToolSpanAttributes(span, seed);

        return new ToolExecutionRecorder(this, seed, span, startedAt);
    }

    /**
     * Runs a callback within a tool execution recorder lifecycle.
     *
     * <p>The recorder is always ended. Callback exceptions are propagated and also captured via
     * {@link ToolExecutionRecorder#setCallError(Throwable)}.</p>
     */
    public <T> T withToolExecution(ToolExecutionStart start, ThrowingFunction<ToolExecutionRecorder, T> fn) throws Exception {
        try (ToolExecutionRecorder recorder = startToolExecution(start)) {
            try {
                return fn.apply(recorder);
            } catch (Exception exception) {
                recorder.setCallError(exception);
                throw exception;
            } catch (Throwable throwable) {
                recorder.setCallError(throwable);
                throw new RuntimeException(throwable);
            }
        }
    }

    /** Submits a user-facing conversation rating through Sigil HTTP API. */
    public SubmitConversationRatingResponse submitConversationRating(
            String conversationId,
            SubmitConversationRatingRequest request) {
        assertOpen();

        String normalizedConversationId = conversationId == null ? "" : conversationId.trim();
        if (normalizedConversationId.isBlank()) {
            throw new ValidationException("sigil conversation rating validation failed: conversationId is required");
        }
        if (normalizedConversationId.length() > MAX_RATING_CONVERSATION_ID_LEN) {
            throw new ValidationException("sigil conversation rating validation failed: conversationId is too long");
        }

        SubmitConversationRatingRequest normalizedRequest = normalizeConversationRatingRequest(request);
        String endpoint = conversationRatingEndpoint(config.getApi(), config.getGenerationExport(), normalizedConversationId);

        Map<String, Object> payload = new LinkedHashMap<>();
        payload.put("rating_id", normalizedRequest.getRatingId());
        payload.put("rating", normalizedRequest.getRating().wireValue());
        if (!normalizedRequest.getComment().isBlank()) {
            payload.put("comment", normalizedRequest.getComment());
        }
        if (!normalizedRequest.getMetadata().isEmpty()) {
            payload.put("metadata", normalizedRequest.getMetadata());
        }
        if (!normalizedRequest.getGenerationId().isBlank()) {
            payload.put("generation_id", normalizedRequest.getGenerationId());
        }
        if (!normalizedRequest.getRaterId().isBlank()) {
            payload.put("rater_id", normalizedRequest.getRaterId());
        }
        if (!normalizedRequest.getSource().isBlank()) {
            payload.put("source", normalizedRequest.getSource());
        }

        String body;
        try {
            body = Json.MAPPER.writeValueAsString(payload);
        } catch (Exception exception) {
            throw new RatingTransportException("sigil conversation rating transport failed: serialize request", exception);
        }

        HttpRequest.Builder requestBuilder = HttpRequest.newBuilder()
                .uri(URI.create(endpoint))
                .timeout(Duration.ofSeconds(10))
                .header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(body));
        for (Map.Entry<String, String> header : config.getGenerationExport().getHeaders().entrySet()) {
            requestBuilder.header(header.getKey(), header.getValue());
        }

        HttpResponse<String> response;
        try {
            response = ratingHttpClient.send(requestBuilder.build(), HttpResponse.BodyHandlers.ofString());
        } catch (Exception exception) {
            if (exception instanceof InterruptedException) {
                Thread.currentThread().interrupt();
            }
            throw new RatingTransportException("sigil conversation rating transport failed", exception);
        }

        String responseBody = response.body() == null ? "" : response.body().trim();
        if (response.statusCode() == 400) {
            throw new ValidationException("sigil conversation rating validation failed: " + ratingErrorText(responseBody, 400));
        }
        if (response.statusCode() == 409) {
            throw new RatingConflictException("sigil conversation rating conflict: " + ratingErrorText(responseBody, 409));
        }
        if (response.statusCode() < 200 || response.statusCode() >= 300) {
            throw new RatingTransportException(
                    "sigil conversation rating transport failed: status " + response.statusCode() + ": " + ratingErrorText(responseBody, response.statusCode()));
        }
        if (responseBody.isBlank()) {
            throw new RatingTransportException("sigil conversation rating transport failed: empty response payload");
        }

        return parseSubmitConversationRatingResponse(responseBody);
    }

    /** Flushes queued generation payloads immediately. */
    public void flush() {
        if (shuttingDown) {
            throw new ClientShutdownException("sigil: client is shutting down");
        }
        flushInternal();
    }

    /**
     * Flushes pending data and shuts down generation + trace exporters.
     *
     * <p>Safe to call more than once.</p>
     */
    public void shutdown() {
        synchronized (lifecycleLock) {
            if (closed) {
                return;
            }
            shuttingDown = true;
        }

        flushTimer.shutdownNow();

        try {
            flushInternal();
        } catch (Exception exception) {
            logWarn("sigil generation export flush on shutdown failed", exception);
        }

        try {
            generationExporter.shutdown();
        } catch (Exception exception) {
            logWarn("sigil generation exporter shutdown failed", exception);
        }

        try {
            traceRuntime.flush();
        } catch (Exception exception) {
            logWarn("sigil trace provider flush on shutdown failed", exception);
        }

        try {
            traceRuntime.shutdown();
        } catch (Exception exception) {
            logWarn("sigil trace provider shutdown failed", exception);
        }

        flushExecutor.shutdown();

        synchronized (lifecycleLock) {
            closed = true;
        }
    }

    @Override
    public void close() {
        shutdown();
    }

    /** Returns an in-memory snapshot of recorded generations, tool executions, and queue size. */
    public SigilDebugSnapshot debugSnapshot() {
        synchronized (debugLock) {
            return new SigilDebugSnapshot(generations, toolExecutions, pendingGenerations.size());
        }
    }

    Instant now() {
        return Instant.now(clock);
    }

    void enqueueGeneration(Generation generation) {
        if (shuttingDown || closed) {
            throw new ClientShutdownException("sigil: client is shutting down");
        }

        int maxPayloadBytes = config.getGenerationExport().getPayloadMaxBytes();
        if (maxPayloadBytes > 0) {
            int payloadSize = ProtoMapper.toProtoGeneration(generation).getSerializedSize();
            if (payloadSize > maxPayloadBytes) {
                throw new EnqueueException("generation payload exceeds max bytes (" + payloadSize + " > " + maxPayloadBytes + ")");
            }
        }

        boolean shouldFlush = false;
        synchronized (queueLock) {
            if (pendingGenerations.size() >= config.getGenerationExport().getQueueSize()) {
                throw new QueueFullException("sigil: generation queue is full");
            }
            pendingGenerations.add(generation.copy());
            if (pendingGenerations.size() >= config.getGenerationExport().getBatchSize()) {
                shouldFlush = true;
            }
        }

        if (shouldFlush) {
            triggerAsyncFlush();
        }
    }

    void recordGeneration(Generation generation) {
        synchronized (debugLock) {
            generations.add(generation.copy());
        }
    }

    void recordToolExecution(ToolExecution toolExecution) {
        synchronized (debugLock) {
            toolExecutions.add(toolExecution.copy());
        }
    }

    private GenerationRecorder startGenerationInternal(GenerationStart start, GenerationMode defaultMode) {
        assertOpen();

        GenerationStart seed = start == null ? new GenerationStart() : start.copy();
        seed.setMode(seed.getMode() == null ? defaultMode : seed.getMode());

        if (seed.getOperationName().isBlank()) {
            seed.setOperationName(defaultOperationName(seed.getMode()));
        }
        if (seed.getConversationId().isBlank()) {
            seed.setConversationId(SigilContext.conversationIdFromContext());
        }
        if (seed.getAgentName().isBlank()) {
            seed.setAgentName(SigilContext.agentNameFromContext());
        }
        if (seed.getAgentVersion().isBlank()) {
            seed.setAgentVersion(SigilContext.agentVersionFromContext());
        }

        Instant startedAt = seed.getStartedAt() == null ? now() : seed.getStartedAt();
        seed.setStartedAt(startedAt);

        Span span = tracer.spanBuilder(generationSpanName(seed.getOperationName(), seed.getModel().getName()))
                .setSpanKind(SpanKind.CLIENT)
                .setStartTimestamp(startedAt)
                .startSpan();

        Generation initial = new Generation();
        initial.setId(seed.getId());
        initial.setConversationId(seed.getConversationId());
        initial.setAgentName(seed.getAgentName());
        initial.setAgentVersion(seed.getAgentVersion());
        initial.setMode(seed.getMode());
        initial.setOperationName(seed.getOperationName());
        initial.setModel(seed.getModel().copy());
        initial.setMaxTokens(seed.getMaxTokens());
        initial.setTemperature(seed.getTemperature());
        initial.setTopP(seed.getTopP());
        initial.setToolChoice(seed.getToolChoice());
        initial.setThinkingEnabled(seed.getThinkingEnabled());
        setGenerationSpanAttributes(span, initial);

        return new GenerationRecorder(this, seed, span, startedAt);
    }

    private void flushInternal() {
        synchronized (flushLock) {
            while (true) {
                List<Generation> batch;
                synchronized (queueLock) {
                    if (pendingGenerations.isEmpty()) {
                        return;
                    }
                    int batchSize = Math.max(1, config.getGenerationExport().getBatchSize());
                    int end = Math.min(batchSize, pendingGenerations.size());
                    batch = new ArrayList<>(pendingGenerations.subList(0, end));
                    pendingGenerations.subList(0, end).clear();
                }

                ExportGenerationsRequest request = new ExportGenerationsRequest().setGenerations(batch);
                ExportGenerationsResponse response = exportWithRetry(request);
                for (ExportGenerationResult result : response.getResults()) {
                    if (!result.isAccepted()) {
                        logWarn("sigil generation rejected id=" + result.getGenerationId() + " error=" + result.getError(), null);
                    }
                }
            }
        }
    }

    private ExportGenerationsResponse exportWithRetry(ExportGenerationsRequest request) {
        int attempts = Math.max(0, config.getGenerationExport().getMaxRetries()) + 1;
        long backoffMs = Math.max(1L, config.getGenerationExport().getInitialBackoff().toMillis());
        long maxBackoffMs = Math.max(backoffMs, config.getGenerationExport().getMaxBackoff().toMillis());

        Exception last = null;
        for (int attempt = 0; attempt < attempts; attempt++) {
            try {
                return generationExporter.exportGenerations(request);
            } catch (Exception exception) {
                last = exception;
                if (attempt == attempts - 1) {
                    break;
                }
                try {
                    Thread.sleep(backoffMs);
                } catch (InterruptedException interruptedException) {
                    Thread.currentThread().interrupt();
                    throw new EnqueueException("generation export interrupted", interruptedException);
                }
                backoffMs = Math.min(maxBackoffMs, backoffMs * 2);
            }
        }

        throw new EnqueueException("sigil generation export failed", last);
    }

    private void triggerAsyncFlush() {
        if (!flushScheduled.compareAndSet(false, true)) {
            return;
        }
        flushExecutor.execute(() -> {
            try {
                flushInternal();
            } catch (Exception exception) {
                logWarn("sigil generation export failed", exception);
            } finally {
                flushScheduled.set(false);
            }
        });
    }

    private void assertOpen() {
        if (closed) {
            throw new ClientShutdownException("sigil: client is shutting down");
        }
    }

    private GenerationExporter createGenerationExporter(GenerationExportConfig exportConfig) {
        return switch (exportConfig.getProtocol()) {
            case GRPC -> new GrpcGenerationExporter(exportConfig.getEndpoint(), exportConfig.getHeaders(), exportConfig.isInsecure());
            case HTTP -> new HttpGenerationExporter(exportConfig.getEndpoint(), exportConfig.getHeaders());
            case NONE -> new NoopGenerationExporter();
        };
    }

    private SubmitConversationRatingRequest normalizeConversationRatingRequest(SubmitConversationRatingRequest request) {
        SubmitConversationRatingRequest normalized = request == null ? new SubmitConversationRatingRequest() : request;

        String ratingId = normalized.getRatingId().trim();
        if (ratingId.isBlank()) {
            throw new ValidationException("sigil conversation rating validation failed: ratingId is required");
        }
        if (ratingId.length() > MAX_RATING_ID_LEN) {
            throw new ValidationException("sigil conversation rating validation failed: ratingId is too long");
        }

        String comment = normalized.getComment().trim();
        if (utf8ByteLength(comment) > MAX_RATING_COMMENT_BYTES) {
            throw new ValidationException("sigil conversation rating validation failed: comment is too long");
        }

        String generationId = normalized.getGenerationId().trim();
        if (generationId.length() > MAX_RATING_GENERATION_ID_LEN) {
            throw new ValidationException("sigil conversation rating validation failed: generationId is too long");
        }

        String raterId = normalized.getRaterId().trim();
        if (raterId.length() > MAX_RATING_ACTOR_ID_LEN) {
            throw new ValidationException("sigil conversation rating validation failed: raterId is too long");
        }

        String source = normalized.getSource().trim();
        if (source.length() > MAX_RATING_SOURCE_LEN) {
            throw new ValidationException("sigil conversation rating validation failed: source is too long");
        }

        Map<String, Object> metadata = new LinkedHashMap<>(normalized.getMetadata());
        if (!metadata.isEmpty()) {
            int metadataBytes;
            try {
                metadataBytes = utf8ByteLength(Json.MAPPER.writeValueAsString(metadata));
            } catch (Exception exception) {
                throw new ValidationException("sigil conversation rating validation failed: metadata must be valid JSON");
            }
            if (metadataBytes > MAX_RATING_METADATA_BYTES) {
                throw new ValidationException("sigil conversation rating validation failed: metadata is too large");
            }
        }

        ConversationRatingValue value = normalized.getRating();
        if (value != ConversationRatingValue.GOOD && value != ConversationRatingValue.BAD) {
            throw new ValidationException(
                    "sigil conversation rating validation failed: rating must be CONVERSATION_RATING_VALUE_GOOD or CONVERSATION_RATING_VALUE_BAD");
        }

        return new SubmitConversationRatingRequest()
                .setRatingId(ratingId)
                .setRating(value)
                .setComment(comment)
                .setMetadata(metadata)
                .setGenerationId(generationId)
                .setRaterId(raterId)
                .setSource(source);
    }

    private String conversationRatingEndpoint(ApiConfig apiConfig, GenerationExportConfig exportConfig, String conversationId) {
        String baseUrl = baseUrlFromEndpoint(apiConfig.getEndpoint(), exportConfig.isInsecure());
        return baseUrl + "/api/v1/conversations/" + encodePathSegment(conversationId) + "/ratings";
    }

    private String baseUrlFromEndpoint(String endpoint, boolean insecure) {
        String trimmed = endpoint == null ? "" : endpoint.trim();
        if (trimmed.isBlank()) {
            throw new RatingTransportException("sigil conversation rating transport failed: api endpoint is required");
        }

        if (trimmed.startsWith("http://") || trimmed.startsWith("https://")) {
            URI parsed = URI.create(trimmed);
            if (parsed.getHost() == null || parsed.getHost().isBlank()) {
                throw new RatingTransportException("sigil conversation rating transport failed: api endpoint host is required");
            }
            int port = parsed.getPort();
            String host = parsed.getHost();
            if (port > 0) {
                host += ":" + port;
            }
            return parsed.getScheme() + "://" + host;
        }

        String withoutScheme = trimmed.startsWith("grpc://") ? trimmed.substring("grpc://".length()) : trimmed;
        String host = withoutScheme.split("/", 2)[0].trim();
        if (host.isBlank()) {
            throw new RatingTransportException("sigil conversation rating transport failed: api endpoint host is required");
        }
        return (insecure ? "http://" : "https://") + host;
    }

    private SubmitConversationRatingResponse parseSubmitConversationRatingResponse(String responseBody) {
        try {
            JsonNode payload = Json.MAPPER.readTree(responseBody);
            JsonNode ratingNode = payload.path("rating");
            JsonNode summaryNode = payload.path("summary");
            if (!ratingNode.isObject() || !summaryNode.isObject()) {
                throw new RatingTransportException("sigil conversation rating transport failed: invalid response payload");
            }

            return new SubmitConversationRatingResponse()
                    .setRating(parseConversationRating(ratingNode))
                    .setSummary(parseConversationRatingSummary(summaryNode));
        } catch (RatingTransportException exception) {
            throw exception;
        } catch (Exception exception) {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid response payload", exception);
        }
    }

    private ConversationRating parseConversationRating(JsonNode node) {
        ConversationRatingValue ratingValue;
        try {
            ratingValue = ConversationRatingValue.fromWireValue(requiredString(node, "rating"));
        } catch (IllegalArgumentException exception) {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid rating payload", exception);
        }

        Map<String, Object> metadata = new LinkedHashMap<>();
        JsonNode metadataNode = node.path("metadata");
        if (!metadataNode.isMissingNode() && !metadataNode.isNull()) {
            if (!metadataNode.isObject()) {
                throw new RatingTransportException("sigil conversation rating transport failed: invalid rating payload");
            }
            metadata = Json.MAPPER.convertValue(metadataNode, Json.MAPPER.getTypeFactory().constructMapType(LinkedHashMap.class, String.class, Object.class));
        }

        return new ConversationRating()
                .setRatingId(requiredString(node, "rating_id"))
                .setConversationId(requiredString(node, "conversation_id"))
                .setRating(ratingValue)
                .setCreatedAt(requiredInstant(node, "created_at"))
                .setComment(optionalString(node, "comment"))
                .setMetadata(metadata)
                .setGenerationId(optionalString(node, "generation_id"))
                .setRaterId(optionalString(node, "rater_id"))
                .setSource(optionalString(node, "source"));
    }

    private ConversationRatingSummary parseConversationRatingSummary(JsonNode node) {
        ConversationRatingSummary summary = new ConversationRatingSummary()
                .setTotalCount(requiredInt(node, "total_count"))
                .setGoodCount(requiredInt(node, "good_count"))
                .setBadCount(requiredInt(node, "bad_count"))
                .setLatestRatedAt(requiredInstant(node, "latest_rated_at"))
                .setHasBadRating(requiredBoolean(node, "has_bad_rating"));

        String latestRating = optionalString(node, "latest_rating");
        if (!latestRating.isBlank()) {
            try {
                summary.setLatestRating(ConversationRatingValue.fromWireValue(latestRating));
            } catch (IllegalArgumentException exception) {
                throw new RatingTransportException("sigil conversation rating transport failed: invalid rating summary payload", exception);
            }
        }

        String latestBadAt = optionalString(node, "latest_bad_at");
        if (!latestBadAt.isBlank()) {
            summary.setLatestBadAt(parseInstant(latestBadAt));
        }

        return summary;
    }

    private static String requiredString(JsonNode node, String key) {
        JsonNode value = node.path(key);
        if (!value.isTextual() || value.asText().isBlank()) {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid response payload");
        }
        return value.asText();
    }

    private static String optionalString(JsonNode node, String key) {
        JsonNode value = node.path(key);
        if (!value.isTextual()) {
            return "";
        }
        return value.asText();
    }

    private static int requiredInt(JsonNode node, String key) {
        JsonNode value = node.path(key);
        if (!value.isInt()) {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid response payload");
        }
        return value.asInt();
    }

    private static boolean requiredBoolean(JsonNode node, String key) {
        JsonNode value = node.path(key);
        if (!value.isBoolean()) {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid response payload");
        }
        return value.asBoolean();
    }

    private static Instant requiredInstant(JsonNode node, String key) {
        return parseInstant(requiredString(node, key));
    }

    private static Instant parseInstant(String value) {
        try {
            return Instant.parse(value);
        } catch (Exception exception) {
            throw new RatingTransportException("sigil conversation rating transport failed: invalid timestamp in response payload", exception);
        }
    }

    private static int utf8ByteLength(String value) {
        return value == null ? 0 : value.getBytes(StandardCharsets.UTF_8).length;
    }

    private static String encodePathSegment(String value) {
        return URLEncoder.encode(value, StandardCharsets.UTF_8).replace("+", "%20");
    }

    private static String ratingErrorText(String body, int statusCode) {
        return body == null || body.isBlank() ? "HTTP " + statusCode : body;
    }

    private void logWarn(String message, Throwable error) {
        if (logger == null) {
            return;
        }
        if (error == null) {
            logger.warning(message);
        } else {
            logger.log(Level.WARNING, message, error);
        }
    }

    static String generationSpanName(String operationName, String modelName) {
        return operationName + " " + modelName;
    }

    static String toolSpanName(String toolName) {
        return "execute_tool " + toolName;
    }

    static String defaultOperationName(GenerationMode mode) {
        return mode == GenerationMode.STREAM ? "streamText" : "generateText";
    }

    static String newID(String prefix) {
        return prefix + "-" + UUID.randomUUID().toString().replace("-", "");
    }

    static void setGenerationSpanAttributes(Span span, Generation generation) {
        if (!generation.getId().isBlank()) {
            span.setAttribute(SPAN_ATTR_GENERATION_ID, generation.getId());
        }
        if (!generation.getConversationId().isBlank()) {
            span.setAttribute(SPAN_ATTR_CONVERSATION_ID, generation.getConversationId());
        }
        if (!generation.getAgentName().isBlank()) {
            span.setAttribute(SPAN_ATTR_AGENT_NAME, generation.getAgentName());
        }
        if (!generation.getAgentVersion().isBlank()) {
            span.setAttribute(SPAN_ATTR_AGENT_VERSION, generation.getAgentVersion());
        }
        if (!generation.getOperationName().isBlank()) {
            span.setAttribute(SPAN_ATTR_OPERATION_NAME, generation.getOperationName());
        }
        if (!generation.getModel().getProvider().isBlank()) {
            span.setAttribute(SPAN_ATTR_PROVIDER_NAME, generation.getModel().getProvider());
        }
        if (!generation.getModel().getName().isBlank()) {
            span.setAttribute(SPAN_ATTR_REQUEST_MODEL, generation.getModel().getName());
        }
        if (generation.getMaxTokens() != null) {
            span.setAttribute(SPAN_ATTR_REQUEST_MAX_TOKENS, generation.getMaxTokens());
        }
        if (generation.getTemperature() != null) {
            span.setAttribute(SPAN_ATTR_REQUEST_TEMPERATURE, generation.getTemperature());
        }
        if (generation.getTopP() != null) {
            span.setAttribute(SPAN_ATTR_REQUEST_TOP_P, generation.getTopP());
        }
        if (!generation.getToolChoice().isBlank()) {
            span.setAttribute(SPAN_ATTR_REQUEST_TOOL_CHOICE, generation.getToolChoice());
        }
        if (generation.getThinkingEnabled() != null) {
            span.setAttribute(SPAN_ATTR_REQUEST_THINKING_ENABLED, generation.getThinkingEnabled());
        }
        Long thinkingBudget = thinkingBudgetFromMetadata(generation.getMetadata());
        if (thinkingBudget != null) {
            span.setAttribute(SPAN_ATTR_REQUEST_THINKING_BUDGET, thinkingBudget);
        }
        if (!generation.getResponseId().isBlank()) {
            span.setAttribute(SPAN_ATTR_RESPONSE_ID, generation.getResponseId());
        }
        if (!generation.getResponseModel().isBlank()) {
            span.setAttribute(SPAN_ATTR_RESPONSE_MODEL, generation.getResponseModel());
        }
        if (!generation.getStopReason().isBlank()) {
            span.setAttribute(AttributeKey.stringArrayKey(SPAN_ATTR_FINISH_REASONS), List.of(generation.getStopReason()));
        }
        TokenUsage usage = generation.getUsage();
        if (usage != null) {
            span.setAttribute(SPAN_ATTR_INPUT_TOKENS, usage.getInputTokens());
            span.setAttribute(SPAN_ATTR_OUTPUT_TOKENS, usage.getOutputTokens());
            span.setAttribute(SPAN_ATTR_CACHE_READ_TOKENS, usage.getCacheReadInputTokens());
            span.setAttribute(SPAN_ATTR_CACHE_WRITE_TOKENS, usage.getCacheWriteInputTokens());
            span.setAttribute(SPAN_ATTR_CACHE_CREATION_TOKENS, usage.getCacheCreationInputTokens());
            span.setAttribute(SPAN_ATTR_REASONING_TOKENS, usage.getReasoningTokens());
        }
    }

    static void setToolSpanAttributes(Span span, ToolExecutionStart seed) {
        if (!seed.getConversationId().isBlank()) {
            span.setAttribute(SPAN_ATTR_CONVERSATION_ID, seed.getConversationId());
        }
        if (!seed.getAgentName().isBlank()) {
            span.setAttribute(SPAN_ATTR_AGENT_NAME, seed.getAgentName());
        }
        if (!seed.getAgentVersion().isBlank()) {
            span.setAttribute(SPAN_ATTR_AGENT_VERSION, seed.getAgentVersion());
        }
        if (!seed.getToolName().isBlank()) {
            span.setAttribute(SPAN_ATTR_TOOL_NAME, seed.getToolName());
        }
        if (!seed.getToolCallId().isBlank()) {
            span.setAttribute(SPAN_ATTR_TOOL_CALL_ID, seed.getToolCallId());
        }
        if (!seed.getToolType().isBlank()) {
            span.setAttribute(SPAN_ATTR_TOOL_TYPE, seed.getToolType());
        }
        if (!seed.getToolDescription().isBlank()) {
            span.setAttribute(SPAN_ATTR_TOOL_DESCRIPTION, seed.getToolDescription());
        }
    }

    void recordGenerationMetrics(Generation generation, String errorType, String errorCategory, Instant firstTokenAt) {
        if (generation == null) {
            return;
        }
        if (generation.getStartedAt() == null || generation.getCompletedAt() == null) {
            return;
        }

        double durationSeconds = Math.max(0d, Duration.between(generation.getStartedAt(), generation.getCompletedAt()).toNanos() / 1_000_000_000d);
        operationDurationHistogram.record(
                durationSeconds,
                Attributes.builder()
                        .put(SPAN_ATTR_OPERATION_NAME, operationName(generation))
                        .put(SPAN_ATTR_PROVIDER_NAME, generation.getModel() == null ? "" : generation.getModel().getProvider())
                        .put(SPAN_ATTR_REQUEST_MODEL, generation.getModel() == null ? "" : generation.getModel().getName())
                        .put(SPAN_ATTR_AGENT_NAME, generation.getAgentName())
                        .put(SPAN_ATTR_ERROR_TYPE, errorType == null ? "" : errorType)
                        .put(SPAN_ATTR_ERROR_CATEGORY, errorCategory == null ? "" : errorCategory)
                        .build()
        );

        TokenUsage usage = generation.getUsage();
        if (usage != null) {
            recordTokenUsage(generation, METRIC_TOKEN_TYPE_INPUT, usage.getInputTokens());
            recordTokenUsage(generation, METRIC_TOKEN_TYPE_OUTPUT, usage.getOutputTokens());
            recordTokenUsage(generation, METRIC_TOKEN_TYPE_CACHE_READ, usage.getCacheReadInputTokens());
            recordTokenUsage(generation, METRIC_TOKEN_TYPE_CACHE_WRITE, usage.getCacheWriteInputTokens());
            recordTokenUsage(generation, METRIC_TOKEN_TYPE_CACHE_CREATION, usage.getCacheCreationInputTokens());
            recordTokenUsage(generation, METRIC_TOKEN_TYPE_REASONING, usage.getReasoningTokens());
        }

        toolCallsHistogram.record(
                (double) countToolCalls(generation.getOutput()),
                Attributes.builder()
                        .put(SPAN_ATTR_PROVIDER_NAME, generation.getModel() == null ? "" : generation.getModel().getProvider())
                        .put(SPAN_ATTR_REQUEST_MODEL, generation.getModel() == null ? "" : generation.getModel().getName())
                        .put(SPAN_ATTR_AGENT_NAME, generation.getAgentName())
                        .build()
        );

        if (defaultOperationName(GenerationMode.STREAM).equals(operationName(generation)) && firstTokenAt != null) {
            double ttftSeconds = Duration.between(generation.getStartedAt(), firstTokenAt).toNanos() / 1_000_000_000d;
            if (ttftSeconds >= 0d) {
                ttftHistogram.record(
                        ttftSeconds,
                        Attributes.builder()
                                .put(SPAN_ATTR_PROVIDER_NAME, generation.getModel() == null ? "" : generation.getModel().getProvider())
                                .put(SPAN_ATTR_REQUEST_MODEL, generation.getModel() == null ? "" : generation.getModel().getName())
                                .put(SPAN_ATTR_AGENT_NAME, generation.getAgentName())
                                .build()
                );
            }
        }
    }

    private void recordTokenUsage(Generation generation, String tokenType, long value) {
        if (value == 0L) {
            return;
        }
        tokenUsageHistogram.record(
                (double) value,
                Attributes.builder()
                        .put(SPAN_ATTR_PROVIDER_NAME, generation.getModel() == null ? "" : generation.getModel().getProvider())
                        .put(SPAN_ATTR_REQUEST_MODEL, generation.getModel() == null ? "" : generation.getModel().getName())
                        .put(SPAN_ATTR_AGENT_NAME, generation.getAgentName())
                        .put(METRIC_ATTR_TOKEN_TYPE, tokenType)
                        .build()
        );
    }

    void recordToolExecutionMetrics(ToolExecutionStart seed, Instant startedAt, Instant completedAt, Throwable finalError) {
        if (seed == null || startedAt == null || completedAt == null) {
            return;
        }

        double durationSeconds = Math.max(0d, Duration.between(startedAt, completedAt).toNanos() / 1_000_000_000d);
        String errorType = "";
        String errorCategory = "";
        if (finalError != null) {
            errorType = "tool_execution_error";
            errorCategory = errorCategoryFromThrowable(finalError, true);
        }

        operationDurationHistogram.record(
                durationSeconds,
                Attributes.builder()
                        .put(SPAN_ATTR_OPERATION_NAME, "execute_tool")
                        .put(SPAN_ATTR_PROVIDER_NAME, "")
                        .put(SPAN_ATTR_REQUEST_MODEL, seed.getToolName())
                        .put(SPAN_ATTR_AGENT_NAME, seed.getAgentName())
                        .put(SPAN_ATTR_ERROR_TYPE, errorType)
                        .put(SPAN_ATTR_ERROR_CATEGORY, errorCategory)
                        .build()
        );
    }

    static String errorCategoryFromThrowable(Throwable error, boolean fallbackSdk) {
        if (error == null) {
            return fallbackSdk ? "sdk_error" : "";
        }

        String message = String.valueOf(error.getMessage());
        String lower = message.toLowerCase();
        if (lower.contains("timeout") || lower.contains("deadline exceeded")) {
            return "timeout";
        }

        Integer statusCode = extractStatusCode(error);
        if (statusCode != null) {
            if (statusCode == 429) {
                return "rate_limit";
            }
            if (statusCode == 401 || statusCode == 403) {
                return "auth_error";
            }
            if (statusCode == 408) {
                return "timeout";
            }
            if (statusCode >= 500 && statusCode <= 599) {
                return "server_error";
            }
            if (statusCode >= 400 && statusCode <= 499) {
                return "client_error";
            }
        }

        return fallbackSdk ? "sdk_error" : "";
    }

    private static Integer extractStatusCode(Throwable error) {
        if (error == null) {
            return null;
        }

        Integer byMethod = invokeStatusMethod(error, "statusCode");
        if (byMethod != null) {
            return byMethod;
        }
        byMethod = invokeStatusMethod(error, "status");
        if (byMethod != null) {
            return byMethod;
        }

        Integer byField = readStatusField(error, "statusCode");
        if (byField != null) {
            return byField;
        }
        byField = readStatusField(error, "status");
        if (byField != null) {
            return byField;
        }

        Matcher matcher = STATUS_CODE_PATTERN.matcher(String.valueOf(error.getMessage()));
        while (matcher.find()) {
            try {
                int parsed = Integer.parseInt(matcher.group(1));
                if (parsed >= 100 && parsed <= 599) {
                    return parsed;
                }
            } catch (NumberFormatException ignored) {
                // Continue scanning.
            }
        }
        return null;
    }

    private static Integer invokeStatusMethod(Throwable error, String methodName) {
        try {
            Method method = error.getClass().getMethod(methodName);
            Object value = method.invoke(error);
            return asStatusCode(value);
        } catch (Exception ignored) {
            return null;
        }
    }

    private static Integer readStatusField(Throwable error, String fieldName) {
        Class<?> current = error.getClass();
        while (current != null) {
            try {
                Field field = current.getDeclaredField(fieldName);
                field.setAccessible(true);
                Object value = field.get(error);
                return asStatusCode(value);
            } catch (Exception ignored) {
                current = current.getSuperclass();
            }
        }
        return null;
    }

    private static Integer asStatusCode(Object value) {
        if (value instanceof Number number) {
            int parsed = number.intValue();
            return parsed >= 100 && parsed <= 599 ? parsed : null;
        }
        if (value instanceof String text) {
            try {
                int parsed = Integer.parseInt(text.trim());
                return parsed >= 100 && parsed <= 599 ? parsed : null;
            } catch (NumberFormatException ignored) {
                return null;
            }
        }
        return null;
    }

    private static long countToolCalls(List<Message> messages) {
        long total = 0;
        if (messages == null) {
            return 0;
        }
        for (Message message : messages) {
            if (message == null || message.getParts() == null) {
                continue;
            }
            for (MessagePart part : message.getParts()) {
                if (part != null && part.getKind() == MessagePartKind.TOOL_CALL) {
                    total++;
                }
            }
        }
        return total;
    }

    private static String operationName(Generation generation) {
        return operationName(generation.getOperationName(), generation.getMode());
    }

    private static String operationName(String operationName, GenerationMode mode) {
        if (operationName != null && !operationName.isBlank()) {
            return operationName;
        }
        return defaultOperationName(mode == null ? GenerationMode.SYNC : mode);
    }

    private static Long thinkingBudgetFromMetadata(Map<String, Object> metadata) {
        if (metadata == null) {
            return null;
        }
        Object raw = metadata.get(SPAN_ATTR_REQUEST_THINKING_BUDGET);
        if (raw == null) {
            return null;
        }
        if (raw instanceof Number number) {
            return number.longValue();
        }
        if (raw instanceof String text) {
            try {
                return Long.parseLong(text.trim());
            } catch (NumberFormatException ignored) {
                return null;
            }
        }
        return null;
    }
}
