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
  api: {
    endpoint: "http://localhost:8080",
  },
  trace: {
    protocol: "http",
    endpoint: "http://localhost:4318/v1/traces",
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
- Generation export supports HTTP, gRPC, and `none` (instrumentation-only).
- Trace export supports OTLP HTTP and OTLP gRPC.
- Exports are asynchronous with bounded queueing and retry/backoff.
- `flush()` drains queued generations; `shutdown()` flushes and closes exporters.
- Empty tool names produce a no-op tool recorder.
- Raw provider artifacts are opt-in (`rawArtifacts: true`).

## Instrumentation-only mode (no generation send)

Set `generationExport.protocol` to `"none"` to keep generation/tool instrumentation and spans while disabling generation transport.

```ts
const client = new SigilClient({
  generationExport: {
    protocol: "none",
  },
  trace: {
    protocol: "http",
    endpoint: "http://localhost:4318/v1/traces",
  },
});
```

## SDK metrics

The SDK emits these OTel histograms automatically on the trace OTLP endpoint:

- `gen_ai.client.operation.duration`
- `gen_ai.client.token.usage`
- `gen_ai.client.time_to_first_token`
- `gen_ai.client.tool_calls_per_operation`

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
  api: {
    endpoint: "http://localhost:8080",
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
  api: {
    endpoint: "http://localhost:8080",
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

## Conversation Ratings

Use the SDK helper to submit user-facing ratings:

```ts
const result = await client.submitConversationRating("conv-123", {
  ratingId: "rat-123",
  rating: "CONVERSATION_RATING_VALUE_BAD",
  comment: "Answer ignored user context",
  metadata: { channel: "assistant-ui" },
  source: "sdk-js",
});

console.log(result.rating.rating, result.summary.hasBadRating);
```

`submitConversationRating` sends requests to `api.endpoint` (default `http://localhost:8080`) and uses the same generation-export auth headers (`tenant` or `bearer`) already configured on the SDK client.
