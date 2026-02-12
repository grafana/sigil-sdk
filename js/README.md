# Grafana Sigil TypeScript/JavaScript SDK

Sigil records normalized LLM generation and tool-execution telemetry with OpenTelemetry traces.

## Installation

```bash
pnpm add @grafana/sigil-sdk-js
```

## Quick Start

```ts
import { SigilClient } from "@grafana/sigil-sdk-js";

const client = new SigilClient({
  generationExport: {
    protocol: "http",
    endpoint: "http://localhost:8080/api/v1/generations:export",
    auth: { mode: "tenant", tenantId: "dev-tenant" },
  },
  trace: {
    protocol: "http",
    endpoint: "http://localhost:4318/v1/traces",
    auth: { mode: "none" },
  },
});

await client.startGeneration(
  {
    conversationId: "conv-1",
    model: { provider: "openai", name: "gpt-5" },
  },
  async (recorder) => {
    const outputText = "Hello from model";
    recorder.setResult({
      output: [{ role: "assistant", content: outputText }],
    });
  }
);

await client.shutdown();
```

## Core API

- `startGeneration(...)` and `startStreamingGeneration(...)`
- `startToolExecution(...)`
- Recorder methods: `setResult(...)`, `setCallError(...)`, `end()`, `getError()`
- Lifecycle: `flush()`, `shutdown()`

### Manual `try/finally` style

```ts
const recorder = client.startGeneration({
  model: { provider: "anthropic", name: "claude-sonnet-4-5" },
});

try {
  recorder.setResult({
    output: [{ role: "assistant", content: "Done" }],
  });
} catch (error) {
  recorder.setCallError(error);
  throw error;
} finally {
  recorder.end();
}
```

## Tool Execution Example

```ts
await client.startToolExecution(
  {
    toolName: "weather",
    includeContent: true,
  },
  async (recorder) => {
    recorder.setResult({
      arguments: { city: "Paris" },
      result: { temp_c: 18 },
    });
  }
);
```

## Provider Helpers

- OpenAI: `docs/providers/openai.md`
- Anthropic: `docs/providers/anthropic.md`
- Gemini: `docs/providers/gemini.md`

## Behavior

- Generation modes are explicit: `SYNC` and `STREAM`.
- Generation export supports HTTP and gRPC.
- Trace export supports OTLP HTTP and OTLP gRPC.
- Exports are asynchronous with bounded queueing and retry/backoff.
- `flush()` drains queued generations; `shutdown()` flushes and closes exporters.
- Empty tool names produce a no-op tool recorder.
- Raw provider artifacts are opt-in (`rawArtifacts: true`).

## Per-export auth modes

Auth is configured independently for `generationExport` and `trace`.

- `mode: "none"`
- `mode: "tenant"` (requires `tenantId`, injects `X-Scope-OrgID`)
- `mode: "bearer"` (requires `bearerToken`, injects `Authorization: Bearer <token>`)

Invalid mode/field combinations throw during client config resolution.

If explicit headers already contain `Authorization` or `X-Scope-OrgID`, explicit headers take precedence.

```ts
const client = new SigilClient({
  generationExport: {
    protocol: "http",
    endpoint: "http://localhost:8080/api/v1/generations:export",
    auth: { mode: "tenant", tenantId: "prod-tenant" },
  },
  trace: {
    protocol: "grpc",
    endpoint: "localhost:4317",
    auth: { mode: "none" }, // traces through Collector/Alloy
  },
});
```

## Env-secret wiring example

The SDK does not auto-load env vars. Resolve env secrets in your app and map them into config.

```ts
const generationBearerToken = (process.env.SIGIL_GEN_BEARER_TOKEN ?? "").trim();
const traceBearerToken = (process.env.SIGIL_TRACE_BEARER_TOKEN ?? "").trim();

const client = new SigilClient({
  generationExport: {
    protocol: "http",
    endpoint: "http://localhost:8080/api/v1/generations:export",
    auth:
      generationBearerToken.length > 0
        ? { mode: "bearer", bearerToken: generationBearerToken }
        : { mode: "tenant", tenantId: "dev-tenant" },
  },
  trace: {
    protocol: "grpc",
    endpoint: "localhost:4317",
    auth:
      traceBearerToken.length > 0
        ? { mode: "bearer", bearerToken: traceBearerToken }
        : { mode: "none" },
  },
});
```

Common topology:

- Generations direct to Sigil: generation `tenant` mode.
- Traces via OTEL Collector/Alloy: trace `none` or `bearer` mode.
- Enterprise proxy: generation `bearer` mode to proxy; proxy authenticates and forwards tenant header upstream.
