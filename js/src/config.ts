import type { GenerationExportConfig, SigilLogger, SigilSdkConfig, SigilSdkConfigInput, TraceConfig } from './types.js';

export const defaultTraceConfig: TraceConfig = {
  protocol: 'http',
  endpoint: 'http://localhost:4318/v1/traces',
  insecure: true,
};

export const defaultGenerationExportConfig: GenerationExportConfig = {
  protocol: 'http',
  endpoint: 'http://localhost:8080/api/v1/generations:export',
  insecure: true,
  batchSize: 100,
  flushIntervalMs: 1_000,
  queueSize: 2_000,
  maxRetries: 5,
  initialBackoffMs: 100,
  maxBackoffMs: 5_000,
  payloadMaxBytes: 4 << 20,
};

export const defaultLogger: SigilLogger = {
  debug(message: string, ...args: unknown[]) {
    console.debug(message, ...args);
  },
  warn(message: string, ...args: unknown[]) {
    console.warn(message, ...args);
  },
  error(message: string, ...args: unknown[]) {
    console.error(message, ...args);
  },
};

export function defaultConfig(): SigilSdkConfig {
  return {
    trace: cloneTraceConfig(defaultTraceConfig),
    generationExport: cloneGenerationExportConfig(defaultGenerationExportConfig),
  };
}

export function mergeConfig(config: SigilSdkConfigInput): SigilSdkConfig {
  return {
    trace: mergeTraceConfig(config.trace),
    generationExport: mergeGenerationExportConfig(config.generationExport),
    generationExporter: config.generationExporter,
    tracer: config.tracer,
    logger: config.logger,
    now: config.now,
    sleep: config.sleep,
  };
}

function mergeTraceConfig(config: Partial<TraceConfig> | undefined): TraceConfig {
  return {
    ...defaultTraceConfig,
    ...config,
    headers: config?.headers !== undefined ? { ...config.headers } : undefined,
  };
}

function mergeGenerationExportConfig(config: Partial<GenerationExportConfig> | undefined): GenerationExportConfig {
  return {
    ...defaultGenerationExportConfig,
    ...config,
    headers: config?.headers !== undefined ? { ...config.headers } : undefined,
  };
}

function cloneTraceConfig(config: TraceConfig): TraceConfig {
  return {
    ...config,
    headers: config.headers ? { ...config.headers } : undefined,
  };
}

function cloneGenerationExportConfig(config: GenerationExportConfig): GenerationExportConfig {
  return {
    ...config,
    headers: config.headers ? { ...config.headers } : undefined,
  };
}
