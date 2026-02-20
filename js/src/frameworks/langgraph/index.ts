import type { SigilClient } from '../../client.js';
import { SigilFrameworkHandler, type FrameworkHandlerOptions } from '../shared.js';

export type { FrameworkHandlerOptions };

export class SigilLangGraphHandler extends SigilFrameworkHandler {
  name = 'sigil_langgraph_handler';

  constructor(client: SigilClient, options: FrameworkHandlerOptions = {}) {
    super(client, 'langgraph', 'javascript', options);
  }

  async handleLLMStart(
    serialized: unknown,
    prompts: unknown,
    runId: string,
    _parentRunId?: string,
    extraParams?: Record<string, unknown>
  ): Promise<void> {
    this.onLLMStart(serialized, prompts, runId, extraParams);
  }

  async handleChatModelStart(
    serialized: unknown,
    messages: unknown,
    runId: string,
    _parentRunId?: string,
    extraParams?: Record<string, unknown>
  ): Promise<void> {
    this.onChatModelStart(serialized, messages, runId, extraParams);
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
}
