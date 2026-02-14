import type { Meter, Tracer } from '@opentelemetry/api';

/** Supported OTLP trace export protocols. */
export type TraceProtocol = 'grpc' | 'http';
/** Supported generation export protocols. */
export type GenerationExportProtocol = 'grpc' | 'http' | 'none';
/** Generation execution mode. */
export type GenerationMode = 'SYNC' | 'STREAM';
/** Supported auth modes for transport exports. */
export type ExportAuthMode = 'none' | 'tenant' | 'bearer';

/** Per-export auth configuration. */
export interface ExportAuthConfig {
  mode: ExportAuthMode;
  tenantId?: string;
  bearerToken?: string;
}

/** Trace exporter configuration. */
export interface TraceConfig {
  /** OTLP protocol for trace export. */
  protocol: TraceProtocol;
  /** Trace endpoint. Examples: `http://localhost:4318/v1/traces`, `localhost:4317`. */
  endpoint: string;
  /** Optional transport headers/metadata. */
  headers?: Record<string, string>;
  /** Optional auth mode for trace transport. */
  auth: ExportAuthConfig;
  /** Force insecure transport when protocol allows it. */
  insecure: boolean;
}

/** Generation exporter runtime configuration. */
export interface GenerationExportConfig {
  /** Export protocol for generations. */
  protocol: GenerationExportProtocol;
  /** Generation ingest endpoint. */
  endpoint: string;
  /** Optional transport headers/metadata. */
  headers?: Record<string, string>;
  /** Optional auth mode for generation transport. */
  auth: ExportAuthConfig;
  /** Force insecure transport when protocol allows it. */
  insecure: boolean;
  /** Max generations per export request. */
  batchSize: number;
  /** Periodic drain interval for queued generations. */
  flushIntervalMs: number;
  /** Max queued generations before enqueue errors. */
  queueSize: number;
  /** Max retry attempts after first failed export. */
  maxRetries: number;
  /** Initial backoff between retry attempts. */
  initialBackoffMs: number;
  /** Max backoff cap between retry attempts. */
  maxBackoffMs: number;
  /** Optional per-generation max encoded payload size in bytes. */
  payloadMaxBytes: number;
}

/** Optional logger hooks used by the SDK runtime. */
export interface SigilLogger {
  debug?: (message: string, ...args: unknown[]) => void;
  warn?: (message: string, ...args: unknown[]) => void;
  error?: (message: string, ...args: unknown[]) => void;
}

/** Per-generation ingest result. */
export interface ExportGenerationResult {
  generationId: string;
  accepted: boolean;
  error?: string;
}

/** Generation export request payload. */
export interface ExportGenerationsRequest {
  generations: Generation[];
}

/** Generation export response payload. */
export interface ExportGenerationsResponse {
  results: ExportGenerationResult[];
}

/** Pluggable generation exporter interface. */
export interface GenerationExporter {
  exportGenerations(request: ExportGenerationsRequest): Promise<ExportGenerationsResponse>;
  shutdown?(): Promise<void> | void;
}

/** Fully resolved SDK configuration. */
export interface SigilSdkConfig {
  trace: TraceConfig;
  generationExport: GenerationExportConfig;
  generationExporter?: GenerationExporter;
  tracer?: Tracer;
  meter?: Meter;
  logger?: SigilLogger;
  now?: () => Date;
  sleep?: (durationMs: number) => Promise<void>;
}

/** Partial SDK configuration passed by callers. */
export interface SigilSdkConfigInput {
  trace?: Partial<TraceConfig>;
  generationExport?: Partial<GenerationExportConfig>;
  generationExporter?: GenerationExporter;
  tracer?: Tracer;
  meter?: Meter;
  logger?: SigilLogger;
  now?: () => Date;
  sleep?: (durationMs: number) => Promise<void>;
}

/** Provider/model identity. */
export interface ModelRef {
  provider: string;
  name: string;
}

/** Tool definition visible to the model. */
export interface ToolDefinition {
  name: string;
  description?: string;
  type?: string;
  /** JSON schema string for tool input; mapped to `input_schema_json` on gRPC export. */
  inputSchemaJSON?: string;
}

/** Token usage counters. */
export interface TokenUsage {
  inputTokens?: number;
  outputTokens?: number;
  totalTokens?: number;
  cacheReadInputTokens?: number;
  cacheWriteInputTokens?: number;
  cacheCreationInputTokens?: number;
  reasoningTokens?: number;
}

/** Provider-specific metadata attached to message parts. */
export interface PartMetadata {
  providerType?: string;
}

/** Tool call part payload. */
export interface ToolCallPart {
  id?: string;
  name: string;
  inputJSON?: string;
}

/** Tool result part payload. */
export interface ToolResultPart {
  toolCallId?: string;
  name?: string;
  content?: string;
  contentJSON?: string;
  isError?: boolean;
}

