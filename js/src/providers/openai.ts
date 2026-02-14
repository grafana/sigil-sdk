import type OpenAI from 'openai';
import type { GenerationResult, Message, TokenUsage, ToolDefinition } from '../types.js';
import type { SigilClient } from '../client.js';

const thinkingBudgetMetadataKey = 'sigil.gen_ai.request.thinking.budget_tokens';
type AnyRecord = Record<string, unknown>;

type ChatCreateRequest = OpenAI.Chat.Completions.ChatCompletionCreateParamsNonStreaming & AnyRecord;
type ChatStreamRequest = OpenAI.Chat.Completions.ChatCompletionCreateParamsStreaming & AnyRecord;
type ChatResponse = OpenAI.Chat.Completions.ChatCompletion & AnyRecord;
type ChatStreamEvent = OpenAI.Chat.Completions.ChatCompletionChunk;

type ResponsesCreateRequest = OpenAI.Responses.ResponseCreateParamsNonStreaming & AnyRecord;
type ResponsesStreamRequest = OpenAI.Responses.ResponseCreateParamsStreaming & AnyRecord;
type ResponsesResponse = OpenAI.Responses.Response & AnyRecord;
type ResponsesStreamEvent = OpenAI.Responses.ResponseStreamEvent;

/** Optional Sigil fields applied during OpenAI helper mapping. */
export interface OpenAIOptions {
  conversationId?: string;
  agentName?: string;
  agentVersion?: string;
  tags?: Record<string, string>;
  metadata?: Record<string, unknown>;
  rawArtifacts?: boolean;
}

/** Streaming summary accepted by chat-completions stream wrapper. */
export interface ChatCompletionsStreamSummary {
  events?: ChatStreamEvent[];
  finalResponse?: ChatResponse;
  outputText?: string;
  firstChunkAt?: Date | string | number;
}

/** Streaming summary accepted by responses stream wrapper. */
export interface ResponsesStreamSummary {
  events?: ResponsesStreamEvent[];
  finalResponse?: ResponsesResponse;
  outputText?: string;
  firstChunkAt?: Date | string | number;
}

async function chatCompletionsCreate(
  client: SigilClient,
  request: ChatCreateRequest,
  providerCall: (request: ChatCreateRequest) => Promise<ChatResponse>,
  options: OpenAIOptions = {}
): Promise<ChatResponse> {
  const requestMessages = mapChatRequestMessages(request);
  const mappedTools = mapChatTools(request);
  const maxTokens = readIntFromAny((request as AnyRecord).max_completion_tokens)
    ?? readIntFromAny((request as AnyRecord).max_tokens);
  const temperature = readNumberFromAny((request as AnyRecord).temperature);
  const topP = readNumberFromAny((request as AnyRecord).top_p);
  const toolChoice = canonicalToolChoice((request as AnyRecord).tool_choice);
  const thinkingBudget = openAIThinkingBudget((request as AnyRecord).reasoning);

  return client.startGeneration(
    {
      conversationId: options.conversationId,
      agentName: options.agentName,
      agentVersion: options.agentVersion,
      model: {
        provider: 'openai',
        name: String((request as AnyRecord).model ?? ''),
      },
      systemPrompt: requestMessages.systemPrompt,
      maxTokens,
      temperature,
      topP,
      toolChoice,
      thinkingEnabled: hasValue((request as AnyRecord).reasoning) ? true : undefined,
      tools: mappedTools,
      tags: options.tags,
      metadata: metadataWithThinkingBudget(options.metadata, thinkingBudget),
    },
    async (recorder) => {
      const response = await providerCall(request);
      recorder.setResult(chatCompletionsFromRequestResponse(request, response, options));
      return response;
    }
  );
}

