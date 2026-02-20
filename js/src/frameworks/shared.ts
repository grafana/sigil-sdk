import type { GenerationRecorder, GenerationResult, Message, TokenUsage } from '../types.js';
import type { SigilClient } from '../client.js';

type AnyRecord = Record<string, unknown>;

type ProviderResolverFn = (context: {
  modelName: string;
  serialized?: unknown;
  invocationParams?: unknown;
}) => string;

export interface FrameworkHandlerOptions {
  agentName?: string;
  agentVersion?: string;
  providerResolver?: 'auto' | ProviderResolverFn;
  provider?: string;
  captureInputs?: boolean;
  captureOutputs?: boolean;
  extraTags?: Record<string, string>;
  extraMetadata?: Record<string, unknown>;
}

interface RunState {
  recorder: GenerationRecorder;
  input: Message[];
  captureOutputs: boolean;
  outputChunks: string[];
}

export class SigilFrameworkHandler {
  private readonly runs = new Map<string, RunState>();
  private readonly agentName?: string;
  private readonly agentVersion?: string;
  private readonly providerResolver: 'auto' | ProviderResolverFn;
  private readonly provider?: string;
  private readonly captureInputs: boolean;
  private readonly captureOutputs: boolean;
  private readonly extraTags: Record<string, string>;
  private readonly extraMetadata: Record<string, unknown>;

  constructor(
    private readonly client: SigilClient,
    private readonly frameworkName: 'langchain' | 'langgraph',
    private readonly frameworkLanguage: 'javascript',
    options: FrameworkHandlerOptions = {}
  ) {
    this.agentName = options.agentName;
    this.agentVersion = options.agentVersion;
    this.providerResolver = options.providerResolver ?? 'auto';
    this.provider = options.provider;
    this.captureInputs = options.captureInputs ?? true;
    this.captureOutputs = options.captureOutputs ?? true;
    this.extraTags = { ...(options.extraTags ?? {}) };
    this.extraMetadata = { ...(options.extraMetadata ?? {}) };
  }

  protected onLLMStart(
    serialized: unknown,
    prompts: unknown,
    runId: string,
    extraParams?: AnyRecord
  ): void {
    const runKey = String(runId);
    if (runKey.length === 0 || this.runs.has(runKey)) {
      return;
    }

    const invocationParams = asRecord(extraParams?.invocation_params);
    const modelName = resolveModelName(serialized, invocationParams);
    const provider = resolveProvider(this.provider, this.providerResolver, modelName, serialized, invocationParams);
    const stream = isStreaming(invocationParams);
    const input = this.captureInputs ? mapPromptInputs(prompts) : [];

    const recorder = stream
      ? this.client.startStreamingGeneration(this.startPayload(runKey, provider, modelName))
      : this.client.startGeneration(this.startPayload(runKey, provider, modelName));

    this.runs.set(runKey, {
      recorder,
      input,
      captureOutputs: this.captureOutputs,
      outputChunks: [],
    });
  }

  protected onChatModelStart(
    serialized: unknown,
    messages: unknown,
    runId: string,
    extraParams?: AnyRecord
  ): void {
    const runKey = String(runId);
    if (runKey.length === 0 || this.runs.has(runKey)) {
      return;
    }

    const invocationParams = asRecord(extraParams?.invocation_params);
    const modelName = resolveModelName(serialized, invocationParams);
    const provider = resolveProvider(this.provider, this.providerResolver, modelName, serialized, invocationParams);
    const stream = isStreaming(invocationParams);
    const input = this.captureInputs ? mapChatInputs(messages) : [];

    const recorder = stream
      ? this.client.startStreamingGeneration(this.startPayload(runKey, provider, modelName))
      : this.client.startGeneration(this.startPayload(runKey, provider, modelName));

    this.runs.set(runKey, {
      recorder,
      input,
      captureOutputs: this.captureOutputs,
      outputChunks: [],
    });
  }

  protected onLLMNewToken(token: string, runId: string): void {
    const runState = this.runs.get(String(runId));
    if (runState === undefined) {
      return;
    }

    if (typeof token !== 'string' || token.trim().length === 0) {
      return;
    }

    if (runState.captureOutputs) {
      runState.outputChunks.push(token);
    }

    runState.recorder.setFirstTokenAt(new Date());
  }

