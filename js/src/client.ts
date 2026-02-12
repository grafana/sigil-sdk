import { defaultLogger, mergeConfig } from './config.js';
import { createDefaultGenerationExporter } from './exporters/default.js';
import { SpanKind, SpanStatusCode, type Span, type Tracer } from '@opentelemetry/api';
import { createTraceRuntime, type TraceRuntime } from './tracing.js';
import type {
  Generation,
  GenerationExporter,
  GenerationMode,
  GenerationRecorder,
  GenerationResult,
  RecorderCallback,
  RecorderWithError,
  SigilDebugSnapshot,
  SigilLogger,
  SigilSdkConfig,
  SigilSdkConfigInput,
  ToolExecution,
  ToolExecutionRecorder,
  ToolExecutionResult,
  ToolExecutionStart,
  GenerationStart,
} from './types.js';
import {
  asError,
  cloneGeneration,
  cloneGenerationResult,
  cloneModelRef,
  cloneToolDefinition,
  cloneMessage,
  cloneArtifact,
  cloneToolExecution,
  cloneToolExecutionResult,
  defaultOperationNameForMode,
  defaultSleep,
  encodedSizeBytes,
  maybeUnref,
  newLocalID,
  validateGeneration,
  validateToolExecution,
} from './utils.js';

const spanAttrGenerationID = 'sigil.generation.id';
const spanAttrConversationID = 'gen_ai.conversation.id';
const spanAttrAgentName = 'gen_ai.agent.name';
const spanAttrAgentVersion = 'gen_ai.agent.version';
const spanAttrErrorType = 'error.type';
const spanAttrOperationName = 'gen_ai.operation.name';
const spanAttrProviderName = 'gen_ai.provider.name';
const spanAttrRequestModel = 'gen_ai.request.model';
const spanAttrResponseID = 'gen_ai.response.id';
const spanAttrResponseModel = 'gen_ai.response.model';
const spanAttrFinishReasons = 'gen_ai.response.finish_reasons';
const spanAttrInputTokens = 'gen_ai.usage.input_tokens';
const spanAttrOutputTokens = 'gen_ai.usage.output_tokens';
const spanAttrCacheReadTokens = 'gen_ai.usage.cache_read_input_tokens';
const spanAttrCacheWriteTokens = 'gen_ai.usage.cache_write_input_tokens';
const spanAttrToolName = 'gen_ai.tool.name';
const spanAttrToolCallID = 'gen_ai.tool.call.id';
const spanAttrToolType = 'gen_ai.tool.type';
const spanAttrToolDescription = 'gen_ai.tool.description';
const spanAttrToolCallArguments = 'gen_ai.tool.call.arguments';
const spanAttrToolCallResult = 'gen_ai.tool.call.result';

export class SigilClient {
  private readonly config: SigilSdkConfig;
  private readonly nowFn: () => Date;
  private readonly sleepFn: (durationMs: number) => Promise<void>;
  private readonly logger: SigilLogger;
  private readonly generationExporter: GenerationExporter;
  private readonly traceRuntime: TraceRuntime;
  private readonly tracer: Tracer;
  private readonly generations: Generation[] = [];
  private readonly toolExecutions: ToolExecution[] = [];
  private readonly pendingGenerations: Generation[] = [];

  private flushPromise: Promise<void> | undefined;
  private flushRequested = false;
  private flushTimer: ReturnType<typeof setInterval> | undefined;
  private shutdownPromise: Promise<void> | undefined;
  private shuttingDown = false;
  private closed = false;

