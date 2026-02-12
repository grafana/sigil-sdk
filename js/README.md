# Grafana Sigil TypeScript/JavaScript SDK

If you already use OpenTelemetry, Sigil is a thin extension plus sugar for AI observability.

This package is authored in TypeScript and serves both TypeScript and JavaScript users.

## Core API direction (explicit, primary)

Core SDK docs are explicit API first:

- `startGeneration(...)`
- `startStreamingGeneration(...)`
- `startToolExecution(...)`
- recorder methods: `setResult(...)`, `setCallError(...)`, `end()`
- lifecycle: `flush()`, `shutdown()`

### Primary usage style: active-span callback

```ts
const client = new SigilClient(config);

await client.startGeneration(
  {
    conversationId: "conv-1",
    model: { provider: "openai", name: "gpt-5" },
  },
  async (rec) => {
    const resp = await openai.responses.create(req);
    rec.setResult(sigilOpenAI.fromResponse(req, resp));
  }
);

await client.shutdown();
```

### Alternative explicit style: `try/finally`

```ts
const rec = client.startGeneration({
  conversationId: "conv-1",
  model: { provider: "openai", name: "gpt-5" },
});

try {
  const resp = await openai.responses.create(req);
  rec.setResult(sigilOpenAI.fromResponse(req, resp));
} catch (err) {
  rec.setCallError(err as Error);
  throw err;
} finally {
  rec.end();
}
```

## Provider docs direction (wrapper-first)

Provider package docs are wrapper-first and include explicit flow as secondary guidance.

Parity target:

- OpenAI
- Anthropic
- Gemini

## Runtime behavior contract

- generation mode is explicit (`SYNC` / `STREAM`)
- exports are async with bounded queue + retry/backoff
- `shutdown()` flushes pending generation batches and trace state
- local recorder errors are separated from background export retries

## Raw artifact policy

- default OFF
- explicit debug opt-in only