/** Typed message part union. */
export type MessagePart =
  | { type: 'text'; text: string; metadata?: PartMetadata }
  | { type: 'thinking'; thinking: string; metadata?: PartMetadata }
  | { type: 'tool_call'; toolCall: ToolCallPart; metadata?: PartMetadata }
  | { type: 'tool_result'; toolResult: ToolResultPart; metadata?: PartMetadata };

/** Normalized message payload. */
export interface Message {
  /** Role value: `user`, `assistant`, or `tool`. */
  role: string;
  /** Optional text shorthand; mapped to a text part for gRPC export. */
  content?: string;
  name?: string;
  /** Preferred typed part representation. */
  parts?: MessagePart[];
}

/** Optional raw provider artifact. */
export interface Artifact {
  /** Artifact kind: `request`, `response`, `tools`, or `provider_event`. */
  type: string;
  name?: string;
  payload?: string;
  mimeType?: string;
  recordId?: string;
  uri?: string;
}

/** Generation start seed fields. */
export interface GenerationStart {
  id?: string;
  conversationId?: string;
  agentName?: string;
  agentVersion?: string;
  mode?: GenerationMode;
  operationName?: string;
  model: ModelRef;
  systemPrompt?: string;
  maxTokens?: number;
  temperature?: number;
  topP?: number;
  toolChoice?: string;
  thinkingEnabled?: boolean;
  tools?: ToolDefinition[];
  tags?: Record<string, string>;
  metadata?: Record<string, unknown>;
  startedAt?: Date;
}

/** Final generation result fields. */
export interface GenerationResult {
  conversationId?: string;
  agentName?: string;
  agentVersion?: string;
  operationName?: string;
  responseId?: string;
  responseModel?: string;
  maxTokens?: number;
  temperature?: number;
  topP?: number;
  toolChoice?: string;
  thinkingEnabled?: boolean;
  input?: Message[];
  output?: Message[];
  tools?: ToolDefinition[];
  usage?: TokenUsage;
  stopReason?: string;
  completedAt?: Date;
  tags?: Record<string, string>;
  metadata?: Record<string, unknown>;
  artifacts?: Artifact[];
}

/** Fully normalized generation record exported by the SDK. */
export interface Generation {
  id: string;
  conversationId?: string;
  agentName?: string;
  agentVersion?: string;
  mode: GenerationMode;
  operationName: string;
  traceId?: string;
  spanId?: string;
  model: ModelRef;
  systemPrompt?: string;
  responseId?: string;
  responseModel?: string;
  maxTokens?: number;
  temperature?: number;
  topP?: number;
  toolChoice?: string;
  thinkingEnabled?: boolean;
  input?: Message[];
  output?: Message[];
  tools?: ToolDefinition[];
  usage?: TokenUsage;
  stopReason?: string;
  startedAt: Date;
  completedAt: Date;
  tags?: Record<string, string>;
  metadata?: Record<string, unknown>;
  artifacts?: Artifact[];
  callError?: string;
}

/** Tool execution start seed fields. */
export interface ToolExecutionStart {
  toolName: string;
  toolCallId?: string;
  toolType?: string;
  toolDescription?: string;
  conversationId?: string;
  agentName?: string;
  agentVersion?: string;
  includeContent?: boolean;
  startedAt?: Date;
}

/** Tool execution completion fields. */
export interface ToolExecutionResult {
  arguments?: unknown;
  result?: unknown;
  completedAt?: Date;
}

/** Final tool execution record kept in debug snapshots. */
export interface ToolExecution {
  toolName: string;
  toolCallId?: string;
  toolType?: string;
  toolDescription?: string;
  conversationId?: string;
  agentName?: string;
  agentVersion?: string;
  includeContent: boolean;
  startedAt: Date;
  completedAt: Date;
  arguments?: unknown;
  result?: unknown;
  callError?: string;
}

/** Recorder API for generation lifecycle. */
export interface GenerationRecorder {
  setResult(result: GenerationResult): void;
  setCallError(error: unknown): void;
  setFirstTokenAt(firstTokenAt: Date): void;
  end(): void;
  getError(): Error | undefined;
}

/** Recorder API for tool execution lifecycle. */
export interface ToolExecutionRecorder {
  setResult(result: ToolExecutionResult): void;
  setCallError(error: unknown): void;
  end(): void;
  getError(): Error | undefined;
}

/** In-memory snapshot for tests/debugging. */
export interface SigilDebugSnapshot {
  generations: Generation[];
  toolExecutions: ToolExecution[];
  queueSize: number;
}

/** Callback form used by recorder helper APIs. */
export type RecorderCallback<TRecorder, TResult> = (recorder: TRecorder) => TResult | Promise<TResult>;

/** Shared recorder methods used by callback runtime helpers. */
export interface RecorderWithError {
  setCallError(error: unknown): void;
  end(): void;
  getError(): Error | undefined;
}