  /**
   * Creates a Sigil SDK client.
   *
   * `inputConfig` is merged with defaults. Both trace and generation exporters
   * are initialized during construction.
   */
  constructor(inputConfig: SigilSdkConfigInput = {}) {
    this.config = mergeConfig(inputConfig);
    this.nowFn = this.config.now ?? (() => new Date());
    this.sleepFn = this.config.sleep ?? defaultSleep;
    this.logger = this.config.logger ?? defaultLogger;
    this.generationExporter = this.config.generationExporter ?? createDefaultGenerationExporter(this.config.generationExport);
    if (this.config.tracer !== undefined) {
      this.tracer = this.config.tracer;
      this.traceRuntime = {
        tracer: this.config.tracer,
        async flush() {},
        async shutdown() {},
      };
    } else {
      this.traceRuntime = createTraceRuntime(this.config.trace, (message, error) => {
        this.logWarn(message, error);
      });
      this.tracer = this.traceRuntime.tracer;
    }

    if (this.config.generationExport.flushIntervalMs > 0) {
      this.flushTimer = setInterval(() => {
        this.triggerAsyncFlush();
      }, this.config.generationExport.flushIntervalMs);
      maybeUnref(this.flushTimer);
    }
  }

  /**
   * Starts a generation recorder (`SYNC` mode).
   *
   * Overloads:
   * - returns recorder for manual lifecycle
   * - executes callback and auto-ends recorder
   */
  startGeneration(start: GenerationStart): GenerationRecorder;
  startGeneration<TResult>(
    start: GenerationStart,
    callback: RecorderCallback<GenerationRecorder, TResult>
  ): Promise<TResult>;
  startGeneration<TResult>(
    start: GenerationStart,
    callback?: RecorderCallback<GenerationRecorder, TResult>
  ): GenerationRecorder | Promise<TResult> {
    return this.startGenerationWithMode(start, 'SYNC', callback);
  }

  /**
   * Starts a streaming generation recorder (`STREAM` mode).
   *
   * Overloads:
   * - returns recorder for manual lifecycle
   * - executes callback and auto-ends recorder
   */
  startStreamingGeneration(start: GenerationStart): GenerationRecorder;
  startStreamingGeneration<TResult>(
    start: GenerationStart,
    callback: RecorderCallback<GenerationRecorder, TResult>
  ): Promise<TResult>;
  startStreamingGeneration<TResult>(
    start: GenerationStart,
    callback?: RecorderCallback<GenerationRecorder, TResult>
  ): GenerationRecorder | Promise<TResult> {
    return this.startGenerationWithMode(start, 'STREAM', callback);
  }

  /**
   * Starts a tool execution recorder.
   *
   * Empty tool names return a no-op recorder to keep instrumentation safe.
   */
  startToolExecution(start: ToolExecutionStart): ToolExecutionRecorder;
  startToolExecution<TResult>(
    start: ToolExecutionStart,
    callback: RecorderCallback<ToolExecutionRecorder, TResult>
  ): Promise<TResult>;
  startToolExecution<TResult>(
    start: ToolExecutionStart,
    callback?: RecorderCallback<ToolExecutionRecorder, TResult>
  ): ToolExecutionRecorder | Promise<TResult> {
    this.assertOpen();
    const recorder: ToolExecutionRecorder =
      start.toolName.trim().length === 0 ? new NoopToolExecutionRecorder() : new ToolExecutionRecorderImpl(this, start);
    if (callback === undefined) {
      return recorder;
    }
    return runWithRecorder(recorder, callback);
  }

  /** Forces immediate drain of queued generation exports. */
  async flush(): Promise<void> {
    this.assertOpen();
    await this.flushInternal();
  }

  /** Flushes pending generations and shuts down generation + trace exporters. */
  async shutdown(): Promise<void> {
    if (this.shutdownPromise !== undefined) {
      await this.shutdownPromise;
      return;
    }

    this.shuttingDown = true;
    this.shutdownPromise = (async () => {
      this.stopFlushTimer();
      try {
        await this.flushInternal();
      } catch (error) {
        this.logWarn('sigil generation export flush on shutdown failed', error);
      }

      try {
        await this.generationExporter.shutdown?.();
      } catch (error) {
        this.logWarn('sigil generation exporter shutdown failed', error);
      }

      try {
        await this.traceRuntime.flush();
      } catch (error) {
        this.logWarn('sigil trace provider flush on shutdown failed', error);
      }

      try {
        await this.traceRuntime.shutdown();
      } catch (error) {
        this.logWarn('sigil trace provider shutdown failed', error);
      }

      this.closed = true;
    })();

    await this.shutdownPromise;
  }

