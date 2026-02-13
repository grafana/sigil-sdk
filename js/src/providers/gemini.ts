import type { SigilClient } from '../client.js';
import type { GenerationResult, Message, TokenUsage, ToolDefinition } from '../types.js';

const thinkingBudgetMetadataKey = 'sigil.gen_ai.request.thinking.budget_tokens';

/** Simplified Gemini message shape consumed by the helper. */
export interface GeminiMessage {
  role: 'system' | 'user' | 'assistant' | 'tool';
  content: string;
  name?: string;
}

/** Simplified Gemini request shape consumed by the helper. */
export interface GeminiRequest {
  model: string;
  systemPrompt?: string;
  maxOutputTokens?: number;
  temperature?: number;
  topP?: number;
  functionCallingMode?: unknown;
  thinkingConfig?: unknown;
  messages: GeminiMessage[];
  tools?: ToolDefinition[];
}

/** Simplified Gemini response shape produced by caller mapping. */
export interface GeminiResponse {
  id?: string;
  model?: string;
  outputText: string;
  usage?: TokenUsage;
  stopReason?: string;
  raw?: unknown;
}

/** Streaming summary accepted by the helper for stream finalization. */
export interface GeminiStreamSummary {
  outputText: string;
  finalResponse?: GeminiResponse;
  events?: unknown[];
}

/** Optional Sigil fields applied during Gemini helper mapping. */
export interface GeminiOptions {
  conversationId?: string;
  agentName?: string;
  agentVersion?: string;
  tags?: Record<string, string>;
  metadata?: Record<string, unknown>;
  rawArtifacts?: boolean;
}

/**
 * Runs a non-stream Gemini call and records a `SYNC` Sigil generation.
 *
 * The provided `providerCall` executes the actual Gemini SDK/API request.
 */
export async function completion(
  client: SigilClient,
  request: GeminiRequest,
  providerCall: (request: GeminiRequest) => Promise<GeminiResponse>,
  options: GeminiOptions = {}
): Promise<GeminiResponse> {
  return client.startGeneration(
    {
      conversationId: options.conversationId,
      agentName: options.agentName,
      agentVersion: options.agentVersion,
      model: {
        provider: 'gemini',
        name: request.model,
      },
      systemPrompt: request.systemPrompt,
      maxTokens: request.maxOutputTokens,
      temperature: request.temperature,
      topP: request.topP,
      toolChoice: canonicalToolChoice(request.functionCallingMode),
      thinkingEnabled: geminiThinkingEnabled(request.thinkingConfig),
      tools: request.tools,
      tags: options.tags,
      metadata: metadataWithThinkingBudget(options.metadata, geminiThinkingBudget(request.thinkingConfig)),
    },
    async (recorder) => {
      const response = await providerCall(request);
      recorder.setResult(fromRequestResponse(request, response, options));
      return response;
    }
  );
}

/**
 * Runs a stream Gemini call and records a `STREAM` Sigil generation.
 *
 * The provided `providerCall` should return a final stitched stream summary.
 */
export async function completionStream(
  client: SigilClient,
  request: GeminiRequest,
  providerCall: (request: GeminiRequest) => Promise<GeminiStreamSummary>,
  options: GeminiOptions = {}
): Promise<GeminiStreamSummary> {
  return client.startStreamingGeneration(
    {
      conversationId: options.conversationId,
      agentName: options.agentName,
      agentVersion: options.agentVersion,
      model: {
        provider: 'gemini',
        name: request.model,
      },
      systemPrompt: request.systemPrompt,
      maxTokens: request.maxOutputTokens,
      temperature: request.temperature,
      topP: request.topP,
      toolChoice: canonicalToolChoice(request.functionCallingMode),
      thinkingEnabled: geminiThinkingEnabled(request.thinkingConfig),
      tools: request.tools,
      tags: options.tags,
      metadata: metadataWithThinkingBudget(options.metadata, geminiThinkingBudget(request.thinkingConfig)),
    },
    async (recorder) => {
      const summary = await providerCall(request);
      recorder.setResult(fromStream(request, summary, options));
      return summary;
    }
  );
}

/** Maps a non-stream Gemini request/response pair into a Sigil generation result. */
export function fromRequestResponse(
  request: GeminiRequest,
  response: GeminiResponse,
  options: GeminiOptions = {}
): GenerationResult {
  const result: GenerationResult = {
    responseId: response.id,
    responseModel: response.model ?? request.model,
    maxTokens: request.maxOutputTokens,
    temperature: request.temperature,
    topP: request.topP,
    toolChoice: canonicalToolChoice(request.functionCallingMode),
    thinkingEnabled: geminiThinkingEnabled(request.thinkingConfig),
    input: request.messages
      .filter((message) => message.role !== 'system')
      .map((message) => ({
        role: normalizeRole(message.role),
        content: message.content,
        name: message.name,
      })),
    output: [
      {
        role: 'assistant',
        content: response.outputText,
      },
    ],
    tools: request.tools,
    usage: response.usage,
    stopReason: response.stopReason,
    metadata: metadataWithThinkingBudget(options.metadata, geminiThinkingBudget(request.thinkingConfig)),
    tags: options.tags ? { ...options.tags } : undefined,
  };

  if (options.rawArtifacts) {
    result.artifacts = [
      {
        type: 'request',
        payload: JSON.stringify(request),
        mimeType: 'application/json',
      },
      {
        type: 'response',
        payload: JSON.stringify(response.raw ?? response),
        mimeType: 'application/json',
      },
    ];
  }

  return result;
}