  protected onLLMEnd(output: unknown, runId: string): void {
    const runState = this.runs.get(String(runId));
    if (runState === undefined) {
      return;
    }
    this.runs.delete(String(runId));

    try {
      const llmOutput = asRecord(read(output, 'llm_output'));
      const responseModel = asString(read(llmOutput, 'model_name'));
      const stopReason = asString(read(llmOutput, 'finish_reason'));
      const usage = mapUsage(read(llmOutput, 'token_usage'));

      let mappedOutput: Message[] | undefined;
      if (runState.captureOutputs) {
        mappedOutput = mapOutputMessages(output);
        if ((mappedOutput?.length ?? 0) === 0 && runState.outputChunks.length > 0) {
          mappedOutput = [{ role: 'assistant', content: runState.outputChunks.join('') }];
        }
      }

      const result: GenerationResult = {
        input: runState.input,
        output: mappedOutput,
        usage,
        responseModel: responseModel.length > 0 ? responseModel : undefined,
        stopReason: stopReason.length > 0 ? stopReason : undefined,
      };
      runState.recorder.setResult(result);
    } finally {
      runState.recorder.end();
    }

    const recorderError = runState.recorder.getError();
    if (recorderError !== undefined) {
      throw recorderError;
    }
  }

  protected onLLMError(error: unknown, runId: string): void {
    const runState = this.runs.get(String(runId));
    if (runState === undefined) {
      return;
    }
    this.runs.delete(String(runId));

    try {
      runState.recorder.setCallError(error);
      if (runState.captureOutputs && runState.outputChunks.length > 0) {
        runState.recorder.setResult({
          input: runState.input,
          output: [{ role: 'assistant', content: runState.outputChunks.join('') }],
        });
      }
    } finally {
      runState.recorder.end();
    }

    const recorderError = runState.recorder.getError();
    if (recorderError !== undefined) {
      throw recorderError;
    }
  }

  private startPayload(runId: string, provider: string, modelName: string) {
    return {
      conversationId: runId,
      agentName: this.agentName,
      agentVersion: this.agentVersion,
      model: {
        provider,
        name: modelName,
      },
      tags: {
        'sigil.framework.name': this.frameworkName,
        'sigil.framework.source': 'handler',
        'sigil.framework.language': this.frameworkLanguage,
        ...this.extraTags,
      },
      metadata: {
        'sigil.framework.run_id': runId,
        ...this.extraMetadata,
      },
    };
  }
}

function resolveProvider(
  explicitProvider: string | undefined,
  resolver: 'auto' | ProviderResolverFn,
  modelName: string,
  serialized: unknown,
  invocationParams: unknown
): string {
  const explicit = normalizeProvider(explicitProvider);
  if (explicit.length > 0) {
    return explicit;
  }

  if (typeof resolver === 'function') {
    const resolved = normalizeProvider(
      resolver({
        modelName,
        serialized,
        invocationParams,
      })
    );
    return resolved.length > 0 ? resolved : 'custom';
  }

  for (const payload of [asRecord(invocationParams), asRecord(serialized)]) {
    const fromProvider = normalizeProvider(asString(read(payload, 'provider')));
    if (fromProvider.length > 0) {
      return fromProvider;
    }
    const fromLsProvider = normalizeProvider(asString(read(payload, 'ls_provider')));
    if (fromLsProvider.length > 0) {
      return fromLsProvider;
    }
  }

  return inferProviderFromModelName(modelName);
}

function resolveModelName(serialized: unknown, invocationParams: unknown): string {
  for (const payload of [asRecord(invocationParams), asRecord(serialized)]) {
    for (const key of ['model', 'model_name', 'ls_model_name']) {
      const value = asString(read(payload, key));
      if (value.length > 0) {
        return value;
      }
    }

    const kwargs = asRecord(read(payload, 'kwargs'));
    for (const key of ['model', 'model_name']) {
      const value = asString(read(kwargs, key));
      if (value.length > 0) {
        return value;
      }
    }
  }

  return 'unknown';
}

function isStreaming(invocationParams: AnyRecord | undefined): boolean {
  if (invocationParams === undefined) {
    return false;
  }
  return asBoolean(read(invocationParams, 'stream')) || asBoolean(read(invocationParams, 'streaming'));
}

function mapPromptInputs(prompts: unknown): Message[] {
  if (!Array.isArray(prompts)) {
    return [];
  }

  const input: Message[] = [];
  for (const prompt of prompts) {
    if (typeof prompt !== 'string') {
      continue;
    }
    const trimmed = prompt.trim();
    if (trimmed.length === 0) {
      continue;
    }
    input.push({ role: 'user', content: trimmed });
  }
  return input;
}

function mapChatInputs(messages: unknown): Message[] {
  if (!Array.isArray(messages)) {
    return [];
  }

  const output: Message[] = [];
  for (const batch of messages) {
    if (!Array.isArray(batch)) {
      continue;
    }

    for (const message of batch) {
      const text = extractMessageText(message);
      if (text.length === 0) {
        continue;
      }

      output.push({
        role: normalizeRole(extractMessageRole(message)),
        content: text,
      });
    }
  }

  return output;
}