  /** Returns a cloned in-memory snapshot for debugging and tests. */
  debugSnapshot(): SigilDebugSnapshot {
    return {
      generations: this.generations.map(cloneGeneration),
      toolExecutions: this.toolExecutions.map(cloneToolExecution),
      queueSize: this.pendingGenerations.length,
    };
  }

  internalNow(): Date {
    return this.nowFn();
  }

  internalRecordGeneration(generation: Generation): void {
    this.generations.push(cloneGeneration(generation));
  }

  internalRecordToolExecution(toolExecution: ToolExecution): void {
    this.toolExecutions.push(cloneToolExecution(toolExecution));
  }

  internalEnqueueGeneration(generation: Generation): void {
    if (this.shuttingDown || this.closed) {
      throw new Error('sigil client is shutdown');
    }

    const payloadMaxBytes = this.config.generationExport.payloadMaxBytes;
    if (payloadMaxBytes > 0) {
      const payloadBytes = encodedSizeBytes(generation);
      if (payloadBytes > payloadMaxBytes) {
        throw new Error(`generation payload exceeds max bytes (${payloadBytes} > ${payloadMaxBytes})`);
      }
    }

    const queueSize = Math.max(1, this.config.generationExport.queueSize);
    if (this.pendingGenerations.length >= queueSize) {
      throw new Error('generation queue is full');
    }

    this.pendingGenerations.push(cloneGeneration(generation));

    const batchSize = Math.max(1, this.config.generationExport.batchSize);
    if (this.pendingGenerations.length >= batchSize) {
      this.triggerAsyncFlush();
    }
  }

  internalLogWarn(message: string, error?: unknown): void {
    this.logWarn(message, error);
  }

  internalStartGenerationSpan(seed: GenerationStart, mode: GenerationMode, startedAt: Date): Span {
    const operationName = seed.operationName ?? defaultOperationNameForMode(mode);
    const span = this.tracer.startSpan(generationSpanName(operationName, seed.model.name), {
      kind: SpanKind.CLIENT,
      startTime: startedAt,
    });

    setGenerationSpanAttributes(span, {
      id: seed.id,
      conversationId: seed.conversationId,
      agentName: seed.agentName,
      agentVersion: seed.agentVersion,
      operationName,
      model: seed.model,
    });

    return span;
  }

  internalStartToolExecutionSpan(seed: ToolExecutionStart, startedAt: Date): Span {
    const span = this.tracer.startSpan(toolSpanName(seed.toolName), {
      kind: SpanKind.INTERNAL,
      startTime: startedAt,
    });

    setToolSpanAttributes(span, seed);
    return span;
  }

  internalApplyTraceContextFromSpan(span: Span, generation: Generation): void {
    const context = span.spanContext();
    if (context.traceId.length > 0) {
      generation.traceId = context.traceId;
    }
    if (context.spanId.length > 0) {
      generation.spanId = context.spanId;
    }
  }

  internalFinalizeGenerationSpan(
    span: Span,
    generation: Generation,
    callError: string | undefined,
    validationError: Error | undefined,
    enqueueError: Error | undefined
  ): void {
    span.updateName(generationSpanName(generation.operationName, generation.model.name));
    setGenerationSpanAttributes(span, generation);

    if (callError !== undefined) {
      span.recordException(new Error(callError));
    }
    if (validationError !== undefined) {
      span.recordException(validationError);
    }
    if (enqueueError !== undefined) {
      span.recordException(enqueueError);
    }

    if (callError !== undefined) {
      span.setAttribute(spanAttrErrorType, 'provider_call_error');
      span.setStatus({ code: SpanStatusCode.ERROR, message: callError });
    } else if (validationError !== undefined) {
      span.setAttribute(spanAttrErrorType, 'validation_error');
      span.setStatus({ code: SpanStatusCode.ERROR, message: validationError.message });
    } else if (enqueueError !== undefined) {
      span.setAttribute(spanAttrErrorType, 'enqueue_error');
      span.setStatus({ code: SpanStatusCode.ERROR, message: enqueueError.message });
    } else {
      span.setStatus({ code: SpanStatusCode.OK });
    }

    span.end(generation.completedAt);
  }