/** Maps a stream Gemini summary into a Sigil generation result. */
export function fromStream(
  request: GeminiRequest,
  summary: GeminiStreamSummary,
  options: GeminiOptions = {}
): GenerationResult {
  const finalResponse = summary.finalResponse;
  const result: GenerationResult = {
    responseId: finalResponse?.id,
    responseModel: finalResponse?.model ?? request.model,
    maxTokens: request.maxOutputTokens,
    temperature: request.temperature,
    topP: request.topP,
    toolChoice: canonicalToolChoice(request.functionCallingMode),
    thinkingEnabled: geminiThinkingEnabled(request.thinkingConfig),
    input: request.messages
      .filter((message) => message.role !== 'system')
      .map((message) => ({
        role: normalizeRole(message.role),
        content: message.content,
        name: message.name,
      })),
    output: [
      {
        role: 'assistant',
        content: summary.outputText,
      },
    ],
    tools: request.tools,
    usage: finalResponse?.usage,
    stopReason: finalResponse?.stopReason,
    metadata: metadataWithThinkingBudget(options.metadata, geminiThinkingBudget(request.thinkingConfig)),
    tags: options.tags ? { ...options.tags } : undefined,
  };

  if (options.rawArtifacts) {
    result.artifacts = [
      {
        type: 'request',
        payload: JSON.stringify(request),
        mimeType: 'application/json',
      },
      {
        type: 'provider_event',
        payload: JSON.stringify(summary.events ?? []),
        mimeType: 'application/json',
      },
    ];
  }

  return result;
}

function normalizeRole(role: GeminiMessage['role']): Message['role'] {
  if (role === 'assistant' || role === 'tool') {
    return role;
  }
  return 'user';
}

function geminiThinkingEnabled(value: unknown): boolean | undefined {
  if (value === undefined || value === null) {
    return undefined;
  }
  if (isRecord(value) && typeof value.includeThoughts === 'boolean') {
    return value.includeThoughts;
  }
  return undefined;
}

function geminiThinkingBudget(value: unknown): number | undefined {
  if (!isRecord(value)) {
    return undefined;
  }
  const snakeCase = coerceInteger(value.thinking_budget);
  if (snakeCase !== undefined) {
    return snakeCase;
  }
  return coerceInteger(value.thinkingBudget);
}

function canonicalToolChoice(value: unknown): string | undefined {
  if (value === undefined || value === null) {
    return undefined;
  }
  if (typeof value === 'string') {
    const normalized = value.trim().toLowerCase();
    return normalized.length > 0 ? normalized : undefined;
  }
  if (isRecord(value) && 'value' in value) {
    const normalized = String((value as { value: unknown }).value ?? '').trim().toLowerCase();
    return normalized.length > 0 ? normalized : undefined;
  }
  try {
    const encoded = JSON.stringify(value, objectKeySorter);
    return encoded.length > 0 ? encoded : undefined;
  } catch {
    const normalized = String(value).trim().toLowerCase();
    return normalized.length > 0 ? normalized : undefined;
  }
}

function objectKeySorter(_key: string, value: unknown): unknown {
  if (!isRecord(value) || Array.isArray(value)) {
    return value;
  }
  const sorted = {} as Record<string, unknown>;
  for (const key of Object.keys(value).sort()) {
    sorted[key] = value[key];
  }
  return sorted;
}

function metadataWithThinkingBudget(
  metadata: Record<string, unknown> | undefined,
  thinkingBudget: number | undefined
): Record<string, unknown> | undefined {
  if (thinkingBudget === undefined) {
    return metadata ? { ...metadata } : undefined;
  }
  const out = metadata ? { ...metadata } : {};
  out[thinkingBudgetMetadataKey] = thinkingBudget;
  return out;
}

function coerceInteger(value: unknown): number | undefined {
  if (value === undefined || value === null || typeof value === 'boolean') {
    return undefined;
  }
  if (typeof value === 'number') {
    if (!Number.isFinite(value) || !Number.isInteger(value)) {
      return undefined;
    }
    return value;
  }
  if (typeof value === 'string') {
    const normalized = value.trim();
    if (normalized.length === 0) {
      return undefined;
    }
    const parsed = Number.parseInt(normalized, 10);
    if (Number.isNaN(parsed)) {
      return undefined;
    }
    return parsed;
  }
  return undefined;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}
