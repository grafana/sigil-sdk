# Grafana Sigil Python SDK

`sigil-sdk` records normalized LLM generation and tool-execution telemetry and exports it to Sigil ingest plus OTLP traces.

Use this package when you want:

- A provider-agnostic generation record (same schema for OpenAI, Anthropic, Gemini, or custom adapters).
- OTel-aligned tracing attributes for generation and tool spans.
- Async export with retry/backoff, queueing, batching, and explicit shutdown semantics.

## Installation

```bash
pip install sigil-sdk
```

Optional provider helper packages:

```bash
pip install sigil-sdk-openai
pip install sigil-sdk-anthropic
pip install sigil-sdk-gemini
```

## Quick Start (Sync Generation)

```python
from sigil_sdk import (
    Client,
    ClientConfig,
    GenerationStart,
    ModelRef,
    assistant_text_message,
    user_text_message,
)

client = Client(
    ClientConfig(
        generation_export_endpoint="http://localhost:8080/api/v1/generations:export",
        trace_endpoint="http://localhost:4318/v1/traces",
    )
)

with client.start_generation(
    GenerationStart(
        conversation_id="conv-1",
        agent_name="my-service",
        agent_version="1.0.0",
        model=ModelRef(provider="openai", name="gpt-5"),
    )
) as rec:
    rec.set_result(
        input=[user_text_message("What is the weather in Paris?")],
        output=[assistant_text_message("It is 18C and sunny.")],
    )

    # Recorder errors are local SDK errors (validation/enqueue/shutdown),
    # not provider call failures.
    if rec.err() is not None:
        raise rec.err()

client.shutdown()
```

## Streaming Generation

Use `start_streaming_generation(...)` when the upstream provider call is streaming.

```python
from sigil_sdk import GenerationStart, ModelRef

with client.start_streaming_generation(
    GenerationStart(
        conversation_id="conv-stream",
        model=ModelRef(provider="anthropic", name="claude-sonnet-4-5"),
    )
) as rec:
    rec.set_result(output=[assistant_text_message("partial stream summary")])
```

## Tool Execution Span Recording

Tool spans are recorded independently of generation export.

```python
from sigil_sdk import ToolExecutionStart

with client.start_tool_execution(
    ToolExecutionStart(
        tool_name="weather",
        tool_call_id="call_weather_1",
        tool_type="function",
        include_content=True,
    )
) as rec:
    rec.set_result(arguments={"city": "Paris"}, result={"temp_c": 18})
```

## Context Defaults

Use context helpers to set defaults once per request/task boundary.

```python
from sigil_sdk import with_agent_name, with_agent_version, with_conversation_id

with with_conversation_id("conv-ctx"), with_agent_name("planner"), with_agent_version("2026.02"):
    with client.start_generation(
        GenerationStart(model=ModelRef(provider="gemini", name="gemini-2.5-pro"))
    ) as rec:
        rec.set_result(output=[assistant_text_message("ok")])
```

## Export Configuration

### HTTP generation + OTLP HTTP trace

```python
from sigil_sdk import ApiConfig, AuthConfig, ClientConfig, GenerationExportConfig, TraceConfig

cfg = ClientConfig(
    generation_export=GenerationExportConfig(
        protocol="http",
        endpoint="http://localhost:8080/api/v1/generations:export",
        auth=AuthConfig(mode="tenant", tenant_id="dev-tenant"),
    ),
    trace=TraceConfig(
        protocol="http",
        endpoint="http://localhost:4318/v1/traces",
    ),
    api=ApiConfig(endpoint="http://localhost:8080"),
)
```

### gRPC generation + OTLP gRPC trace

```python
cfg = ClientConfig(
    generation_export=GenerationExportConfig(
        protocol="grpc",
        endpoint="localhost:50051",
        insecure=True,
        auth=AuthConfig(mode="tenant", tenant_id="dev-tenant"),
    ),
    trace=TraceConfig(
        protocol="grpc",
        endpoint="localhost:4317",
        insecure=True,
    ),
    api=ApiConfig(endpoint="http://localhost:8080"),
)
```

## Per-export auth modes

Auth is resolved independently per export (`trace` and `generation_export`).

- `mode="none"`
- `mode="tenant"` (requires `tenant_id`, injects `X-Scope-OrgID`)
- `mode="bearer"` (requires `bearer_token`, injects `Authorization: Bearer <token>`)

Invalid mode/field combinations fail fast in `resolve_config(...)`.

If explicit `headers` already include `Authorization` or `X-Scope-OrgID`, explicit headers win.