  internalFinalizeToolExecutionSpan(span: Span, toolExecution: ToolExecution, localError: Error | undefined): Error | undefined {
    setToolSpanAttributes(span, toolExecution);

    if (toolExecution.includeContent) {
      const argumentsResult = serializeToolContent(toolExecution.arguments);
      if (argumentsResult.error !== undefined && localError === undefined) {
        localError = argumentsResult.error;
      } else if (argumentsResult.value !== undefined) {
        span.setAttribute(spanAttrToolCallArguments, argumentsResult.value);
      }

      const resultValue = serializeToolContent(toolExecution.result);
      if (resultValue.error !== undefined && localError === undefined) {
        localError = resultValue.error;
      } else if (resultValue.value !== undefined) {
        span.setAttribute(spanAttrToolCallResult, resultValue.value);
      }
    }

    if (toolExecution.callError !== undefined) {
      span.recordException(new Error(toolExecution.callError));
      span.setAttribute(spanAttrErrorType, 'tool_execution_error');
      span.setStatus({ code: SpanStatusCode.ERROR, message: toolExecution.callError });
    } else if (localError !== undefined) {
      span.recordException(localError);
      span.setAttribute(spanAttrErrorType, 'tool_execution_error');
      span.setStatus({ code: SpanStatusCode.ERROR, message: localError.message });
    } else {
      span.setStatus({ code: SpanStatusCode.OK });
    }

    span.end(toolExecution.completedAt);
    return localError;
  }

  private assertOpen(): void {
    if (this.shuttingDown || this.closed) {
      throw new Error('sigil client is shutdown');
    }
  }

  private startGenerationWithMode<TResult>(
    start: GenerationStart,
    mode: GenerationMode,
    callback?: RecorderCallback<GenerationRecorder, TResult>
  ): GenerationRecorder | Promise<TResult> {
    this.assertOpen();
    const recorder = new GenerationRecorderImpl(this, start, mode);
    if (callback === undefined) {
      return recorder;
    }
    return runWithRecorder(recorder, callback);
  }

  private triggerAsyncFlush(): void {
    void this.flushInternal().catch((error) => {
      this.logWarn('sigil generation export failed', error);
    });
  }

  private flushInternal(): Promise<void> {
    if (this.flushPromise !== undefined) {
      this.flushRequested = true;
      return this.flushPromise;
    }

    this.flushPromise = this.drainPendingGenerations().finally(() => {
      this.flushPromise = undefined;
    });

    return this.flushPromise;
  }

  private async drainPendingGenerations(): Promise<void> {
    do {
      this.flushRequested = false;

      while (this.pendingGenerations.length > 0) {
        const batchSize = Math.max(1, this.config.generationExport.batchSize);
        const batch = this.pendingGenerations.splice(0, batchSize).map(cloneGeneration);
        await this.exportWithRetry(batch);
      }
    } while (this.flushRequested || this.pendingGenerations.length > 0);
  }

  private async exportWithRetry(generations: Generation[]): Promise<void> {
    const maxRetries = Math.max(0, this.config.generationExport.maxRetries);
    const attempts = maxRetries + 1;
    const baseBackoffMs =
      this.config.generationExport.initialBackoffMs > 0 ? this.config.generationExport.initialBackoffMs : 100;
    const maxBackoffMs =
      this.config.generationExport.maxBackoffMs > 0 ? this.config.generationExport.maxBackoffMs : baseBackoffMs;

    let backoffMs = baseBackoffMs;
    let lastError: Error | undefined;

    for (let attempt = 0; attempt < attempts; attempt++) {
      try {
        const response = await this.generationExporter.exportGenerations({
          generations: generations.map(cloneGeneration),
        });
        this.logRejectedResults(response.results);
        return;
      } catch (error) {
        lastError = asError(error);
        if (attempt === attempts - 1) {
          break;
        }

        await this.sleepFn(backoffMs);
        backoffMs = Math.min(backoffMs * 2, maxBackoffMs);
      }
    }

    throw lastError ?? new Error('generation export failed');
  }

