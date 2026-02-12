import type { SigilClient } from '../client.js';
import type { GenerationResult, Message, TokenUsage, ToolDefinition } from '../types.js';

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
      tools: request.tools,
      tags: options.tags,
      metadata: options.metadata,
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
      tools: request.tools,
      tags: options.tags,
      metadata: options.metadata,
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
    metadata: options.metadata ? { ...options.metadata } : undefined,
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
    metadata: options.metadata ? { ...options.metadata } : undefined,
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
