package com.grafana.sigil.sdk;

import io.opentelemetry.api.GlobalOpenTelemetry;
import io.opentelemetry.api.common.AttributeKey;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.SpanKind;
import io.opentelemetry.api.trace.Tracer;
import io.opentelemetry.context.Scope;
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

/** Sigil Java SDK runtime client. */
public final class SigilClient implements AutoCloseable {
    static final String SPAN_ATTR_GENERATION_ID = "sigil.generation.id";
    static final String SPAN_ATTR_CONVERSATION_ID = "gen_ai.conversation.id";
    static final String SPAN_ATTR_AGENT_NAME = "gen_ai.agent.name";
    static final String SPAN_ATTR_AGENT_VERSION = "gen_ai.agent.version";
    static final String SPAN_ATTR_ERROR_TYPE = "error.type";
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
    static final String SPAN_ATTR_TOOL_NAME = "gen_ai.tool.name";
    static final String SPAN_ATTR_TOOL_CALL_ID = "gen_ai.tool.call.id";
    static final String SPAN_ATTR_TOOL_TYPE = "gen_ai.tool.type";
    static final String SPAN_ATTR_TOOL_DESCRIPTION = "gen_ai.tool.description";
    static final String SPAN_ATTR_TOOL_CALL_ARGUMENTS = "gen_ai.tool.call.arguments";
    static final String SPAN_ATTR_TOOL_CALL_RESULT = "gen_ai.tool.call.result";

    private final SigilClientConfig config;
    private final GenerationExporter generationExporter;
    private final TraceRuntime traceRuntime;
    private final Tracer tracer;
    private final Logger logger;
    private final Clock clock;

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

        TraceConfig traceConfig = config.getTrace();
        traceConfig.setHeaders(AuthHeaders.resolve(traceConfig.getHeaders(), traceConfig.getAuth(), "trace"));

        this.generationExporter = config.getGenerationExporter() == null
                ? createGenerationExporter(exportConfig)
                : config.getGenerationExporter();

        if (config.getTracer() != null) {
            this.traceRuntime = new TraceRuntime() {
                @Override
                public Tracer tracer() {
                    return config.getTracer();
                }

                @Override
                public void flush() {
                }

                @Override
                public void shutdown() {
                }
            };
            this.tracer = config.getTracer();
        } else {
            TraceRuntime runtime;
            try {
                runtime = OtelTraceRuntime.fromConfig(traceConfig);
            } catch (Exception exception) {
                logWarn("sigil trace exporter init failed", exception);
                runtime = new TraceRuntime() {
                    private final Tracer fallback = GlobalOpenTelemetry.getTracer("github.com/grafana/sigil/sdks/java");

                    @Override
                    public Tracer tracer() {
                        return fallback;
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
            this.tracer = runtime.tracer();
        }

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
        return exportConfig.getProtocol() == GenerationExportProtocol.GRPC
                ? new GrpcGenerationExporter(exportConfig.getEndpoint(), exportConfig.getHeaders(), exportConfig.isInsecure())
                : new HttpGenerationExporter(exportConfig.getEndpoint(), exportConfig.getHeaders());
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