async function chatCompletionsStream(
  client: SigilClient,
  request: ChatStreamRequest,
  providerCall: (request: ChatStreamRequest) => Promise<ChatCompletionsStreamSummary>,
  options: OpenAIOptions = {}
): Promise<ChatCompletionsStreamSummary> {
  const requestMessages = mapChatRequestMessages(request);
  const mappedTools = mapChatTools(request);
  const maxTokens = readIntFromAny((request as AnyRecord).max_completion_tokens)
    ?? readIntFromAny((request as AnyRecord).max_tokens);
  const temperature = readNumberFromAny((request as AnyRecord).temperature);
  const topP = readNumberFromAny((request as AnyRecord).top_p);
  const toolChoice = canonicalToolChoice((request as AnyRecord).tool_choice);
  const thinkingBudget = openAIThinkingBudget((request as AnyRecord).reasoning);

  return client.startStreamingGeneration(
    {
      conversationId: options.conversationId,
      agentName: options.agentName,
      agentVersion: options.agentVersion,
      model: {
        provider: 'openai',
        name: String((request as AnyRecord).model ?? ''),
      },
      systemPrompt: requestMessages.systemPrompt,
      maxTokens,
      temperature,
      topP,
      toolChoice,
      thinkingEnabled: hasValue((request as AnyRecord).reasoning) ? true : undefined,
      tools: mappedTools,
      tags: options.tags,
      metadata: metadataWithThinkingBudget(options.metadata, thinkingBudget),
    },
    async (recorder) => {
      const summary = await providerCall(request);
      const firstChunkAt = asDate(summary.firstChunkAt);
      if (firstChunkAt !== undefined) {
        recorder.setFirstTokenAt(firstChunkAt);
      }
      recorder.setResult(chatCompletionsFromStream(request, summary, options));
      return summary;
    }
  );
}

function chatCompletionsFromRequestResponse(
  request: ChatCreateRequest,
  response: ChatResponse,
  options: OpenAIOptions = {}
): GenerationResult {
  const requestMessages = mapChatRequestMessages(request);
  const mappedTools = mapChatTools(request);
  const maxTokens = readIntFromAny((request as AnyRecord).max_completion_tokens)
    ?? readIntFromAny((request as AnyRecord).max_tokens);
  const temperature = readNumberFromAny((request as AnyRecord).temperature);
  const topP = readNumberFromAny((request as AnyRecord).top_p);
  const toolChoice = canonicalToolChoice((request as AnyRecord).tool_choice);
  const thinkingBudget = openAIThinkingBudget((request as AnyRecord).reasoning);

  const result: GenerationResult = {
    responseId: response.id,
    responseModel: response.model ?? String((request as AnyRecord).model ?? ''),
    maxTokens,
    temperature,
    topP,
    toolChoice,
    thinkingEnabled: hasValue((request as AnyRecord).reasoning) ? true : undefined,
    input: requestMessages.input,
    output: mapChatResponseOutput(response),
    tools: mappedTools,
    usage: mapChatUsage(response.usage),
    stopReason: normalizeChatStopReason(firstFinishReason(response)),
    metadata: metadataWithThinkingBudget(options.metadata, thinkingBudget),
    tags: options.tags ? { ...options.tags } : undefined,
  };

  if (options.rawArtifacts) {
    result.artifacts = [
      jsonArtifact('request', 'openai.chat.request', request),
      jsonArtifact('response', 'openai.chat.response', response),
    ];
    if (mappedTools.length > 0) {
      result.artifacts.push(jsonArtifact('tools', 'openai.chat.tools', mappedTools));
    }
  }

  return result;
}

function chatCompletionsFromStream(
  request: ChatStreamRequest,
  summary: ChatCompletionsStreamSummary,
  options: OpenAIOptions = {}
): GenerationResult {
  const requestMessages = mapChatRequestMessages(request);
  const mappedTools = mapChatTools(request);
  const maxTokens = readIntFromAny((request as AnyRecord).max_completion_tokens)
    ?? readIntFromAny((request as AnyRecord).max_tokens);
  const temperature = readNumberFromAny((request as AnyRecord).temperature);
  const topP = readNumberFromAny((request as AnyRecord).top_p);
  const toolChoice = canonicalToolChoice((request as AnyRecord).tool_choice);
  const thinkingBudget = openAIThinkingBudget((request as AnyRecord).reasoning);

  const outputText = summary.outputText ?? extractChatStreamText(summary.events ?? []);
  const fallbackOutput: Message[] = outputText.length > 0
    ? [{ role: 'assistant', content: outputText }]
    : [];

  const result: GenerationResult = summary.finalResponse
    ? {
        ...chatCompletionsFromRequestResponse(request as unknown as ChatCreateRequest, summary.finalResponse, options),
        output: mapChatResponseOutput(summary.finalResponse).length > 0
          ? mapChatResponseOutput(summary.finalResponse)
          : fallbackOutput,
      }
    : {
        responseModel: String((request as AnyRecord).model ?? ''),
        maxTokens,
        temperature,
        topP,
        toolChoice,
        thinkingEnabled: hasValue((request as AnyRecord).reasoning) ? true : undefined,
        input: requestMessages.input,
        output: fallbackOutput,
        tools: mappedTools,
        metadata: metadataWithThinkingBudget(options.metadata, thinkingBudget),
        tags: options.tags ? { ...options.tags } : undefined,
      };

  if (options.rawArtifacts) {
    const existing = result.artifacts ?? [];
    if (!existing.some((artifact) => artifact.type === 'request')) {
      existing.push(jsonArtifact('request', 'openai.chat.request', request));
    }
    if (mappedTools.length > 0 && !existing.some((artifact) => artifact.type === 'tools')) {
      existing.push(jsonArtifact('tools', 'openai.chat.tools', mappedTools));
    }
    existing.push(jsonArtifact('provider_event', 'openai.chat.stream_events', summary.events ?? []));
    result.artifacts = existing;
  }

  return result;
}

