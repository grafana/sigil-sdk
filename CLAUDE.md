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

Traces and metrics can be sent to Grafana Cloud in two ways. Always use env vars (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`) so the app doesn't hardcode assumptions.

### Option A — Direct to Grafana Cloud (no collector needed)

Send OTLP straight to the Grafana Cloud OTLP gateway. The exact URL is stack-specific — get it from the **Grafana Cloud portal → stack Details page** ([docs](https://grafana.com/docs/grafana-cloud/send-data/otlp/send-data-otlp)). Authentication uses Basic auth with instance ID and a Cloud API token.

Env vars:
```
OTEL_EXPORTER_OTLP_ENDPOINT=https://<your-otlp-gateway-url>   # from Cloud portal
OTEL_EXPORTER_OTLP_HEADERS=Authorization=Basic <base64(instance_id:cloud_api_token)>
```

The OTel SDK exporters read these env vars automatically — no extra code needed beyond the provider setup below.

### Option B — Via Alloy / OTel Collector (optional)

Run a local Alloy or OTel Collector that receives unauthenticated OTLP and forwards to Cloud with credentials. Useful for centralized token management, retries, relabeling, and metadata enrichment. Common local ports: 4318 (OTLP/HTTP), 4317 (OTLP/gRPC).

Env vars:
```
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
```

### Provider setup (required for both options)

The snippets below configure TracerProvider and MeterProvider using OTLP/HTTP exporters that honour the env vars above.

#### Python
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

tp = TracerProvider(resource=resource)
tp.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
trace.set_tracer_provider(tp)

mp = MeterProvider(resource=resource, metric_readers=[
    PeriodicExportingMetricReader(OTLPMetricExporter())
])
metrics.set_meter_provider(mp)
# Deps: opentelemetry-sdk, opentelemetry-exporter-otlp-proto-http
```

#### Go
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

#### JS/TS
```typescript
import { metrics } from '@opentelemetry/api';
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
metrics.setGlobalMeterProvider(mp);
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
  - `gen_ai.usage.cache_write_input_tokens`
  - `gen_ai.usage.reasoning_tokens`
  - `gen_ai.response.finish_reasons`
  - error classification fields (`error.type`, `error.category`)

## Workflow step instrumentation (agentic pipelines)

Sigil has two separate ingest pipelines: **generations** (LLM calls) and **workflow steps** (agentic execution nodes).

### When to use workflow steps

Use workflow steps when instrumenting agentic frameworks (LangGraph, CrewAI, Google ADK, custom orchestrators) where:
- The pipeline has non-LLM nodes (routing, formatting, validation, tool calls)
- You want to see the full execution graph, not just LLM calls
- You need input/output state captured per node

### Architecture

- Workflow steps are a **separate entity** from generations, with their own proto message, ingest endpoint, and storage.
- Generations track LLM calls (model, tokens, cost). Workflow steps track execution nodes (input state, output state, duration).
- The two are linked via `linked_generation_ids` on the workflow step (which LLM calls ran inside this node) and `parent_step_ids` (DAG edges between workflow nodes).

### Ingest endpoints

| Entity | gRPC | HTTP |
|--------|------|------|
| Generations | `GenerationIngestService.ExportGenerations` | `POST /api/v1/generations:export` |
| Workflow steps | `WorkflowStepIngestService.ExportWorkflowSteps` | `POST /api/v1/workflow-steps:export` |

### WorkflowStep fields

```
id                     — unique step ID (e.g. "wfs_<hex>")
conversation_id        — links step to a conversation
step_name              — node name (e.g. "classify", "analyze")
framework              — framework name (e.g. "langgraph")
started_at / completed_at — execution timestamps
input_state            — node input (Struct/dict)
output_state           — node output (Struct/dict)
error                  — error message if the node failed
tags                   — key-value filtering tags
metadata               — arbitrary metadata (Struct/dict)
linked_generation_ids  — generation IDs of LLM calls inside this step
parent_step_ids        — IDs of upstream workflow steps (DAG edges)
agent_name / agent_version — agent identity
trace_id / span_id     — OTel correlation
```

### Choosing a workflow step instrumentation path

There are two layers:

- **Manual SDK API** — use `enqueue_workflow_step(step)` when forwarding pre-built workflow steps or instrumenting custom execution graphs directly. You create step IDs, parent edges, timestamps, input/output state, and linked generation IDs yourself.
- **Framework adapter** — use the adapter when one exists, such as `SigilLangGraphHandler` for LangGraph. The adapter creates workflow steps automatically from framework callbacks.

Do not use both paths for the same LangGraph node, or you will record duplicate workflow steps.

### Manual SDK workflow step API

The SDK client exposes `enqueue_workflow_step(step)` which queues the step for batch export alongside generations. The flush pipeline handles both.

#### Python
```python
from sigil_sdk import Client, WorkflowStep
from datetime import datetime, timezone

step = WorkflowStep(
    id="wfs_abc123",
    conversation_id="conv-1",
    step_name="classify",
    framework="custom-orchestrator",
    started_at=datetime.now(timezone.utc),
    completed_at=datetime.now(timezone.utc),
    input_state={"text": "input data"},
    output_state={"category": "incident"},
    tags={"sigil.framework.name": "custom-orchestrator"},
    linked_generation_ids=["gen_xyz"],  # LLM calls inside this step
    parent_step_ids=[],                  # first node, no parents
)
client.enqueue_workflow_step(step)
```

### LangGraph adapter pattern

For LangGraph, prefer the adapter instead of manually calling `enqueue_workflow_step`.
The `SigilLangGraphHandler` automatically captures workflow steps when `capture_workflow_steps=True`.
Always set `conversation_title` to a short human-readable label — it appears as the conversation name in the Sigil UI. Without it, the title falls back to an opaque auto-generated ID.

```python
from sigil_sdk_langgraph import SigilLangGraphHandler

handler = SigilLangGraphHandler(
    client=client,
    agent_name="my-pipeline",
    conversation_title="Incident Response Pipeline",
    capture_workflow_steps=True,
)
result = graph.invoke(
    {"prompt": "Investigate elevated API latency.", "answer": ""},
    config={"callbacks": [handler]},
)
```

### Complete LangGraph instrumentation example

End-to-end example showing OTel setup, Sigil client, handler, and correct shutdown order.
The `OTEL_EXPORTER_OTLP_ENDPOINT` env var is read automatically by the OTel exporters.

Environment variables — export these values or load them with your app's `.env` tooling.
Find the Sigil values at `https://<your-grafana>.grafana.net/plugins/grafana-sigil-app` and the OTLP endpoint in the Grafana Cloud OpenTelemetry card:
```dotenv
SIGIL_ENDPOINT=https://sigil-prod-<region>.grafana.net
SIGIL_AUTH_TENANT_ID=<instance-id>
SIGIL_AUTH_TOKEN=glc_...
OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp-gateway-prod-<region>.grafana.net/otlp
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Basic <base64(otlp-instance-id:cloud_api_token)>"
OPENAI_API_KEY=sk-...
```

```python
import os
from typing import TypedDict

from langchain_core.runnables import RunnableConfig
from langchain_openai import ChatOpenAI
from langgraph.graph import END, StateGraph
from opentelemetry import metrics, trace
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

import sigil_sdk
from sigil_sdk.config import AuthConfig, GenerationExportConfig
from sigil_sdk_langgraph import SigilLangGraphHandler


class GraphState(TypedDict):
    prompt: str
    classification: str
    answer: str


# 1. OTel providers MUST be configured BEFORE creating the Sigil client.
resource = Resource.create({"service.name": "my-langgraph-app"})
tp = TracerProvider(resource=resource)
tp.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
trace.set_tracer_provider(tp)
mp = MeterProvider(resource=resource, metric_readers=[
    PeriodicExportingMetricReader(OTLPMetricExporter())
])
metrics.set_meter_provider(mp)

# 2. Sigil client — endpoint is the Sigil API base URL (SDK appends the path).
client = sigil_sdk.Client(
    sigil_sdk.ClientConfig(
        generation_export=GenerationExportConfig(
            protocol="http",
            endpoint=os.environ["SIGIL_ENDPOINT"],
            auth=AuthConfig(
                mode="basic",
                tenant_id=os.environ["SIGIL_AUTH_TENANT_ID"],
                basic_password=os.environ["SIGIL_AUTH_TOKEN"],
            ),
        ),
    )
)

# 3. LangGraph handler with workflow step capture.
handler = SigilLangGraphHandler(
    client=client,
    provider_resolver="auto",
    agent_name="support-triage",
    agent_version="1.0.0",
    conversation_title="Support Triage Workflow",
    capture_workflow_steps=True,
)

# 4. Build a small graph. Passing config into llm.invoke keeps LLM generations
# linked to the enclosing workflow step.
llm = ChatOpenAI(model="gpt-4o-mini", temperature=0)


def classify(state: GraphState, config: RunnableConfig) -> dict[str, str]:
    response = llm.invoke(
        "Classify this support request as billing, reliability, or usage: "
        f"{state['prompt']}",
        config=config,
    )
    return {"classification": str(response.content).strip()}


def draft_answer(state: GraphState, config: RunnableConfig) -> dict[str, str]:
    response = llm.invoke(
        "Write a concise answer for this support request.\n"
        f"Category: {state['classification']}\n"
        f"Request: {state['prompt']}",
        config=config,
    )
    return {"answer": str(response.content).strip()}


workflow = StateGraph(GraphState)
workflow.add_node("classify", classify)
workflow.add_node("draft_answer", draft_answer)
workflow.set_entry_point("classify")
workflow.add_edge("classify", "draft_answer")
workflow.add_edge("draft_answer", END)
graph = workflow.compile()

try:
    # 5. Run the graph with the handler as a callback.
    result = graph.invoke(
        {
            "prompt": "My dashboard is slow after adding several high-cardinality labels.",
            "classification": "",
            "answer": "",
        },
        config={"callbacks": [handler]},
    )
    print(result["answer"])
finally:
    # 6. Shutdown order: Sigil first, then OTel providers.
    client.shutdown()
    tp.shutdown()
    mp.shutdown()
```

### Workflow step vs generation: decision table

| Signal | Use Generation | Use WorkflowStep |
|--------|---------------|-------------------|
| LLM API call (tokens, model, cost) | Yes | No |
| Non-LLM node (routing, formatting) | No | Yes |
| Tool execution | Yes (start_tool_execution) | Optional (as a step) |
| Node input/output state | No | Yes |
| DAG edges between nodes | parent_generation_ids | parent_step_ids |

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

## Regenerating protobuf stubs

After editing `proto/sigil/v1/generation_ingest.proto`, run `mise run generate:proto`. See [`docs/development.md`](docs/development.md) for details.

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
  - Full token usage including `cache_read_input_tokens`, `cache_write_input_tokens`, and `reasoning_tokens` when the provider returns them
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
