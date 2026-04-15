import type {
  ApiConfig,
  ContentCaptureMode,
  EmbeddingCaptureConfig,
  ExportAuthConfig,
  GenerationExportConfig,
  SigilLogger,
  SigilSdkConfig,
  SigilSdkConfigInput,
} from './types.js';

const tenantHeaderName = 'X-Scope-OrgID';
const authorizationHeaderName = 'Authorization';

const defaultExportAuthConfig: ExportAuthConfig = {
  mode: 'none',
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

export const defaultAPIConfig: ApiConfig = {
  endpoint: 'http://localhost:8080',
};

export const defaultEmbeddingCaptureConfig: EmbeddingCaptureConfig = {
  captureInput: false,
  maxInputItems: 20,
  maxTextLength: 1024,
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

export const defaultContentCaptureMode: ContentCaptureMode = 'default';

export function defaultConfig(): SigilSdkConfig {
  return {
    generationExport: cloneGenerationExportConfig(defaultGenerationExportConfig),
    api: cloneAPIConfig(defaultAPIConfig),
    embeddingCapture: cloneEmbeddingCaptureConfig(defaultEmbeddingCaptureConfig),
    contentCapture: defaultContentCaptureMode,
  };
}

export function mergeConfig(config: SigilSdkConfigInput): SigilSdkConfig {
  return {
    generationExport: mergeGenerationExportConfig(config.generationExport),
    api: mergeAPIConfig(config.api),
    embeddingCapture: mergeEmbeddingCaptureConfig(config.embeddingCapture),
    contentCapture: config.contentCapture ?? defaultContentCaptureMode,
    contentCaptureResolver: config.contentCaptureResolver,
    generationExporter: config.generationExporter,
    tracer: config.tracer,
    meter: config.meter,
    logger: config.logger,
    now: config.now,
    sleep: config.sleep,
  };
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

function mergeAPIConfig(config: Partial<ApiConfig> | undefined): ApiConfig {
  return {
    ...defaultAPIConfig,
    ...config,
  };
}

function mergeEmbeddingCaptureConfig(config: Partial<EmbeddingCaptureConfig> | undefined): EmbeddingCaptureConfig {
  return {
    ...defaultEmbeddingCaptureConfig,
    ...config,
  };
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
  label: string,
): Record<string, string> | undefined {
  const mode = (auth.mode ?? 'none').trim().toLowerCase();
  const tenantId = auth.tenantId?.trim() ?? '';
  const bearerToken = auth.bearerToken?.trim() ?? '';
  const out = headers ? { ...headers } : undefined;

  if (mode === 'none') {
    const basicUser = auth.basicUser?.trim() ?? '';
    const basicPassword = auth.basicPassword?.trim() ?? '';
    if (tenantId.length > 0 || bearerToken.length > 0 || basicUser.length > 0 || basicPassword.length > 0) {
      throw new Error(`${label} auth mode "none" does not allow credentials`);
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

  if (mode === 'basic') {
    const password = auth.basicPassword?.trim() ?? '';
    if (password.length === 0) {
      throw new Error(`${label} auth mode "basic" requires basicPassword`);
    }
    let user = auth.basicUser?.trim() ?? '';
    if (user.length === 0) {
      user = tenantId;
    }
    if (user.length === 0) {
      throw new Error(`${label} auth mode "basic" requires basicUser or tenantId`);
    }
    const result: Record<string, string> = { ...(out ?? {}) };
    if (!hasHeaderKey(result, authorizationHeaderName)) {
      const encoded = new TextEncoder().encode(`${user}:${password}`);
      result[authorizationHeaderName] = `Basic ${btoa(String.fromCharCode(...encoded))}`;
    }
    if (tenantId.length > 0 && !hasHeaderKey(result, tenantHeaderName)) {
      result[tenantHeaderName] = tenantId;
    }
    return result;
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

function cloneGenerationExportConfig(config: GenerationExportConfig): GenerationExportConfig {
  return {
    ...config,
    auth: { ...config.auth },
    headers: config.headers ? { ...config.headers } : undefined,
  };
}

function cloneAPIConfig(config: ApiConfig): ApiConfig {
  return {
    ...config,
  };
}

function cloneEmbeddingCaptureConfig(config: EmbeddingCaptureConfig): EmbeddingCaptureConfig {
  return {
    ...config,
  };
}