async function responsesCreate(
  client: SigilClient,
  request: ResponsesCreateRequest,
  providerCall: (request: ResponsesCreateRequest) => Promise<ResponsesResponse>,
  options: OpenAIOptions = {}
): Promise<ResponsesResponse> {
  const requestPayload = mapResponsesRequest(request);
  const controls = mapResponsesRequestControls(request);

  return client.startGeneration(
    {
      conversationId: options.conversationId,
      agentName: options.agentName,
      agentVersion: options.agentVersion,
      model: {
        provider: 'openai',
        name: String((request as AnyRecord).model ?? ''),
      },
      systemPrompt: requestPayload.systemPrompt,
      maxTokens: controls.maxTokens,
      temperature: controls.temperature,
      topP: controls.topP,
      toolChoice: controls.toolChoice,
      thinkingEnabled: controls.thinkingEnabled,
      tools: requestPayload.tools,
      tags: options.tags,
      metadata: metadataWithThinkingBudget(options.metadata, controls.thinkingBudget),
    },
    async (recorder) => {
      const response = await providerCall(request);
      recorder.setResult(responsesFromRequestResponse(request, response, options));
      return response;
    }
  );
}

async function responsesStream(
  client: SigilClient,
  request: ResponsesStreamRequest,
  providerCall: (request: ResponsesStreamRequest) => Promise<ResponsesStreamSummary>,
  options: OpenAIOptions = {}
): Promise<ResponsesStreamSummary> {
  const requestPayload = mapResponsesRequest(request);
  const controls = mapResponsesRequestControls(request);

  return client.startStreamingGeneration(
    {
      conversationId: options.conversationId,
      agentName: options.agentName,
      agentVersion: options.agentVersion,
      model: {
        provider: 'openai',
        name: String((request as AnyRecord).model ?? ''),
      },
      systemPrompt: requestPayload.systemPrompt,
      maxTokens: controls.maxTokens,
      temperature: controls.temperature,
      topP: controls.topP,
      toolChoice: controls.toolChoice,
      thinkingEnabled: controls.thinkingEnabled,
      tools: requestPayload.tools,
      tags: options.tags,
      metadata: metadataWithThinkingBudget(options.metadata, controls.thinkingBudget),
    },
    async (recorder) => {
      const summary = await providerCall(request);
      const firstChunkAt = asDate(summary.firstChunkAt);
      if (firstChunkAt !== undefined) {
        recorder.setFirstTokenAt(firstChunkAt);
      }
      recorder.setResult(responsesFromStream(request, summary, options));
      return summary;
    }
  );
}

function responsesFromRequestResponse(
  request: ResponsesCreateRequest,
  response: ResponsesResponse,
  options: OpenAIOptions = {}
): GenerationResult {
  const requestPayload = mapResponsesRequest(request);
  const controls = mapResponsesRequestControls(request);

  const result: GenerationResult = {
    responseId: response.id,
    responseModel: response.model ?? String((request as AnyRecord).model ?? ''),
    maxTokens: controls.maxTokens,
    temperature: controls.temperature,
    topP: controls.topP,
    toolChoice: controls.toolChoice,
    thinkingEnabled: controls.thinkingEnabled,
    input: requestPayload.input,
    output: mapResponsesOutputItems((response as AnyRecord).output),
    tools: requestPayload.tools,
    usage: mapResponsesUsage((response as AnyRecord).usage),
    stopReason: normalizeResponsesStopReason(response),
    metadata: metadataWithThinkingBudget(options.metadata, controls.thinkingBudget),
    tags: options.tags ? { ...options.tags } : undefined,
  };

  if (options.rawArtifacts) {
    result.artifacts = [
      jsonArtifact('request', 'openai.responses.request', request),
      jsonArtifact('response', 'openai.responses.response', response),
    ];
    if (requestPayload.tools.length > 0) {
      result.artifacts.push(jsonArtifact('tools', 'openai.responses.tools', requestPayload.tools));
    }
  }

  return result;
}

