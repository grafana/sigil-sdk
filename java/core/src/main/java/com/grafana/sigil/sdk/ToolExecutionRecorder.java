package com.grafana.sigil.sdk;

import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.StatusCode;
import java.time.Instant;
import java.util.Optional;
import java.util.concurrent.TimeUnit;

/** Recorder for one tool execution lifecycle. */
public class ToolExecutionRecorder implements AutoCloseable {
    private final SigilClient client;
    private final ToolExecutionStart seed;
    private final Span span;
    private final Instant startedAt;

    private final Object lock = new Object();
    private boolean ended;
    private Throwable callError;
    private ToolExecutionResult result;
    private Throwable finalError;

    ToolExecutionRecorder(SigilClient client, ToolExecutionStart seed, Span span, Instant startedAt) {
        this.client = client;
        this.seed = seed;
        this.span = span;
        this.startedAt = startedAt;
    }

    protected ToolExecutionRecorder() {
        this.client = null;
        this.seed = null;
        this.span = null;
        this.startedAt = null;
    }

    /** Sets tool execution arguments/result payload. */
    public void setResult(ToolExecutionResult result) {
        synchronized (lock) {
            this.result = result == null ? null : result.copy();
        }
    }

    /** Records a tool execution error for this lifecycle. */
    public void setCallError(Throwable error) {
        if (error == null) {
            return;
        }
        synchronized (lock) {
            this.callError = error;
        }
    }

    /** Finalizes the tool execution lifecycle. Safe to call multiple times. */
    public void end() {
        ToolExecutionResult snapshotResult;
        Throwable snapshotCallError;

        synchronized (lock) {
            if (ended) {
                return;
            }
            ended = true;
            snapshotResult = result == null ? new ToolExecutionResult() : result.copy();
            snapshotCallError = callError;
        }

        Instant completedAt = snapshotResult.getCompletedAt() == null ? client.now() : snapshotResult.getCompletedAt();

        ToolExecution execution = new ToolExecution()
                .setToolName(seed.getToolName())
                .setToolCallId(seed.getToolCallId())
                .setToolType(seed.getToolType())
                .setToolDescription(seed.getToolDescription())
                .setConversationId(seed.getConversationId())
                .setAgentName(seed.getAgentName())
                .setAgentVersion(seed.getAgentVersion())
                .setIncludeContent(seed.isIncludeContent())
                .setStartedAt(startedAt)
                .setCompletedAt(completedAt)
                .setArguments(snapshotResult.getArguments())
                .setResult(snapshotResult.getResult())
                .setCallError(snapshotCallError == null ? "" : String.valueOf(snapshotCallError.getMessage()));

        if (seed.isIncludeContent()) {
            try {
                if (snapshotResult.getArguments() != null) {
                    span.setAttribute(SigilClient.SPAN_ATTR_TOOL_CALL_ARGUMENTS, Json.MAPPER.writeValueAsString(snapshotResult.getArguments()));
                }
                if (snapshotResult.getResult() != null) {
                    span.setAttribute(SigilClient.SPAN_ATTR_TOOL_CALL_RESULT, Json.MAPPER.writeValueAsString(snapshotResult.getResult()));
                }
            } catch (Exception exception) {
                snapshotCallError = snapshotCallError == null ? exception : snapshotCallError;
            }
        }

        if (snapshotCallError != null) {
            span.recordException(snapshotCallError);
            span.setAttribute(SigilClient.SPAN_ATTR_ERROR_TYPE, "tool_execution_error");
            span.setStatus(StatusCode.ERROR, String.valueOf(snapshotCallError.getMessage()));
        } else {
            span.setStatus(StatusCode.OK);
        }

        span.end(completedAt.toEpochMilli(), TimeUnit.MILLISECONDS);
        client.recordToolExecution(execution);

        synchronized (lock) {
            finalError = snapshotCallError;
        }
    }

    /** Returns local SDK errors from tool span finalization. */
    public Optional<Throwable> error() {
        synchronized (lock) {
            return Optional.ofNullable(finalError);
        }
    }

    @Override
    public void close() {
        end();
    }

    private static final class NoopToolExecutionRecorder extends ToolExecutionRecorder {
        private static final NoopToolExecutionRecorder INSTANCE = new NoopToolExecutionRecorder();

        @Override
        public void setResult(ToolExecutionResult result) {
        }

        @Override
        public void setCallError(Throwable error) {
        }

        @Override
        public void end() {
        }

        @Override
        public Optional<Throwable> error() {
            return Optional.empty();
        }

        @Override
        public void close() {
        }
    }

    static final ToolExecutionRecorder INSTANCE_NOOP = NoopToolExecutionRecorder.INSTANCE;
}