  private logRejectedResults(results: Array<{ generationId: string; accepted: boolean; error?: string }>): void {
    for (const result of results) {
      if (!result.accepted) {
        this.logWarn(`sigil generation rejected id=${result.generationId}`, result.error);
      }
    }
  }

  private stopFlushTimer(): void {
    if (this.flushTimer !== undefined) {
      clearInterval(this.flushTimer);
      this.flushTimer = undefined;
    }
  }

  private logWarn(message: string, error?: unknown): void {
    if (error === undefined) {
      this.logger.warn?.(message);
      return;
    }
    this.logger.warn?.(`${message}: ${asError(error).message}`);
  }
}

class GenerationRecorderImpl implements GenerationRecorder {
  private readonly startedAt: Date;
  private readonly mode: GenerationMode;
  private readonly span: Span;
  private ended = false;
  private result?: GenerationResult;
  private callError?: string;
  private localError?: Error;

  constructor(
    private readonly client: SigilClient,
    private readonly seed: GenerationStart,
    defaultMode: GenerationMode
  ) {
    this.mode = seed.mode ?? defaultMode;
    this.startedAt = seed.startedAt ?? this.client.internalNow();
    this.span = this.client.internalStartGenerationSpan(seed, this.mode, this.startedAt);
  }

  setResult(result: GenerationResult): void {
    if (this.ended) {
      return;
    }
    this.result = cloneGenerationResult(result);
  }

  setCallError(error: unknown): void {
    if (this.ended) {
      return;
    }
    this.callError = asError(error).message;
  }

  end(): void {
    if (this.ended) {
      return;
    }
    this.ended = true;

    const generation: Generation = {
      id: this.seed.id ?? newLocalID('gen'),
      conversationId: this.result?.conversationId ?? this.seed.conversationId,
      agentName: this.result?.agentName ?? this.seed.agentName,
      agentVersion: this.result?.agentVersion ?? this.seed.agentVersion,
      mode: this.mode,
      operationName: this.result?.operationName ?? this.seed.operationName ?? defaultOperationNameForMode(this.mode),
      model: cloneModelRef(this.seed.model),
      systemPrompt: this.seed.systemPrompt,
      responseId: this.result?.responseId,
      responseModel: this.result?.responseModel,
      input: this.result?.input?.map(cloneMessage),
      output: this.result?.output?.map(cloneMessage),
      tools: this.result?.tools?.map(cloneToolDefinition) ?? this.seed.tools?.map(cloneToolDefinition),
      usage: this.result?.usage ? { ...this.result.usage } : undefined,
      stopReason: this.result?.stopReason,
      startedAt: new Date(this.startedAt),
      completedAt: new Date(this.result?.completedAt ?? this.client.internalNow()),
      tags: this.result?.tags ? { ...this.result.tags } : this.seed.tags ? { ...this.seed.tags } : undefined,
      metadata: this.result?.metadata
        ? { ...this.result.metadata }
        : this.seed.metadata
          ? { ...this.seed.metadata }
          : undefined,
      artifacts: this.result?.artifacts?.map(cloneArtifact),
      callError: this.callError,
    };

    if (this.callError !== undefined) {
      if (generation.metadata === undefined) {
        generation.metadata = {};
      }
      generation.metadata.call_error = this.callError;
    }

    this.client.internalApplyTraceContextFromSpan(this.span, generation);
    this.client.internalRecordGeneration(generation);

    const validationError = validateGeneration(generation);
    let enqueueError: Error | undefined;
    if (validationError !== undefined) {
      this.localError = validationError;
      this.client.internalLogWarn('sigil generation validation failed', validationError);
    } else {
      try {
        this.client.internalEnqueueGeneration(generation);
      } catch (error) {
        enqueueError = asError(error);
        this.localError = enqueueError;
        this.client.internalLogWarn('sigil generation enqueue failed', enqueueError);
      }
    }

    this.client.internalFinalizeGenerationSpan(this.span, generation, this.callError, validationError, enqueueError);
  }