function responsesFromStream(
  request: ResponsesStreamRequest,
  summary: ResponsesStreamSummary,
  options: OpenAIOptions = {}
): GenerationResult {
  const requestPayload = mapResponsesRequest(request);
  const controls = mapResponsesRequestControls(request);
  const events = summary.events ?? [];
  const finalFromEvents = findResponsesFinalFromEvents(events);
  const finalResponse = summary.finalResponse ?? finalFromEvents;

  const outputText = summary.outputText ?? extractResponsesStreamText(events);

  const result: GenerationResult = finalResponse
    ? {
        ...responsesFromRequestResponse(request as unknown as ResponsesCreateRequest, finalResponse, options),
        output: mapResponsesOutputItems((finalResponse as AnyRecord).output).length > 0
          ? mapResponsesOutputItems((finalResponse as AnyRecord).output)
          : outputText.length > 0
            ? [{ role: 'assistant', content: outputText }]
            : [],
      }
    : {
        responseModel: String((request as AnyRecord).model ?? ''),
        maxTokens: controls.maxTokens,
        temperature: controls.temperature,
        topP: controls.topP,
        toolChoice: controls.toolChoice,
        thinkingEnabled: controls.thinkingEnabled,
        input: requestPayload.input,
        output: outputText.length > 0 ? [{ role: 'assistant', content: outputText }] : [],
        tools: requestPayload.tools,
        stopReason: normalizeResponsesStopReasonFromEvents(events),
        metadata: metadataWithThinkingBudget(options.metadata, controls.thinkingBudget),
        tags: options.tags ? { ...options.tags } : undefined,
      };

  if (options.rawArtifacts) {
    const existing = result.artifacts ?? [];
    if (!existing.some((artifact) => artifact.type === 'request')) {
      existing.push(jsonArtifact('request', 'openai.responses.request', request));
    }
    if (requestPayload.tools.length > 0 && !existing.some((artifact) => artifact.type === 'tools')) {
      existing.push(jsonArtifact('tools', 'openai.responses.tools', requestPayload.tools));
    }
    existing.push(jsonArtifact('provider_event', 'openai.responses.stream_events', events));
    result.artifacts = existing;
  }

  return result;
}

export const chat = {
  completions: {
    create: chatCompletionsCreate,
    stream: chatCompletionsStream,
    fromRequestResponse: chatCompletionsFromRequestResponse,
    fromStream: chatCompletionsFromStream,
  },
} as const;

export const responses = {
  create: responsesCreate,
  stream: responsesStream,
  fromRequestResponse: responsesFromRequestResponse,
  fromStream: responsesFromStream,
} as const;

function mapChatRequestMessages(request: ChatCreateRequest | ChatStreamRequest): {
  input: Message[];
  systemPrompt?: string;
} {
  const source = Array.isArray((request as AnyRecord).messages)
    ? ((request as AnyRecord).messages as unknown[])
    : [];

  const systemChunks: string[] = [];
  const input: Message[] = [];

  for (const rawMessage of source) {
    if (!isRecord(rawMessage)) {
      continue;
    }

    const role = String(rawMessage.role ?? '').trim().toLowerCase();
    const content = extractText(rawMessage.content);

    if (role === 'system' || role === 'developer') {
      if (content.length > 0) {
        systemChunks.push(content);
      }
      continue;
    }

    const normalizedRole: Message['role'] = role === 'assistant' || role === 'tool' ? role : 'user';
    const message: Message = { role: normalizedRole };

    if (content.length > 0) {
      message.content = content;
    }

    if (typeof rawMessage.name === 'string' && rawMessage.name.trim().length > 0) {
      message.name = rawMessage.name;
    }

    if (normalizedRole === 'assistant' && Array.isArray(rawMessage.tool_calls)) {
      const parts = mapChatToolCallParts(rawMessage.tool_calls);
      if (parts.length > 0) {
        message.parts = message.parts ? [...message.parts, ...parts] : parts;
      }
    }

    input.push(message);
  }

  return {
    input,
    systemPrompt: systemChunks.length > 0 ? systemChunks.join('\n\n') : undefined,
  };
}

