export { SigilClient } from './client.js';
export { defaultConfig } from './config.js';
export type {
  Artifact,
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
  TokenUsage,
  ToolCallPart,
  ToolDefinition,
  ToolExecution,
  ToolExecutionRecorder,
  ToolExecutionResult,
  ToolExecutionStart,
  ToolResultPart,
  TraceConfig,
  TraceProtocol,
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