  getError(): Error | undefined {
    return this.localError;
  }
}

class ToolExecutionRecorderImpl implements ToolExecutionRecorder {
  private readonly startedAt: Date;
  private readonly span: Span;
  private ended = false;
  private result?: ToolExecutionResult;
  private callError?: string;
  private localError?: Error;

  constructor(
    private readonly client: SigilClient,
    private readonly seed: ToolExecutionStart
  ) {
    this.startedAt = seed.startedAt ?? this.client.internalNow();
    this.span = this.client.internalStartToolExecutionSpan(seed, this.startedAt);
  }

  setResult(result: ToolExecutionResult): void {
    if (this.ended) {
      return;
    }
    this.result = cloneToolExecutionResult(result);
  }

  setCallError(error: unknown): void {
    if (this.ended) {
      return;
    }
    this.localError = asError(error);
    this.callError = this.localError.message;
  }

  end(): void {
    if (this.ended) {
      return;
    }
    this.ended = true;

    const toolExecution: ToolExecution = {
      toolName: this.seed.toolName,
      toolCallId: this.seed.toolCallId,
      toolType: this.seed.toolType,
      toolDescription: this.seed.toolDescription,
      conversationId: this.seed.conversationId,
      agentName: this.seed.agentName,
      agentVersion: this.seed.agentVersion,
      includeContent: this.seed.includeContent ?? false,
      startedAt: new Date(this.startedAt),
      completedAt: new Date(this.result?.completedAt ?? this.client.internalNow()),
      arguments: this.result?.arguments,
      result: this.result?.result,
      callError: this.callError,
    };

    const validationError = validateToolExecution(toolExecution);
    if (validationError !== undefined) {
      this.localError = validationError;
      this.client.internalLogWarn('sigil tool execution validation failed', validationError);
    } else {
      this.client.internalRecordToolExecution(toolExecution);
    }
    this.localError = this.client.internalFinalizeToolExecutionSpan(this.span, toolExecution, this.localError);
  }

  getError(): Error | undefined {
    return this.localError;
  }
}

class NoopToolExecutionRecorder implements ToolExecutionRecorder {
  setResult(_result: ToolExecutionResult): void {}

  setCallError(_error: unknown): void {}

  end(): void {}

  getError(): Error | undefined {
    return undefined;
  }
}

async function runWithRecorder<TRecorder extends RecorderWithError, TResult>(
  recorder: TRecorder,
  callback: RecorderCallback<TRecorder, TResult>
): Promise<TResult> {
  let callbackError: unknown;
  try {
    return await callback(recorder);
  } catch (error) {
    callbackError = error;
    recorder.setCallError(error);
    throw error;
  } finally {
    recorder.end();
    const recorderError = recorder.getError();
    if (callbackError === undefined && recorderError !== undefined) {
      throw recorderError;
    }
  }
}

function generationSpanName(operationName: string, modelName: string): string {
  const operation = operationName.trim();
  const model = modelName.trim();
  if (model.length === 0) {
    return operation;
  }
  return `${operation} ${model}`;
}

function toolSpanName(toolName: string): string {
  const normalized = toolName.trim();
  if (normalized.length === 0) {
    return 'execute_tool unknown';
  }
  return `execute_tool ${normalized}`;
}