function mapChatResponseOutput(response: ChatResponse): Message[] {
  const choice = response.choices?.[0];
  if (!choice) {
    return [];
  }

  const messageRecord = isRecord(choice.message) ? choice.message : undefined;
  if (!messageRecord) {
    return [];
  }

  const textChunks: string[] = [];
  const rawContent = messageRecord.content;
  const contentText = extractText(rawContent);
  if (contentText.length > 0) {
    textChunks.push(contentText);
  }

  if (typeof messageRecord.refusal === 'string' && messageRecord.refusal.trim().length > 0) {
    textChunks.push(messageRecord.refusal.trim());
  }

  const parts = mapChatToolCallParts(messageRecord.tool_calls);

  if (textChunks.length === 0 && parts.length === 0) {
    return [];
  }

  const output: Message = {
    role: 'assistant',
  };

  if (textChunks.length > 0) {
    output.content = textChunks.join('\n');
  }

  if (parts.length > 0) {
    output.parts = parts;
    if (!output.content) {
      output.content = parts
        .map((part) => (part.type === 'tool_call' ? `${part.toolCall.name}(${part.toolCall.inputJSON ?? ''})` : ''))
        .filter((chunk) => chunk.length > 0)
        .join('\n');
    }
  }

  return [output];
}

function mapChatToolCallParts(value: unknown): NonNullable<Message['parts']> {
  if (!Array.isArray(value)) {
    return [];
  }

  const parts: NonNullable<Message['parts']> = [];

  for (const rawToolCall of value) {
    if (!isRecord(rawToolCall)) {
      continue;
    }

    const functionCall = isRecord(rawToolCall.function) ? rawToolCall.function : undefined;
    if (!functionCall) {
      continue;
    }

    const name = typeof functionCall.name === 'string' ? functionCall.name : '';
    if (name.trim().length === 0) {
      continue;
    }

    const input = typeof functionCall.arguments === 'string'
      ? functionCall.arguments
      : jsonString(functionCall.arguments);

    parts.push({
      type: 'tool_call',
      toolCall: {
        id: typeof rawToolCall.id === 'string' ? rawToolCall.id : undefined,
        name,
        inputJSON: input,
      },
      metadata: { providerType: 'tool_call' },
    });
  }

  return parts;
}

function mapChatTools(request: ChatCreateRequest | ChatStreamRequest): ToolDefinition[] {
  const value = (request as AnyRecord).tools;
  if (!Array.isArray(value)) {
    return [];
  }

  const out: ToolDefinition[] = [];

  for (const rawTool of value) {
    if (!isRecord(rawTool)) {
      continue;
    }

    const rawType = typeof rawTool.type === 'string' ? rawTool.type : '';

    if (rawType === 'function' && isRecord(rawTool.function)) {
      const name = typeof rawTool.function.name === 'string' ? rawTool.function.name : '';
      if (name.trim().length === 0) {
        continue;
      }
      out.push({
        name,
        description: typeof rawTool.function.description === 'string' ? rawTool.function.description : undefined,
        type: 'function',
        inputSchemaJSON: hasValue(rawTool.function.parameters)
          ? jsonString(rawTool.function.parameters)
          : undefined,
      });
      continue;
    }

    if (rawType.length > 0 && typeof rawTool.name === 'string' && rawTool.name.trim().length > 0) {
      out.push({
        name: rawTool.name,
        type: rawType,
      });
    }
  }

  return out;
}

function mapChatUsage(usage: unknown): TokenUsage | undefined {
  if (!isRecord(usage)) {
    return undefined;
  }

  const inputTokens = readIntFromAny(usage.prompt_tokens);
  const outputTokens = readIntFromAny(usage.completion_tokens);
  const totalTokens = readIntFromAny(usage.total_tokens);
  const cacheReadInputTokens = isRecord(usage.prompt_tokens_details)
    ? readIntFromAny(usage.prompt_tokens_details.cached_tokens)
    : undefined;
  const reasoningTokens = isRecord(usage.completion_tokens_details)
    ? readIntFromAny(usage.completion_tokens_details.reasoning_tokens)
    : undefined;

  const out: TokenUsage = {};
  if (inputTokens !== undefined) {
    out.inputTokens = inputTokens;
  }
  if (outputTokens !== undefined) {
    out.outputTokens = outputTokens;
  }
  if (totalTokens !== undefined) {
    out.totalTokens = totalTokens;
  }
  if (cacheReadInputTokens !== undefined) {
    out.cacheReadInputTokens = cacheReadInputTokens;
  }
  if (reasoningTokens !== undefined) {
    out.reasoningTokens = reasoningTokens;
  }

  return Object.keys(out).length > 0 ? out : undefined;
}