function mapOutputMessages(output: unknown): Message[] {
  const generations = read(output, 'generations');
  if (!Array.isArray(generations)) {
    return [];
  }

  const texts: string[] = [];
  for (const candidates of generations) {
    if (!Array.isArray(candidates)) {
      continue;
    }
    for (const candidate of candidates) {
      const text = extractGenerationText(candidate);
      if (text.length > 0) {
        texts.push(text);
      }
    }
  }

  if (texts.length === 0) {
    return [];
  }

  return [{ role: 'assistant', content: texts.join('\n') }];
}

function extractGenerationText(candidate: unknown): string {
  const text = asString(read(candidate, 'text'));
  if (text.length > 0) {
    return text;
  }

  return extractMessageText(read(candidate, 'message'));
}

function extractMessageText(message: unknown): string {
  const content = read(message, 'content');

  if (typeof content === 'string') {
    return content.trim();
  }

  if (Array.isArray(content)) {
    const parts: string[] = [];
    for (const item of content) {
      if (typeof item === 'string') {
        const trimmed = item.trim();
        if (trimmed.length > 0) {
          parts.push(trimmed);
        }
        continue;
      }

      const text = asString(read(item, 'text'));
      if (text.length > 0) {
        parts.push(text);
      }
    }
    return parts.join(' ').trim();
  }

  if (isRecord(content)) {
    return asString(read(content, 'text'));
  }

  return '';
}

function extractMessageRole(message: unknown): string {
  const role = asString(read(message, 'role'));
  if (role.length > 0) {
    return role;
  }
  return asString(read(message, 'type'));
}

function normalizeRole(role: string): Message['role'] {
  const normalized = role.trim().toLowerCase();
  if (normalized === 'assistant' || normalized === 'ai') {
    return 'assistant';
  }
  if (normalized === 'tool') {
    return 'tool';
  }
  return 'user';
}

function mapUsage(rawUsage: unknown): TokenUsage | undefined {
  const usage = asRecord(rawUsage);
  if (usage === undefined) {
    return undefined;
  }

  const inputTokens = asInt(read(usage, 'prompt_tokens')) || asInt(read(usage, 'input_tokens'));
  const outputTokens = asInt(read(usage, 'completion_tokens')) || asInt(read(usage, 'output_tokens'));
  const totalTokens = asInt(read(usage, 'total_tokens')) || inputTokens + outputTokens;

  if (inputTokens === 0 && outputTokens === 0 && totalTokens === 0) {
    return undefined;
  }

  return {
    inputTokens,
    outputTokens,
    totalTokens,
  };
}

function inferProviderFromModelName(modelName: string): string {
  const normalized = modelName.trim().toLowerCase();
  if (
    normalized.startsWith('gpt-')
    || normalized.startsWith('o1')
    || normalized.startsWith('o3')
    || normalized.startsWith('o4')
  ) {
    return 'openai';
  }
  if (normalized.startsWith('claude-')) {
    return 'anthropic';
  }
  if (normalized.startsWith('gemini-')) {
    return 'gemini';
  }
  return 'custom';
}

function normalizeProvider(value: string | undefined): string {
  const normalized = (value ?? '').trim().toLowerCase();
  if (normalized === 'openai' || normalized === 'anthropic' || normalized === 'gemini') {
    return normalized;
  }
  if (normalized.length === 0) {
    return '';
  }
  return 'custom';
}

function read(value: unknown, key: string): unknown {
  if (isRecord(value)) {
    return value[key];
  }
  return undefined;
}

function asRecord(value: unknown): AnyRecord | undefined {
  return isRecord(value) ? value : undefined;
}

function asString(value: unknown): string {
  if (typeof value !== 'string') {
    return '';
  }
  return value.trim();
}

function asInt(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return Math.trunc(value);
  }
  if (typeof value === 'string' && value.trim().length > 0) {
    const parsed = Number.parseInt(value, 10);
    if (Number.isFinite(parsed)) {
      return parsed;
    }
  }
  return 0;
}

function asBoolean(value: unknown): boolean {
  if (typeof value === 'boolean') {
    return value;
  }
  if (typeof value === 'number') {
    return value !== 0;
  }
  if (typeof value === 'string') {
    const normalized = value.trim().toLowerCase();
    return normalized === '1' || normalized === 'true' || normalized === 'yes' || normalized === 'on';
  }
  return false;
}

function isRecord(value: unknown): value is AnyRecord {
  return typeof value === 'object' && value !== null;
}
