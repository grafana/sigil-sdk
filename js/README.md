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
- Generation export supports HTTP and gRPC.
- Trace export supports OTLP HTTP and OTLP gRPC.
- Exports are asynchronous with bounded queueing and retry/backoff.
- `flush()` drains queued generations; `shutdown()` flushes and closes exporters.
- Empty tool names produce a no-op tool recorder.
- Raw provider artifacts are opt-in (`rawArtifacts: true`).
