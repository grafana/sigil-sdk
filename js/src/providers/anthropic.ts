import type { SigilClient } from '../client.js';
import type { GenerationResult, Message, TokenUsage, ToolDefinition } from '../types.js';

/** Simplified Anthropic message shape consumed by the helper. */
export interface AnthropicMessage {
  role: 'system' | 'user' | 'assistant' | 'tool';
  content: string;
  name?: string;
}

/** Simplified Anthropic request shape consumed by the helper. */
export interface AnthropicRequest {
  model: string;
  systemPrompt?: string;
  messages: AnthropicMessage[];
  tools?: ToolDefinition[];
}

/** Simplified Anthropic response shape produced by caller mapping. */
export interface AnthropicResponse {
  id?: string;
  model?: string;
  outputText: string;
  usage?: TokenUsage;
  stopReason?: string;
  raw?: unknown;
}

/** Streaming summary accepted by the helper for stream finalization. */
export interface AnthropicStreamSummary {
  outputText: string;
  finalResponse?: AnthropicResponse;
  events?: unknown[];
}

/** Optional Sigil fields applied during Anthropic helper mapping. */
export interface AnthropicOptions {
  conversationId?: string;
  agentName?: string;
  agentVersion?: string;
  tags?: Record<string, string>;
  metadata?: Record<string, unknown>;
  rawArtifacts?: boolean;
}

/**
 * Runs a non-stream Anthropic call and records a `SYNC` Sigil generation.
 *
 * The provided `providerCall` executes the actual Anthropic SDK/API request.
 */
export async function completion(
  client: SigilClient,
  request: AnthropicRequest,
  providerCall: (request: AnthropicRequest) => Promise<AnthropicResponse>,
  options: AnthropicOptions = {}
): Promise<AnthropicResponse> {
  return client.startGeneration(
    {
      conversationId: options.conversationId,
      agentName: options.agentName,
      agentVersion: options.agentVersion,
      model: {
        provider: 'anthropic',
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
 * Runs a stream Anthropic call and records a `STREAM` Sigil generation.
 *
 * The provided `providerCall` should return a final stitched stream summary.
 */
export async function completionStream(
  client: SigilClient,
  request: AnthropicRequest,
  providerCall: (request: AnthropicRequest) => Promise<AnthropicStreamSummary>,
  options: AnthropicOptions = {}
): Promise<AnthropicStreamSummary> {
  return client.startStreamingGeneration(
    {
      conversationId: options.conversationId,
      agentName: options.agentName,
      agentVersion: options.agentVersion,
      model: {
        provider: 'anthropic',
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

/** Maps a non-stream Anthropic request/response pair into a Sigil generation result. */
export function fromRequestResponse(
  request: AnthropicRequest,
  response: AnthropicResponse,
  options: AnthropicOptions = {}
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

/** Maps a stream Anthropic summary into a Sigil generation result. */
export function fromStream(
  request: AnthropicRequest,
  summary: AnthropicStreamSummary,
  options: AnthropicOptions = {}
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

function normalizeRole(role: AnthropicMessage['role']): Message['role'] {
  if (role === 'assistant' || role === 'tool') {
    return role;
  }
  return 'user';
}