function firstFinishReason(response: ChatResponse): string | undefined {
  for (const choice of response.choices ?? []) {
    if (typeof choice.finish_reason === 'string' && choice.finish_reason.trim().length > 0) {
      return choice.finish_reason;
    }
  }
  return undefined;
}

function normalizeChatStopReason(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  return value;
}

function extractChatStreamText(events: ChatStreamEvent[]): string {
  const chunks: string[] = [];

  for (const event of events) {
    for (const choice of event.choices ?? []) {
      if (typeof choice.delta?.content === 'string' && choice.delta.content.length > 0) {
        chunks.push(choice.delta.content);
      }
    }
  }

  return chunks.join('');
}

function mapResponsesRequest(request: ResponsesCreateRequest | ResponsesStreamRequest): {
  input: Message[];
  systemPrompt?: string;
  tools: ToolDefinition[];
} {
  const input: Message[] = [];
  const systemChunks: string[] = [];

  const instructions = extractText((request as AnyRecord).instructions);
  if (instructions.length > 0) {
    systemChunks.push(instructions);
  }

  const rawInput = (request as AnyRecord).input;
  if (typeof rawInput === 'string') {
    input.push({
      role: 'user',
      content: rawInput,
    });
  } else if (Array.isArray(rawInput)) {
    for (const rawItem of rawInput) {
      if (!isRecord(rawItem)) {
        continue;
      }

      const role = typeof rawItem.role === 'string' ? rawItem.role.trim().toLowerCase() : '';
      const itemType = typeof rawItem.type === 'string' ? rawItem.type.trim().toLowerCase() : '';

      if ((role === 'system' || role === 'developer') && itemType === 'message') {
        const content = extractText(rawItem.content);
        if (content.length > 0) {
          systemChunks.push(content);
        }
        continue;
      }

      if (itemType === 'function_call_output') {
        const outputValue = rawItem.output;
        const content = typeof outputValue === 'string' ? outputValue : jsonString(outputValue);
        if (content.length > 0) {
          input.push({ role: 'tool', content });
        }
        continue;
      }

      if (itemType === 'message' || role.length > 0) {
        const content = extractText(rawItem.content);
        if (content.length === 0) {
          continue;
        }
        const mappedRole: Message['role'] = role === 'assistant' || role === 'tool' ? role : 'user';
        input.push({ role: mappedRole, content });
      }
    }
  }

  const tools = mapResponsesTools((request as AnyRecord).tools);

  return {
    input,
    systemPrompt: systemChunks.length > 0 ? systemChunks.join('\n\n') : undefined,
    tools,
  };
}

function mapResponsesRequestControls(request: ResponsesCreateRequest | ResponsesStreamRequest): {
  maxTokens?: number;
  temperature?: number;
  topP?: number;
  toolChoice?: string;
  thinkingEnabled?: boolean;
  thinkingBudget?: number;
} {
  const maxTokens = readIntFromAny((request as AnyRecord).max_output_tokens);
  const temperature = readNumberFromAny((request as AnyRecord).temperature);
  const topP = readNumberFromAny((request as AnyRecord).top_p);
  const toolChoice = canonicalToolChoice((request as AnyRecord).tool_choice);
  const reasoning = (request as AnyRecord).reasoning;

  return {
    maxTokens,
    temperature,
    topP,
    toolChoice,
    thinkingEnabled: hasValue(reasoning) ? true : undefined,
    thinkingBudget: openAIThinkingBudget(reasoning),
  };
}

function mapResponsesTools(value: unknown): ToolDefinition[] {
  if (!Array.isArray(value)) {
    return [];
  }

  const out: ToolDefinition[] = [];

  for (const rawTool of value) {
    if (!isRecord(rawTool)) {
      continue;
    }

    const toolType = typeof rawTool.type === 'string' ? rawTool.type : '';

    if (toolType === 'function') {
      const name = typeof rawTool.name === 'string' ? rawTool.name : '';
      if (name.trim().length === 0) {
        continue;
      }
      out.push({
        name,
        description: typeof rawTool.description === 'string' ? rawTool.description : undefined,
        type: 'function',
        inputSchemaJSON: hasValue(rawTool.parameters) ? jsonString(rawTool.parameters) : undefined,
      });
      continue;
    }

    if (toolType.length > 0 && typeof rawTool.name === 'string' && rawTool.name.trim().length > 0) {
      out.push({
        name: rawTool.name,
        type: toolType,
      });
    }
  }

  return out;
}

