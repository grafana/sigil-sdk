import * as grpc from '@grpc/grpc-js';
import { metrics, trace, type Meter, type Tracer } from '@opentelemetry/api';
import { OTLPMetricExporter as OTLPMetricExporterGRPC } from '@opentelemetry/exporter-metrics-otlp-grpc';
import { OTLPMetricExporter as OTLPMetricExporterHTTP } from '@opentelemetry/exporter-metrics-otlp-http';
import { OTLPTraceExporter as OTLPTraceExporterGRPC } from '@opentelemetry/exporter-trace-otlp-grpc';
import { OTLPTraceExporter as OTLPTraceExporterHTTP } from '@opentelemetry/exporter-trace-otlp-http';
import { MeterProvider, PeriodicExportingMetricReader } from '@opentelemetry/sdk-metrics';
import { BatchSpanProcessor, BasicTracerProvider, type SpanExporter } from '@opentelemetry/sdk-trace-base';
import type { TraceConfig } from './types.js';

const instrumentationName = 'github.com/grafana/sigil/sdks/js';

export interface TraceRuntime {
  tracer: Tracer;
  meter: Meter;
  flush(): Promise<void>;
  shutdown(): Promise<void>;
}

export function createTraceRuntime(
  config: TraceConfig,
  onError?: (message: string, error: unknown) => void
): TraceRuntime {
  try {
    const traceExporter = createTraceExporter(config);
    const traceProvider = new BasicTracerProvider({
      spanProcessors: [
        new BatchSpanProcessor(traceExporter, {
          maxQueueSize: 2_048,
          maxExportBatchSize: 512,
          scheduledDelayMillis: 1_000,
          exportTimeoutMillis: 1_000,
        }),
      ],
    });

    const metricExporter = createMetricExporter(config);
    const metricReader = new PeriodicExportingMetricReader({
      exporter: metricExporter,
      exportIntervalMillis: 1_000,
      exportTimeoutMillis: 1_000,
    });
    const meterProvider = new MeterProvider({
      readers: [metricReader],
    });

    return {
      tracer: traceProvider.getTracer(instrumentationName),
      meter: meterProvider.getMeter(instrumentationName),
      async flush() {
        await traceProvider.forceFlush();
        await meterProvider.forceFlush();
      },
      async shutdown() {
        await traceProvider.shutdown();
        await meterProvider.shutdown();
      },
    };
  } catch (error) {
    onError?.('sigil telemetry exporter init failed', error);
    return {
      tracer: trace.getTracer(instrumentationName),
      meter: metrics.getMeter(instrumentationName),
      async flush() {},
      async shutdown() {},
    };
  }
}

function createTraceExporter(config: TraceConfig): SpanExporter {
  switch (config.protocol) {
    case 'grpc': {
      const endpoint = parseEndpoint(config.endpoint);
      const url = normalizeGRPCEndpoint(endpoint, config.insecure);
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

function createMetricExporter(config: TraceConfig) {
  switch (config.protocol) {
    case 'grpc': {
      const endpoint = parseEndpoint(config.endpoint);
      const url = normalizeGRPCEndpoint(endpoint, config.insecure);
      const metadata = toGRPCMetadata(config.headers);
      const insecure = config.insecure || endpoint.insecure;
      return new OTLPMetricExporterGRPC({
        url,
        metadata,
        credentials: insecure ? grpc.credentials.createInsecure() : grpc.credentials.createSsl(),
        timeoutMillis: 1_000,
      });
    }
    case 'http':
    default:
      return new OTLPMetricExporterHTTP({
        url: normalizeHTTPMetricsEndpoint(parseEndpoint(config.endpoint), config.insecure),
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

function normalizeHTTPMetricsEndpoint(endpoint: ParsedEndpoint, insecureConfig: boolean): string {
  const scheme = endpoint.scheme ?? (insecureConfig || endpoint.insecure ? 'http' : 'https');
  const trimmedPath = endpoint.path.length > 1 && endpoint.path.endsWith('/')
    ? endpoint.path.slice(0, -1)
    : endpoint.path;
  const path = trimmedPath.length === 0 || trimmedPath === '/'
    ? '/v1/metrics'
    : (trimmedPath === '/v1/traces' || trimmedPath.endsWith('/v1/traces'))
      ? `${trimmedPath.slice(0, -'/v1/traces'.length)}/v1/metrics`
      : trimmedPath;
  return `${scheme}://${endpoint.host}${path}`;
}

function normalizeGRPCEndpoint(endpoint: ParsedEndpoint, insecureConfig: boolean): string {
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
