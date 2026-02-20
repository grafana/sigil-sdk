# Grafana Sigil TypeScript/JavaScript SDK

Sigil records normalized LLM generation and tool-execution telemetry using your OpenTelemetry tracer/meter setup.

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

Configure OTEL exporters (traces/metrics) in your application OTEL SDK setup. You can optionally pass `tracer` and `meter` directly to `SigilClient`.

Quick OTEL setup pattern before creating the Sigil client:

```ts
import { NodeSDK } from "@opentelemetry/sdk-node";

const otel = new NodeSDK();
await otel.start();
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

## Embedding Observability

Use `startEmbedding(...)` for embedding API calls. Embedding recording creates OTel spans and SDK metrics only, and does not enqueue generation exports.

```ts
await client.startEmbedding(
  {
    agentName: "retrieval-worker",
    agentVersion: "1.0.0",
    model: { provider: "openai", name: "text-embedding-3-small" },
  },
  async (recorder) => {
    const response = await openai.embeddings.create(request);
    recorder.setResult({
      inputCount: request.input.length,
      inputTokens: response.usage?.prompt_tokens ?? 0,
      inputTexts: request.input,
      responseModel: response.model,
    });
  }
);
```

Input text capture is opt-in:

```ts
const client = new SigilClient({
  embeddingCapture: {
    captureInput: true,
    maxInputItems: 20,
    maxTextLength: 1024,
  },
});
```

`embeddingCapture.captureInput` may expose PII/document content in spans. Keep it disabled by default and enable it only for scoped debugging.

TraceQL examples:

- `traces{gen_ai.operation.name="embeddings"}`
- `traces{gen_ai.operation.name="embeddings" && gen_ai.request.model="text-embedding-3-small"}`
- `traces{gen_ai.operation.name="embeddings" && error.type!=""}`

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

## Framework Handlers

Use module subpath exports for framework callback integrations:

- LangChain: `@grafana/sigil-sdk-js/langchain`
- LangGraph: `@grafana/sigil-sdk-js/langgraph`
- LangChain guide: `docs/frameworks/langchain.md`
- LangGraph guide: `docs/frameworks/langgraph.md`

```ts
import { SigilClient } from "@grafana/sigil-sdk-js";
import { SigilLangChainHandler } from "@grafana/sigil-sdk-js/langchain";
import { SigilLangGraphHandler } from "@grafana/sigil-sdk-js/langgraph";

const client = new SigilClient();
const chainHandler = new SigilLangChainHandler(client, { providerResolver: "auto" });
const graphHandler = new SigilLangGraphHandler(client, { providerResolver: "auto" });
```

Each framework handler injects:

- `sigil.framework.name` (`langchain` or `langgraph`)
- `sigil.framework.source=handler`
- `sigil.framework.language=javascript`
- `metadata["sigil.framework.run_id"]`
- `metadata["sigil.framework.thread_id"]` (when present in callback metadata/config)

When present in generation metadata, these framework IDs are also copied onto generation span attributes.

For LangGraph persistence, pass `configurable.thread_id` and reuse it across invocations:

```ts
const threadConfig = {
  callbacks: [graphHandler],
  configurable: { thread_id: 'customer-42' },
};
await graph.invoke({ prompt: 'Remember my timezone is UTC+1.', answer: '' }, threadConfig);
await graph.invoke({ prompt: 'What timezone did I give you?', answer: '' }, threadConfig);
```

## Behavior

- Generation modes are explicit: `SYNC` and `STREAM`.
- Generation export supports HTTP, gRPC, and `none` (instrumentation-only).
- Traces/metrics use `config.tracer`/`config.meter` when provided, otherwise OTEL globals.
- Exports are asynchronous with bounded queueing and retry/backoff.
- `flush()` drains queued generations; `shutdown()` flushes and closes generation exporters.
- Empty tool names produce a no-op tool recorder.
- Generation/tool spans always include SDK identity attributes:
  - `sigil.sdk.name=sdk-js`
- Normalized generation metadata always includes the same SDK identity key; conflicting caller values are overwritten.
- Raw provider artifacts are opt-in (`rawArtifacts: true`).

## Instrumentation-only mode (no generation send)

Set `generationExport.protocol` to `"none"` to keep generation/tool instrumentation and spans while disabling generation transport.

```ts
const client = new SigilClient({
  generationExport: {
    protocol: "none",
  },
});
```

## SDK metrics

The SDK emits these OTel histograms through your configured OTEL meter provider:

- `gen_ai.client.operation.duration`
- `gen_ai.client.token.usage`
- `gen_ai.client.time_to_first_token`
- `gen_ai.client.tool_calls_per_operation`

## Generation export auth modes

Auth is configured for `generationExport`.

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
});
```

## Env-secret wiring example

The SDK does not auto-load env vars. Resolve env secrets in your app and map them into config.

```ts
const generationBearerToken = (process.env.SIGIL_GEN_BEARER_TOKEN ?? "").trim();

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
});
```

Common topology:

- Generations direct to Sigil: generation `tenant` mode.
- Traces/metrics via OTEL Collector/Alloy: configure exporters in your app OTEL SDK setup.
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