function mapResponsesOutputItems(value: unknown): Message[] {
  if (!Array.isArray(value)) {
    return [];
  }

  const output: Message[] = [];

  for (const rawItem of value) {
    if (!isRecord(rawItem)) {
      continue;
    }

    const itemType = typeof rawItem.type === 'string' ? rawItem.type : '';

    if (itemType === 'message') {
      const content = extractText(rawItem.content);
      if (content.length > 0) {
        output.push({ role: 'assistant', content });
      }
      continue;
    }

    if (itemType === 'function_call') {
      const name = typeof rawItem.name === 'string' ? rawItem.name : '';
      const args = typeof rawItem.arguments === 'string'
        ? rawItem.arguments
        : jsonString(rawItem.arguments);
      if (name.trim().length > 0) {
        output.push({
          role: 'assistant',
          content: `${name}(${args})`,
          parts: [
            {
              type: 'tool_call',
              toolCall: {
                id: typeof rawItem.call_id === 'string' ? rawItem.call_id : undefined,
                name,
                inputJSON: args,
              },
              metadata: { providerType: 'tool_call' },
            },
          ],
        });
      }
      continue;
    }

    if (itemType === 'function_call_output') {
      const content = typeof rawItem.output === 'string'
        ? rawItem.output
        : jsonString(rawItem.output);
      if (content.length > 0) {
        output.push({ role: 'tool', content });
      }
      continue;
    }

    if (itemType.length > 0) {
      const fallback = extractText(rawItem) || jsonString(rawItem);
      if (fallback.length > 0) {
        output.push({ role: 'assistant', content: fallback });
      }
    }
  }

  return output;
}

function mapResponsesUsage(value: unknown): TokenUsage | undefined {
  if (!isRecord(value)) {
    return undefined;
  }

  const inputTokens = readIntFromAny(value.input_tokens);
  const outputTokens = readIntFromAny(value.output_tokens);
  const totalTokens = readIntFromAny(value.total_tokens);
  const cacheReadInputTokens = isRecord(value.input_tokens_details)
    ? readIntFromAny(value.input_tokens_details.cached_tokens)
    : undefined;
  const reasoningTokens = isRecord(value.output_tokens_details)
    ? readIntFromAny(value.output_tokens_details.reasoning_tokens)
    : undefined;

  const out: TokenUsage = {};
  if (inputTokens !== undefined) {
    out.inputTokens = inputTokens;
  }
  if (outputTokens !== undefined) {
    out.outputTokens = outputTokens;
  }
  if (totalTokens !== undefined) {
    out.totalTokens = totalTokens;
  }
  if (cacheReadInputTokens !== undefined) {
    out.cacheReadInputTokens = cacheReadInputTokens;
  }
  if (reasoningTokens !== undefined) {
    out.reasoningTokens = reasoningTokens;
  }

  return Object.keys(out).length > 0 ? out : undefined;
}

function normalizeResponsesStopReason(response: ResponsesResponse): string | undefined {
  const status = typeof response.status === 'string' ? response.status.trim().toLowerCase() : '';
  const incomplete = isRecord(response.incomplete_details)
    ? String(response.incomplete_details.reason ?? '').trim().toLowerCase()
    : '';

  if (status === 'incomplete' && incomplete.length > 0) {
    return incomplete;
  }

  if (status === 'completed') {
    return 'stop';
  }

  return status.length > 0 ? status : undefined;
}

function normalizeResponsesStopReasonFromEvents(events: ResponsesStreamEvent[]): string | undefined {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index] as unknown as AnyRecord;
    const eventType = typeof event.type === 'string' ? event.type : '';
    const response = isRecord(event.response) ? event.response : undefined;

    if (eventType === 'response.incomplete' && response && isRecord(response.incomplete_details)) {
      const reason = String(response.incomplete_details.reason ?? '').trim().toLowerCase();
      if (reason.length > 0) {
        return reason;
      }
    }
    if (eventType === 'response.completed') {
      return 'stop';
    }
    if (eventType === 'response.failed') {
      return 'failed';
    }
    if (eventType === 'response.cancelled') {
      return 'cancelled';
    }
  }
  return undefined;
}