function setGenerationSpanAttributes(
  span: Span,
  generation: {
    id?: string;
    conversationId?: string;
    agentName?: string;
    agentVersion?: string;
    operationName: string;
    model: { provider: string; name: string };
    responseId?: string;
    responseModel?: string;
    stopReason?: string;
    usage?: {
      inputTokens?: number;
      outputTokens?: number;
      cacheReadInputTokens?: number;
      cacheWriteInputTokens?: number;
    };
  }
): void {
  span.setAttribute(spanAttrOperationName, generation.operationName);

  if (notEmpty(generation.id)) {
    span.setAttribute(spanAttrGenerationID, generation.id);
  }
  if (notEmpty(generation.conversationId)) {
    span.setAttribute(spanAttrConversationID, generation.conversationId);
  }
  if (notEmpty(generation.agentName)) {
    span.setAttribute(spanAttrAgentName, generation.agentName);
  }
  if (notEmpty(generation.agentVersion)) {
    span.setAttribute(spanAttrAgentVersion, generation.agentVersion);
  }
  if (notEmpty(generation.model.provider)) {
    span.setAttribute(spanAttrProviderName, generation.model.provider);
  }
  if (notEmpty(generation.model.name)) {
    span.setAttribute(spanAttrRequestModel, generation.model.name);
  }
  if (notEmpty(generation.responseId)) {
    span.setAttribute(spanAttrResponseID, generation.responseId);
  }
  if (notEmpty(generation.responseModel)) {
    span.setAttribute(spanAttrResponseModel, generation.responseModel);
  }
  if (notEmpty(generation.stopReason)) {
    span.setAttribute(spanAttrFinishReasons, [generation.stopReason]);
  }

  const usage = generation.usage;
  if (usage === undefined) {
    return;
  }
  if ((usage.inputTokens ?? 0) !== 0) {
    span.setAttribute(spanAttrInputTokens, usage.inputTokens ?? 0);
  }
  if ((usage.outputTokens ?? 0) !== 0) {
    span.setAttribute(spanAttrOutputTokens, usage.outputTokens ?? 0);
  }
  if ((usage.cacheReadInputTokens ?? 0) !== 0) {
    span.setAttribute(spanAttrCacheReadTokens, usage.cacheReadInputTokens ?? 0);
  }
  if ((usage.cacheWriteInputTokens ?? 0) !== 0) {
    span.setAttribute(spanAttrCacheWriteTokens, usage.cacheWriteInputTokens ?? 0);
  }
}

function setToolSpanAttributes(
  span: Span,
  tool: {
    toolName: string;
    toolCallId?: string;
    toolType?: string;
    toolDescription?: string;
    conversationId?: string;
    agentName?: string;
    agentVersion?: string;
  }
): void {
  span.setAttribute(spanAttrOperationName, 'execute_tool');
  span.setAttribute(spanAttrToolName, tool.toolName);

  if (notEmpty(tool.toolCallId)) {
    span.setAttribute(spanAttrToolCallID, tool.toolCallId);
  }
  if (notEmpty(tool.toolType)) {
    span.setAttribute(spanAttrToolType, tool.toolType);
  }
  if (notEmpty(tool.toolDescription)) {
    span.setAttribute(spanAttrToolDescription, tool.toolDescription);
  }
  if (notEmpty(tool.conversationId)) {
    span.setAttribute(spanAttrConversationID, tool.conversationId);
  }
  if (notEmpty(tool.agentName)) {
    span.setAttribute(spanAttrAgentName, tool.agentName);
  }
  if (notEmpty(tool.agentVersion)) {
    span.setAttribute(spanAttrAgentVersion, tool.agentVersion);
  }
}

function serializeToolContent(value: unknown): { value?: string; error?: Error } {
  if (value === undefined || value === null) {
    return {};
  }

  if (typeof value === 'string') {
    const trimmed = value.trim();
    if (trimmed.length === 0) {
      return {};
    }
    if (isJSON(trimmed)) {
      return { value: trimmed };
    }

    try {
      return { value: JSON.stringify(trimmed) };
    } catch (error) {
      return { error: asError(error) };
    }
  }

  try {
    const encoded = JSON.stringify(value);
    if (encoded === undefined || encoded === 'null') {
      return {};
    }
    return { value: encoded };
  } catch (error) {
    return { error: asError(error) };
  }
}

function isJSON(value: string): boolean {
  try {
    JSON.parse(value);
    return true;
  } catch {
    return false;
  }
}

function notEmpty(value: string | undefined): value is string {
  return value !== undefined && value.trim().length > 0;
}
