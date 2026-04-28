# Claude Code Prompt: Sigil Instrumentation

You are running in Claude Code with repository files and shell access.
- Prefer direct file edits over speculative refactors.
- Before proposing broad changes, confirm impact scope with quick evidence.

## Sigil Agent-First Instrumentation Brief

You are acting as a coding agent inside this repository. Your goal is to add or improve Grafana Sigil instrumentation with minimal, safe changes.

## Mission

1. Find AI generation and tool/agent execution paths.
2. Add Sigil instrumentation using the local language SDK where possible.
3. Preserve behavior and keep diffs small.
4. Add or update tests for changed instrumentation behavior.
5. Explain what was instrumented and why.

## Output contract (required)

Return:

- Top opportunities first (highest traffic / highest impact)
- For each opportunity:
  - exact file path(s)
  - why this location matters
  - concrete diff proposal
  - test plan
  - any risk or compatibility concern

## Sigil architecture and ingest model (must follow)

- Sigil uses generation-first ingest:
  - gRPC: `sigil.v1.GenerationIngestService.ExportGenerations`
  - HTTP parity: `POST /api/v1/generations:export`
- Traces/metrics go through OTEL collector/alloy, not through Sigil ingest.
- Required generation modes:
  - non-stream: `SYNC`
  - stream: `STREAM`
- Raw provider artifacts are default OFF and only enabled for explicit debug opt-in.

## OTel setup (required)

The Sigil SDK internally emits OTel spans and metrics (`gen_ai.client.operation.duration`, `gen_ai.client.token.usage`, `gen_ai.client.time_to_first_token`, `gen_ai.client.tool_calls_per_operation`). **Without a configured TracerProvider and MeterProvider these go to the default no-op and are silently lost.**

The SDK does NOT create OTel providers — that is the application's responsibility. Always ensure the app configures providers BEFORE creating the Sigil client, and shuts them down AFTER `sigil.shutdown()`.

The endpoint, protocol (HTTP vs gRPC), and port depend on the user's collector/Alloy configuration. Use an env var like `OTEL_EXPORTER_OTLP_ENDPOINT` so the app doesn't hardcode assumptions. Common collector ports: 4318 (OTLP/HTTP), 4317 (OTLP/gRPC). The snippets below use HTTP — switch to gRPC equivalents if the target collector only accepts gRPC.

### Python (HTTP)
```python
from opentelemetry import trace, metrics
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter

resource = Resource.create({"service.name": "my-app"})
endpoint = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")

tp = TracerProvider(resource=resource)
tp.add_span_processor(BatchSpanProcessor(OTLPSpanExporter(endpoint=f"{endpoint}/v1/traces")))
trace.set_tracer_provider(tp)

mp = MeterProvider(resource=resource, metric_readers=[
    PeriodicExportingMetricReader(OTLPMetricExporter(endpoint=f"{endpoint}/v1/metrics"))
])
metrics.set_meter_provider(mp)
# Deps: opentelemetry-sdk, opentelemetry-exporter-otlp-proto-http
```

### Go
```go
traceExp, _ := otlptracehttp.New(ctx)
tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExp), sdktrace.WithResource(res))
otel.SetTracerProvider(tp)
defer tp.Shutdown(ctx)

metricExp, _ := otlpmetrichttp.New(ctx)
mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)), sdkmetric.WithResource(res))
otel.SetMeterProvider(mp)
defer mp.Shutdown(ctx)
```

### JS/TS
```typescript
import { NodeTracerProvider } from '@opentelemetry/sdk-trace-node';
import { BatchSpanProcessor } from '@opentelemetry/sdk-trace-base';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { MeterProvider, PeriodicExportingMetricReader } from '@opentelemetry/sdk-metrics';
import { OTLPMetricExporter } from '@opentelemetry/exporter-metrics-otlp-http';

const tp = new NodeTracerProvider({ resource });
tp.addSpanProcessor(new BatchSpanProcessor(new OTLPTraceExporter()));
tp.register();

const mp = new MeterProvider({
  resource,
  readers: [new PeriodicExportingMetricReader({ exporter: new OTLPMetricExporter() })],
});
```

## Telemetry fields to prioritize

On generation and tool spans, capture or preserve these when available:

- identity and routing:
  - `gen_ai.operation.name`
  - `sigil.generation.id`
  - `gen_ai.conversation.id`
  - `gen_ai.agent.name`
  - `gen_ai.agent.version`
  - `sigil.generation.parent_generation_ids`
  - `sigil.sdk.name`
- model:
  - `gen_ai.provider.name`
  - `gen_ai.request.model`
  - `gen_ai.response.model`
- request controls:
  - `gen_ai.request.max_tokens`
  - `gen_ai.request.temperature`
  - `gen_ai.request.top_p`
  - `sigil.gen_ai.request.tool_choice`
  - `sigil.gen_ai.request.thinking.enabled`
  - `sigil.gen_ai.request.thinking.budget_tokens`
