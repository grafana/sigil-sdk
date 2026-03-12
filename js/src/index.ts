export { SigilClient } from './client.js';
export { defaultConfig } from './config.js';
export {
  agentNameFromContext,
  agentVersionFromContext,
  conversationIdFromContext,
  conversationTitleFromContext,
  userIdFromContext,
  withAgentName,
  withAgentVersion,
  withConversationId,
  withConversationTitle,
  withUserId,
} from './context.js';
export type {
  ApiConfig,
  Artifact,
  ConversationRating,
  ConversationRatingInput,
  ConversationRatingSummary,
  ConversationRatingValue,
  EmbeddingCaptureConfig,
  EmbeddingRecorder,
  EmbeddingResult,
  EmbeddingStart,
  ExportGenerationResult,
  ExportGenerationsRequest,
  ExportGenerationsResponse,
  Generation,
  GenerationExporter,
  GenerationExportConfig,
  GenerationExportProtocol,
  GenerationMode,
  GenerationRecorder,
  GenerationResult,
  GenerationStart,
  Message,
  MessagePart,
  ModelRef,
  PartMetadata,
  RecorderCallback,
  SigilDebugSnapshot,
  SigilLogger,
  SigilSdkConfig,
  SigilSdkConfigInput,
  SubmitConversationRatingResponse,
  TokenUsage,
  ToolCallPart,
  ToolDefinition,
  ToolExecution,
  ToolExecutionRecorder,
  ToolExecutionResult,
  ToolExecutionStart,
  ToolResultPart,
} from './types.js';

export * as openai from './providers/openai.js';
export * as anthropic from './providers/anthropic.js';
export * as gemini from './providers/gemini.js';

import { SigilClient } from './client.js';
import type { SigilSdkConfigInput } from './types.js';

/** Convenience factory equivalent to `new SigilClient(config)`. */
export function createSigilClient(config: SigilSdkConfigInput = {}): SigilClient {
  return new SigilClient(config);
}
