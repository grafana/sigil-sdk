import type {
  ExportAuthConfig,
  GenerationExportConfig,
  SigilLogger,
  SigilSdkConfig,
  SigilSdkConfigInput,
  TraceConfig,
} from './types.js';

const tenantHeaderName = 'X-Scope-OrgID';
const authorizationHeaderName = 'Authorization';

const defaultExportAuthConfig: ExportAuthConfig = {
  mode: 'none',
};

export const defaultTraceConfig: TraceConfig = {
  protocol: 'http',
  endpoint: 'http://localhost:4318/v1/traces',
  auth: defaultExportAuthConfig,
  insecure: true,
};

export const defaultGenerationExportConfig: GenerationExportConfig = {
  protocol: 'http',
  endpoint: 'http://localhost:8080/api/v1/generations:export',
  auth: defaultExportAuthConfig,
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
    meter: config.meter,
    logger: config.logger,
    now: config.now,
    sleep: config.sleep,
  };
}

function mergeTraceConfig(config: Partial<TraceConfig> | undefined): TraceConfig {
  const auth = mergeAuthConfig(config?.auth);
  const headers = config?.headers !== undefined ? { ...config.headers } : undefined;
  const merged: TraceConfig = {
    ...defaultTraceConfig,
    ...config,
    auth,
    headers,
  };
  merged.headers = resolveHeadersWithAuth(merged.headers, merged.auth, 'trace');
  return merged;
}

function mergeGenerationExportConfig(config: Partial<GenerationExportConfig> | undefined): GenerationExportConfig {
  const auth = mergeAuthConfig(config?.auth);
  const headers = config?.headers !== undefined ? { ...config.headers } : undefined;
  const merged: GenerationExportConfig = {
    ...defaultGenerationExportConfig,
    ...config,
    auth,
    headers,
  };
  merged.headers = resolveHeadersWithAuth(merged.headers, merged.auth, 'generation export');
  return merged;
}

function mergeAuthConfig(config: ExportAuthConfig | undefined): ExportAuthConfig {
  return {
    ...defaultExportAuthConfig,
    ...config,
  };
}

function resolveHeadersWithAuth(
  headers: Record<string, string> | undefined,
  auth: ExportAuthConfig,
  label: string
): Record<string, string> | undefined {
  const mode = (auth.mode ?? 'none').trim().toLowerCase();
  const tenantId = auth.tenantId?.trim() ?? '';
  const bearerToken = auth.bearerToken?.trim() ?? '';
  const out = headers ? { ...headers } : undefined;

  if (mode === 'none') {
    if (tenantId.length > 0 || bearerToken.length > 0) {
      throw new Error(`${label} auth mode "none" does not allow tenantId or bearerToken`);
    }
    return out;
  }

  if (mode === 'tenant') {
    if (tenantId.length === 0) {
      throw new Error(`${label} auth mode "tenant" requires tenantId`);
    }
    if (bearerToken.length > 0) {
      throw new Error(`${label} auth mode "tenant" does not allow bearerToken`);
    }
    if (hasHeaderKey(out, tenantHeaderName)) {
      return out;
    }
    return {
      ...(out ?? {}),
      [tenantHeaderName]: tenantId,
    };
  }

  if (mode === 'bearer') {
    if (bearerToken.length === 0) {
      throw new Error(`${label} auth mode "bearer" requires bearerToken`);
    }
    if (tenantId.length > 0) {
      throw new Error(`${label} auth mode "bearer" does not allow tenantId`);
    }
    if (hasHeaderKey(out, authorizationHeaderName)) {
      return out;
    }
    return {
      ...(out ?? {}),
      [authorizationHeaderName]: formatBearerTokenValue(bearerToken),
    };
  }

  throw new Error(`unsupported ${label} auth mode: ${auth.mode}`);
}

function hasHeaderKey(headers: Record<string, string> | undefined, key: string): boolean {
  if (headers === undefined) {
    return false;
  }
  const target = key.toLowerCase();
  return Object.keys(headers).some((existing) => existing.toLowerCase() === target);
}

function formatBearerTokenValue(token: string): string {
  const value = token.trim();
  if (value.toLowerCase().startsWith('bearer ')) {
    return `Bearer ${value.slice(7).trim()}`;
  }
  return `Bearer ${value}`;
}

function cloneTraceConfig(config: TraceConfig): TraceConfig {
  return {
    ...config,
    auth: { ...config.auth },
    headers: config.headers ? { ...config.headers } : undefined,
  };
}

function cloneGenerationExportConfig(config: GenerationExportConfig): GenerationExportConfig {
  return {
    ...config,
    auth: { ...config.auth },
    headers: config.headers ? { ...config.headers } : undefined,
  };
}
