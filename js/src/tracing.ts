import * as grpc from '@grpc/grpc-js';
import { trace, type Tracer } from '@opentelemetry/api';
import { OTLPTraceExporter as OTLPTraceExporterGRPC } from '@opentelemetry/exporter-trace-otlp-grpc';
import { OTLPTraceExporter as OTLPTraceExporterHTTP } from '@opentelemetry/exporter-trace-otlp-http';
import { BatchSpanProcessor, BasicTracerProvider, type SpanExporter } from '@opentelemetry/sdk-trace-base';
import type { TraceConfig } from './types.js';

const instrumentationName = 'github.com/grafana/sigil/sdks/js';

export interface TraceRuntime {
  tracer: Tracer;
  flush(): Promise<void>;
  shutdown(): Promise<void>;
}

export function createTraceRuntime(
  config: TraceConfig,
  onError?: (message: string, error: unknown) => void
): TraceRuntime {
  try {
    const exporter = createTraceExporter(config);
    const provider = new BasicTracerProvider({
      spanProcessors: [
        new BatchSpanProcessor(exporter, {
          maxQueueSize: 2_048,
          maxExportBatchSize: 512,
          scheduledDelayMillis: 1_000,
          exportTimeoutMillis: 1_000,
        }),
      ],
    });
    const tracer = provider.getTracer(instrumentationName);

    return {
      tracer,
      async flush() {
        await provider.forceFlush();
      },
      async shutdown() {
        await provider.shutdown();
      },
    };
  } catch (error) {
    onError?.('sigil trace exporter init failed', error);
    return {
      tracer: trace.getTracer(instrumentationName),
      async flush() {},
      async shutdown() {},
    };
  }
}

function createTraceExporter(config: TraceConfig): SpanExporter {
  switch (config.protocol) {
    case 'grpc': {
      const endpoint = parseEndpoint(config.endpoint);
      const url = normalizeGRPCTraceEndpoint(endpoint, config.insecure);
      const metadata = toGRPCMetadata(config.headers);
      const insecure = config.insecure || endpoint.insecure;

      return new OTLPTraceExporterGRPC({
        url,
        metadata,
        credentials: insecure ? grpc.credentials.createInsecure() : grpc.credentials.createSsl(),
        timeoutMillis: 1_000,
      });
    }
    case 'http':
    default:
      return new OTLPTraceExporterHTTP({
        url: normalizeHTTPTraceEndpoint(parseEndpoint(config.endpoint), config.insecure),
        headers: config.headers ? { ...config.headers } : undefined,
        timeoutMillis: 1_000,
      });
  }
}

function normalizeHTTPTraceEndpoint(endpoint: ParsedEndpoint, insecureConfig: boolean): string {
  const scheme = endpoint.scheme ?? (insecureConfig || endpoint.insecure ? 'http' : 'https');
  const path = endpoint.path.length === 0 || endpoint.path === '/' ? '/v1/traces' : endpoint.path;
  return `${scheme}://${endpoint.host}${path}`;
}

function normalizeGRPCTraceEndpoint(endpoint: ParsedEndpoint, insecureConfig: boolean): string {
  const scheme = endpoint.scheme ?? (insecureConfig || endpoint.insecure ? 'http' : 'https');
  return `${scheme}://${endpoint.host}`;
}

function toGRPCMetadata(headers: Record<string, string> | undefined): grpc.Metadata | undefined {
  if (headers === undefined || Object.keys(headers).length === 0) {
    return undefined;
  }

  const metadata = new grpc.Metadata();
  for (const [key, value] of Object.entries(headers)) {
    metadata.set(key, value);
  }
  return metadata;
}

interface ParsedEndpoint {
  host: string;
  path: string;
  scheme?: string;
  insecure: boolean;
}

function parseEndpoint(endpoint: string): ParsedEndpoint {
  const trimmed = endpoint.trim();
  if (trimmed.length === 0) {
    throw new Error('trace endpoint is required');
  }

  if (trimmed.includes('://')) {
    const parsed = new URL(trimmed);
    return {
      host: parsed.host,
      path: parsed.pathname,
      scheme: parsed.protocol.replace(':', ''),
      insecure: parsed.protocol === 'http:',
    };
  }

  const firstSlash = trimmed.indexOf('/');
  if (firstSlash === -1) {
    return {
      host: trimmed,
      path: '',
      insecure: false,
    };
  }

  return {
    host: trimmed.slice(0, firstSlash),
    path: trimmed.slice(firstSlash),
    insecure: false,
  };
}