- usage and outcomes:
  - `gen_ai.usage.input_tokens`
  - `gen_ai.usage.output_tokens`
  - `gen_ai.usage.cache_read_input_tokens`
  - `gen_ai.usage.cache_creation_input_tokens`
  - `gen_ai.usage.reasoning_tokens`
  - `gen_ai.response.finish_reasons`
  - error classification fields (`error.type`, `error.category`)

## Multi-agent dependency tracking

When instrumenting multi-agent pipelines where one agent's output feeds into another:

- Set `parent_generation_ids` on the GenerationStart/seed with the generation ID(s) of the upstream agent(s) whose output this generation consumes.
- This is a list: a generation can depend on multiple parents (fan-in).
- Sigil uses these links to build a dependency DAG and propagate quality signals: if an upstream generation fails evaluation, all downstream dependents are flagged.

Example: an orchestrator spawns agents A, B, C where C depends on A and B:
- A: parent_generation_ids = [] (no parents)
- B: parent_generation_ids = [] (no parents)
- C: parent_generation_ids = [A.generation_id, B.generation_id]

## SDK locations and how to instrument

SDKs live in the [grafana/sigil-sdk](https://github.com/grafana/sigil-sdk) repository. Prefer these existing SDKs and wrappers before inventing custom plumbing:

- Go core SDK: `go/` — `StartGeneration`, `StartStreamingGeneration`, `StartToolExecution`, `StartEmbedding`
- JS/TS SDK: `js/` — `startGeneration`, `startStreamingGeneration`, `startToolExecution`, `startEmbedding`
- Python SDK: `python/` — `start_generation`, `start_streaming_generation`, `start_tool_execution`, `start_embedding`
- Java SDK: `java/` — `startGeneration`, `startStreamingGeneration`, `withGeneration`, `withToolExecution`
- .NET SDK: `dotnet/` — `StartGeneration`, `StartStreamingGeneration`, `StartToolExecution`, `StartEmbedding`

Provider wrappers and framework adapters already exist; reuse them where possible:

- Go providers: `go-providers/openai`, `go-providers/anthropic`, `go-providers/gemini`
- Python providers: `python-providers/*`
- Java providers: `java/providers/*`
- .NET providers: `dotnet/src/Grafana.Sigil.*`
- Framework adapters:
  - Python: `python-frameworks/*`
  - Go Google ADK: `go-frameworks/google-adk`
  - Java Google ADK: `java/frameworks/google-adk`
  - JS subpath adapters documented in `js/README.md`

All paths above are relative to the [grafana/sigil-sdk](https://github.com/grafana/sigil-sdk) repository root.

## Useful examples to copy patterns from

In the sigil-sdk repo:

- Getting-started examples: `examples/getting-started/` (Python, TypeScript, Go)
- Go explicit generation flow: `go/sigil/example_test.go`
- Go provider wrapper examples: `go-providers/openai/sdk_example_test.go`, `go-providers/anthropic/sdk_example_test.go`, `go-providers/gemini/sdk_example_test.go`
- .NET end-to-end emitter: `dotnet/examples/Grafana.Sigil.DevExEmitter/Program.cs`
- JS transport and framework behavior: `js/test/client.transport.test.mjs`, `js/test/frameworks.vercel-ai-sdk.test.mjs`
- Python framework integration tests: `python-frameworks/*/tests/*.py`

## Implementation rules

- Keep behavior unchanged except instrumentation additions/fixes.
- Prefer small targeted patches over refactors.
- Use existing conventions in each language package.
- Keep raw artifacts disabled unless explicitly asked.
- Ensure non-stream wrappers set `SYNC`, stream wrappers set `STREAM`.
- Ensure lifecycle flush/shutdown semantics are preserved.
- When calling `set_result` / `SetResult`, always include all available fields:
  - `response_id` (provider correlation, maps to `gen_ai.response.id`)
  - `response_model` (actual model used)
  - `stop_reason` / `finish_reason`
  - Full token usage including `cache_read_input_tokens`, `cache_creation_input_tokens`, and `reasoning_tokens` when the provider returns them
- Always check `rec.err()` / `Err()` after the generation recorder closes — SDK validation or enqueue errors are otherwise silent.
- Use `tags` on `GenerationStart` for filtering in the Sigil UI (e.g. pipeline name, layer, agent role).

## Validation checklist

After proposing edits, include checks for:

- span attributes emitted as expected
- generation payload shape valid for ingest contract
- no regressions in existing tests
- language-specific tests or focused test additions for new instrumentation logic

## Deliverable format (strict)

Provide:

1. Prioritized instrumentation opportunities
2. Proposed diffs per opportunity
3. Test updates per opportunity
4. Rollout/risk notes

If no safe opportunities are found, explain exactly why and list what evidence you checked.