```python
from sigil_sdk import ApiConfig, AuthConfig, ClientConfig, GenerationExportConfig, TraceConfig

cfg = ClientConfig(
    generation_export=GenerationExportConfig(
        protocol="http",
        endpoint="http://localhost:8080/api/v1/generations:export",
        auth=AuthConfig(mode="tenant", tenant_id="prod-tenant"),
    ),
    trace=TraceConfig(
        protocol="grpc",
        endpoint="localhost:4317",
        auth=AuthConfig(mode="none"),
    ),
    api=ApiConfig(endpoint="http://localhost:8080"),
)
```

## Env-secret wiring example

The SDK does not auto-load env vars. Resolve env values in your application and pass them into config explicitly.

```python
import os
from sigil_sdk import AuthConfig, ClientConfig

cfg = ClientConfig()

gen_token = (os.getenv("SIGIL_GEN_BEARER_TOKEN") or "").strip()
if gen_token:
    cfg.generation_export.auth = AuthConfig(mode="bearer", bearer_token=gen_token)

trace_token = (os.getenv("SIGIL_TRACE_BEARER_TOKEN") or "").strip()
if trace_token:
    cfg.trace.auth = AuthConfig(mode="bearer", bearer_token=trace_token)
```

Common topology:

- Generations direct to Sigil: generation `tenant` mode.
- Traces via OTEL Collector/Alloy: trace `none` or `bearer` mode.
- Enterprise proxy: generation `bearer` mode to proxy; proxy authenticates and forwards tenant header upstream.

## Conversation Ratings

Use the SDK helper to submit user-facing ratings:

```python
from sigil_sdk import ConversationRatingInput, ConversationRatingValue

result = client.submit_conversation_rating(
    "conv-123",
    ConversationRatingInput(
        rating_id="rat-123",
        rating=ConversationRatingValue.BAD,
        comment="Answer ignored user context",
        metadata={"channel": "assistant-ui"},
        source="sdk-python",
    ),
)

print(result.rating.rating, result.summary.has_bad_rating)
```

`submit_conversation_rating(...)` sends requests to `ClientConfig.api.endpoint` (default `http://localhost:8080`) and uses the same generation-export auth headers (`tenant` or `bearer`) already configured on the SDK client.

## Instrumentation-only mode (no generation send)

Set `generation_export.protocol="none"` to keep generation/tool instrumentation and spans while disabling generation transport.

```python
from sigil_sdk import Client, ClientConfig, GenerationExportConfig, TraceConfig

cfg = ClientConfig(
    generation_export=GenerationExportConfig(
        protocol="none",
    ),
    trace=TraceConfig(
        protocol="http",
        endpoint="http://localhost:4318/v1/traces",
    ),
)

client = Client(cfg)
```

## Lifecycle and Error Semantics

- `flush()` forces immediate export of queued generations.
- `shutdown()` flushes pending generations, then closes generation and trace exporters.
- Always call `shutdown()` during process teardown to avoid dropped telemetry.
- `recorder.set_call_error(exc)` marks provider-call failures on the generation payload and span status.
- `recorder.err()` is for local SDK runtime errors only (validation, queue full, payload too large, shutdown).

## SDK metrics

The SDK emits these OTel histograms automatically on the trace OTLP endpoint:

- `gen_ai.client.operation.duration`
- `gen_ai.client.token.usage`
- `gen_ai.client.time_to_first_token`
- `gen_ai.client.tool_calls_per_operation`

## Public API Overview

Core client and lifecycle:

- `Client`
- `Client.start_generation(...)`
- `Client.start_streaming_generation(...)`
- `Client.start_tool_execution(...)`
- `Client.flush()`
- `Client.shutdown()`

Typed payloads:

- `GenerationStart`, `Generation`, `ModelRef`
- `Message`, `Part`, `ToolDefinition`, `TokenUsage`
- `ToolExecutionStart`, `ToolExecutionEnd`

Helpers:

- `user_text_message(...)`, `assistant_text_message(...)`
- `with_conversation_id(...)`, `with_agent_name(...)`, `with_agent_version(...)`

Validation:

- `validate_generation(...)`

## Provider Helper Packages

Provider wrappers are wrapper-first and mapper-explicit:

- `sigil-sdk-openai`
- `sigil-sdk-anthropic`
- `sigil-sdk-gemini`

Each package exposes sync + async wrappers and explicit mapper functions for custom integration points.

## Regenerating gRPC Stubs

Install dev dependencies once:

```bash
python3 -m pip install -e 'sdks/python[dev]'
```

Then regenerate:

```bash
./sdks/python/scripts/generate_proto.sh
```

This regenerates `sigil_sdk/internal/gen/sigil/v1/*_pb2*.py` from `sigil/proto/sigil/v1/generation_ingest.proto`.