function extractResponsesStreamText(events: ResponsesStreamEvent[]): string {
  const chunks: string[] = [];

  for (const event of events) {
    const record = event as unknown as AnyRecord;
    const type = typeof record.type === 'string' ? record.type : '';

    if (type === 'response.output_text.delta' && typeof record.delta === 'string') {
      chunks.push(record.delta);
      continue;
    }

    if (type === 'response.output_text.done' && typeof record.text === 'string' && chunks.length === 0) {
      chunks.push(record.text);
      continue;
    }

    if (type === 'response.refusal.delta' && typeof record.delta === 'string') {
      chunks.push(record.delta);
    }
  }

  return chunks.join('');
}

function findResponsesFinalFromEvents(events: ResponsesStreamEvent[]): ResponsesResponse | undefined {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index] as unknown as AnyRecord;
    if ((event.type === 'response.completed' || event.type === 'response.incomplete') && isRecord(event.response)) {
      return event.response as unknown as ResponsesResponse;
    }
  }
  return undefined;
}

function extractText(value: unknown): string {
  if (typeof value === 'string') {
    return value.trim();
  }

  if (Array.isArray(value)) {
    const chunks: string[] = [];
    for (const item of value) {
      if (typeof item === 'string') {
        if (item.trim().length > 0) {
          chunks.push(item.trim());
        }
        continue;
      }
      if (!isRecord(item)) {
        continue;
      }
      const itemType = typeof item.type === 'string' ? item.type : '';
      if ((itemType === 'text' || itemType === 'input_text' || itemType === 'output_text') && typeof item.text === 'string') {
        if (item.text.trim().length > 0) {
          chunks.push(item.text.trim());
        }
        continue;
      }
      if (itemType === 'refusal' && typeof item.refusal === 'string') {
        if (item.refusal.trim().length > 0) {
          chunks.push(item.refusal.trim());
        }
        continue;
      }
      if (typeof item.content === 'string' && item.content.trim().length > 0) {
        chunks.push(item.content.trim());
      }
    }
    return chunks.join('\n');
  }

  if (isRecord(value)) {
    if (typeof value.text === 'string' && value.text.trim().length > 0) {
      return value.text.trim();
    }
    if (typeof value.content === 'string' && value.content.trim().length > 0) {
      return value.content.trim();
    }
    if (typeof value.refusal === 'string' && value.refusal.trim().length > 0) {
      return value.refusal.trim();
    }
  }

  return '';
}

function readIntFromAny(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value)) {
    const asInt = Math.trunc(value);
    return Number.isNaN(asInt) ? undefined : asInt;
  }
  if (typeof value === 'string') {
    const parsed = Number.parseInt(value.trim(), 10);
    return Number.isNaN(parsed) ? undefined : parsed;
  }
  return undefined;
}

function readNumberFromAny(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === 'string') {
    const parsed = Number.parseFloat(value.trim());
    return Number.isNaN(parsed) ? undefined : parsed;
  }
  return undefined;
}

function openAIThinkingBudget(reasoning: unknown): number | undefined {
  if (!isRecord(reasoning)) {
    return undefined;
  }
  for (const key of ['budget_tokens', 'thinking_budget', 'thinkingBudget', 'max_output_tokens']) {
    const value = readIntFromAny(reasoning[key]);
    if (value !== undefined) {
      return value;
    }
  }
  return undefined;
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

function canonicalToolChoice(value: unknown): string | undefined {
  if (!hasValue(value)) {
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
  const encoded = jsonString(value);
  return encoded.length > 0 ? encoded : undefined;
}

function jsonArtifact(type: 'request' | 'response' | 'tools' | 'provider_event', name: string, payload: unknown) {
  return {
    type,
    name,
    payload: jsonString(payload),
    mimeType: 'application/json',
  };
}

function jsonString(value: unknown): string {
  try {
    return JSON.stringify(value, objectKeySorter);
  } catch {
    return String(value ?? '');
  }
}

function objectKeySorter(_key: string, value: unknown): unknown {
  if (!isRecord(value) || Array.isArray(value)) {
    return value;
  }
  const sorted: Record<string, unknown> = {};
  for (const key of Object.keys(value).sort()) {
    sorted[key] = value[key];
  }
  return sorted;
}

function hasValue(value: unknown): boolean {
  return value !== undefined && value !== null;
}

function isRecord(value: unknown): value is AnyRecord {
  return typeof value === 'object' && value !== null;
}

function asDate(value: Date | string | number | undefined): Date | undefined {
  if (value === undefined) {
    return undefined;
  }
  const date = value instanceof Date ? new Date(value) : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return undefined;
  }
  return date;
}
