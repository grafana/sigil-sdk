# Grafana Sigil Python SDK

`sigil-sdk` records normalized LLM generation and tool-execution telemetry. It exports normalized generations to Sigil ingest and uses your OpenTelemetry tracer/meter setup for traces and metrics.

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

Configure OTEL exporters (traces/metrics) in your application OTEL SDK setup. You can optionally pass `tracer` and `meter` via `ClientConfig`.

Quick OTEL setup pattern before creating the Sigil client:

```python
from opentelemetry import metrics, trace
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.trace import TracerProvider

trace.set_tracer_provider(TracerProvider())
metrics.set_meter_provider(MeterProvider())
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

## Embedding Observability

Use `start_embedding(...)` for embedding API calls. Embedding recording emits OTel spans and SDK metrics only, and does not enqueue generation exports.

```python
from sigil_sdk import EmbeddingResult, EmbeddingStart, ModelRef

with client.start_embedding(
    EmbeddingStart(
        agent_name="retrieval-worker",
        agent_version="1.0.0",
        model=ModelRef(provider="openai", name="text-embedding-3-small"),
    )
) as rec:
    response = openai.embeddings.create(model="text-embedding-3-small", input=["hello", "world"])
    rec.set_result(
        EmbeddingResult(
            input_count=2,
            input_tokens=response.usage.prompt_tokens,
            input_texts=["hello", "world"],  # captured only when embedding_capture.capture_input=True
            response_model=response.model,
        )
    )
```

Input text capture is opt-in:

```python
from sigil_sdk import ClientConfig, EmbeddingCaptureConfig

cfg = ClientConfig(
    embedding_capture=EmbeddingCaptureConfig(
        capture_input=True,
        max_input_items=20,
        max_text_length=1024,
    )
)
```

`capture_input` may expose PII/document content in spans. Keep it disabled by default and enable only for scoped debugging.

TraceQL examples:

- `traces{gen_ai.operation.name="embeddings"}`
- `traces{gen_ai.operation.name="embeddings" && gen_ai.request.model="text-embedding-3-small"}`
- `traces{gen_ai.operation.name="embeddings" && error.type!=""}`

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

## SDK identity attributes

- Generation and tool spans always include:
  - `sigil.sdk.name=sdk-python`
- Normalized generation metadata always includes the same key.
- If caller metadata provides a conflicting value for this key, the SDK overwrites it.

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

### HTTP generation export

```python
from sigil_sdk import ApiConfig, AuthConfig, ClientConfig, GenerationExportConfig

cfg = ClientConfig(
    generation_export=GenerationExportConfig(
        protocol="http",
        endpoint="http://localhost:8080/api/v1/generations:export",
        auth=AuthConfig(mode="tenant", tenant_id="dev-tenant"),
    ),
    api=ApiConfig(endpoint="http://localhost:8080"),
)
```

### gRPC generation export

```python
cfg = ClientConfig(
    generation_export=GenerationExportConfig(
        protocol="grpc",
        endpoint="localhost:50051",
        insecure=True,
        auth=AuthConfig(mode="tenant", tenant_id="dev-tenant"),
    ),
    api=ApiConfig(endpoint="http://localhost:8080"),
)
```

## Generation export auth modes

Auth is resolved for `generation_export`.

- `mode="none"`
- `mode="tenant"` (requires `tenant_id`, injects `X-Scope-OrgID`)
- `mode="bearer"` (requires `bearer_token`, injects `Authorization: Bearer <token>`)

Invalid mode/field combinations fail fast in `resolve_config(...)`.

If explicit `headers` already include `Authorization` or `X-Scope-OrgID`, explicit headers win.

```python
from sigil_sdk import ApiConfig, AuthConfig, ClientConfig, GenerationExportConfig

cfg = ClientConfig(
    generation_export=GenerationExportConfig(
        protocol="http",
        endpoint="http://localhost:8080/api/v1/generations:export",
        auth=AuthConfig(mode="tenant", tenant_id="prod-tenant"),
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
```

Common topology:

- Generations direct to Sigil: generation `tenant` mode.
- Traces/metrics via OTEL Collector/Alloy: configure exporters in your app OTEL SDK setup.
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
from sigil_sdk import Client, ClientConfig, GenerationExportConfig

cfg = ClientConfig(
    generation_export=GenerationExportConfig(
        protocol="none",
    ),
)

client = Client(cfg)
```

## Lifecycle and Error Semantics

- `flush()` forces immediate export of queued generations.
- `shutdown()` flushes pending generations, then closes generation exporters.
- Always call `shutdown()` during process teardown to avoid dropped telemetry.
- `recorder.set_call_error(exc)` marks provider-call failures on the generation payload and span status.
- `recorder.err()` is for local SDK runtime errors only (validation, queue full, payload too large, shutdown).

## SDK metrics

The SDK emits these OTel histograms through your configured OTEL meter provider:

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
