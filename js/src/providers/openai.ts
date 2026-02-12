import type { SigilClient } from '../client.js';
import type { GenerationResult, Message, TokenUsage, ToolDefinition } from '../types.js';

/** Simplified OpenAI chat message shape consumed by the helper. */
export interface OpenAIMessage {
  role: 'system' | 'user' | 'assistant' | 'tool';
  content: string;
  name?: string;
}

/** Simplified OpenAI chat request shape consumed by the helper. */
export interface OpenAIChatRequest {
  model: string;
  systemPrompt?: string;
  messages: OpenAIMessage[];
  tools?: ToolDefinition[];
}

/** Simplified OpenAI chat response shape produced by caller mapping. */
export interface OpenAIChatResponse {
  id?: string;
  model?: string;
  outputText: string;
  usage?: TokenUsage;
  stopReason?: string;
  raw?: unknown;
}

/** Streaming summary accepted by the helper for stream finalization. */
export interface OpenAIStreamSummary {
  outputText: string;
  finalResponse?: OpenAIChatResponse;
  chunks?: unknown[];
}

/** Optional Sigil fields applied during OpenAI helper mapping. */
export interface OpenAIOptions {
  conversationId?: string;
  agentName?: string;
  agentVersion?: string;
  tags?: Record<string, string>;
  metadata?: Record<string, unknown>;
  rawArtifacts?: boolean;
}

/**
 * Runs a non-stream OpenAI call and records a `SYNC` Sigil generation.
 *
 * The provided `providerCall` executes the actual OpenAI SDK/API request.
 */
export async function chatCompletion(
  client: SigilClient,
  request: OpenAIChatRequest,
  providerCall: (request: OpenAIChatRequest) => Promise<OpenAIChatResponse>,
  options: OpenAIOptions = {}
): Promise<OpenAIChatResponse> {
  return client.startGeneration(
    {
      conversationId: options.conversationId,
      agentName: options.agentName,
      agentVersion: options.agentVersion,
      model: {
        provider: 'openai',
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
 * Runs a stream OpenAI call and records a `STREAM` Sigil generation.
 *
 * The provided `providerCall` should return a final stitched stream summary.
 */
export async function chatCompletionStream(
  client: SigilClient,
  request: OpenAIChatRequest,
  providerCall: (request: OpenAIChatRequest) => Promise<OpenAIStreamSummary>,
  options: OpenAIOptions = {}
): Promise<OpenAIStreamSummary> {
  return client.startStreamingGeneration(
    {
      conversationId: options.conversationId,
      agentName: options.agentName,
      agentVersion: options.agentVersion,
      model: {
        provider: 'openai',
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

/** Maps a non-stream OpenAI request/response pair into a Sigil generation result. */
export function fromRequestResponse(
  request: OpenAIChatRequest,
  response: OpenAIChatResponse,
  options: OpenAIOptions = {}
): GenerationResult {
  const outputMessage: Message = {
    role: 'assistant',
    content: response.outputText,
  };

  const inputMessages: Message[] = request.messages
    .filter((message) => message.role !== 'system')
    .map((message) => ({
      role: normalizeRole(message.role),
      content: message.content,
      name: message.name,
    }));

  const result: GenerationResult = {
    responseId: response.id,
    responseModel: response.model ?? request.model,
    input: inputMessages,
    output: [outputMessage],
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

/** Maps a stream OpenAI summary into a Sigil generation result. */
export function fromStream(
  request: OpenAIChatRequest,
  summary: OpenAIStreamSummary,
  options: OpenAIOptions = {}
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
        payload: JSON.stringify(summary.chunks ?? []),
        mimeType: 'application/json',
      },
    ];
  }

  return result;
}

function normalizeRole(role: OpenAIMessage['role']): Message['role'] {
  if (role === 'assistant' || role === 'tool') {
    return role;
  }
  return 'user';
}
