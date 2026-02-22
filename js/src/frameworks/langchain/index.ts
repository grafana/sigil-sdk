import type { SigilClient } from '../../client.js';
import { SigilFrameworkHandler, type FrameworkHandlerOptions } from '../shared.js';

export type { FrameworkHandlerOptions };

type CallbackConfig = Record<string, unknown> & { callbacks?: unknown };

export class SigilLangChainHandler extends SigilFrameworkHandler {
  name = 'sigil_langchain_handler';

  constructor(client: SigilClient, options: FrameworkHandlerOptions = {}) {
    super(client, 'langchain', 'javascript', options);
  }

  async handleLLMStart(
    serialized: unknown,
    prompts: unknown,
    runId: string,
    parentRunId?: string,
    extraParams?: Record<string, unknown>,
    tags?: string[],
    metadata?: Record<string, unknown>,
    runName?: string
  ): Promise<void> {
    this.onLLMStart(serialized, prompts, runId, parentRunId, extraParams, tags, metadata, runName);
  }

  async handleChatModelStart(
    serialized: unknown,
    messages: unknown,
    runId: string,
    parentRunId?: string,
    extraParams?: Record<string, unknown>,
    tags?: string[],
    metadata?: Record<string, unknown>,
    runName?: string
  ): Promise<void> {
    this.onChatModelStart(serialized, messages, runId, parentRunId, extraParams, tags, metadata, runName);
  }

  async handleLLMNewToken(
    token: string,
    _idx: unknown,
    runId: string
  ): Promise<void> {
    this.onLLMNewToken(token, runId);
  }

  async handleLLMEnd(output: unknown, runId: string): Promise<void> {
    this.onLLMEnd(output, runId);
  }

  async handleLLMError(error: unknown, runId: string): Promise<void> {
    this.onLLMError(error, runId);
  }

  async handleToolStart(
    serialized: unknown,
    input: unknown,
    runId: string,
    parentRunId?: string,
    tags?: string[],
    metadata?: Record<string, unknown>,
    runName?: string
  ): Promise<void> {
    this.onToolStart(serialized, input, runId, parentRunId, tags, metadata, runName);
  }

  async handleToolEnd(output: unknown, runId: string): Promise<void> {
    this.onToolEnd(output, runId);
  }

  async handleToolError(error: unknown, runId: string): Promise<void> {
    this.onToolError(error, runId);
  }

  async handleChainStart(
    serialized: unknown,
    _inputs: unknown,
    runId: string,
    parentRunId?: string,
    tags?: string[],
    metadata?: Record<string, unknown>,
    runType?: string,
    runName?: string
  ): Promise<void> {
    this.onChainStart(serialized, runId, parentRunId, tags, metadata, runType, runName);
  }

  async handleChainEnd(_outputs: unknown, runId: string): Promise<void> {
    this.onChainEnd(runId);
  }

  async handleChainError(error: unknown, runId: string): Promise<void> {
    this.onChainError(error, runId);
  }

  async handleRetrieverStart(
    serialized: unknown,
    _query: string,
    runId: string,
    parentRunId?: string,
    tags?: string[],
    metadata?: Record<string, unknown>,
    runName?: string
  ): Promise<void> {
    this.onRetrieverStart(serialized, runId, parentRunId, tags, metadata, runName);
  }

  async handleRetrieverEnd(_documents: unknown, runId: string): Promise<void> {
    this.onRetrieverEnd(runId);
  }

  async handleRetrieverError(error: unknown, runId: string): Promise<void> {
    this.onRetrieverError(error, runId);
  }
}

export function createSigilLangChainHandler(
  client: SigilClient,
  options: FrameworkHandlerOptions = {}
): SigilLangChainHandler {
  return new SigilLangChainHandler(client, options);
}

export function withSigilLangChainCallbacks<T extends CallbackConfig>(
  config: T | undefined,
  client: SigilClient,
  options: FrameworkHandlerOptions = {}
): T & { callbacks: unknown[] } {
  const handler = createSigilLangChainHandler(client, options);
  const base = { ...(config ?? {}) } as CallbackConfig;
  const existingValue = base.callbacks;
  const callbacks = Array.isArray(existingValue)
    ? [...existingValue]
    : existingValue === undefined
      ? []
      : [existingValue];
  if (!callbacks.some((callback) => callback instanceof SigilLangChainHandler)) {
    callbacks.push(handler);
  }
  return {
    ...base,
    callbacks,
  } as T & { callbacks: unknown[] };
}
