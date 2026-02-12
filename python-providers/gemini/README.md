# Sigil Python Provider Helper: Gemini

`sigil-sdk-gemini` provides wrapper-first helpers and explicit mappers for Gemini request/response flows.

Use it to convert Gemini-style payloads into normalized Sigil generations with predictable defaults.

## Installation

```bash
pip install sigil-sdk sigil-sdk-gemini
```

## Quick Start (Sync)

```python
from sigil_sdk import Client, ClientConfig
from sigil_sdk_gemini import (
    GeminiMessage,
    GeminiOptions,
    GeminiRequest,
    GeminiResponse,
    completion,
)

client = Client(ClientConfig())

request = GeminiRequest(
    model="gemini-2.5-pro",
    messages=[GeminiMessage(role="user", content="Hello")],
)

def provider_call(req: GeminiRequest) -> GeminiResponse:
    # Replace this with your Gemini SDK call.
    return GeminiResponse(id="resp-1", model=req.model, output_text="Hi there")

response = completion(
    client,
    request,
    provider_call,
    GeminiOptions(conversation_id="conv-1", agent_name="my-agent", agent_version="1.0.0"),
)
```

## Streaming Wrapper

```python
from sigil_sdk_gemini import GeminiStreamSummary, completion_stream

summary = completion_stream(
    client,
    request,
    lambda req: GeminiStreamSummary(
        output_text="streamed final text",
        events=[{"delta": "stream"}],
    ),
)
```

Call `client.shutdown()` during process teardown to flush buffered telemetry.

## Mapper-Only Usage

```python
from sigil_sdk_gemini import from_request_response

generation = from_request_response(request, response)
```

## Raw Provider Artifacts (Opt-In)

Raw artifacts are off by default.

```python
from sigil_sdk_gemini import GeminiOptions

options = GeminiOptions(raw_artifacts=True)
```

When enabled, wrappers include request/response payload artifacts (and stream event artifacts for streaming paths).

## Public Functions

- `completion(...)`
- `completion_async(...)`
- `completion_stream(...)`
- `completion_stream_async(...)`
- `from_request_response(...)`
- `from_stream(...)`
