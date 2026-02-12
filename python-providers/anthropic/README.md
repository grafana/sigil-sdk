# Sigil Python Provider Helper: Anthropic

`sigil-sdk-anthropic` provides wrapper-first helpers and explicit mappers for Anthropic request/response flows.

Use it to convert Anthropic-style payloads into normalized Sigil generations with minimal boilerplate.

## Installation

```bash
pip install sigil-sdk sigil-sdk-anthropic
```

## Quick Start (Sync)

```python
from sigil_sdk import Client, ClientConfig
from sigil_sdk_anthropic import (
    AnthropicMessage,
    AnthropicOptions,
    AnthropicRequest,
    AnthropicResponse,
    completion,
)

client = Client(ClientConfig())

request = AnthropicRequest(
    model="claude-sonnet-4-5",
    messages=[AnthropicMessage(role="user", content="Hello")],
)

def provider_call(req: AnthropicRequest) -> AnthropicResponse:
    # Replace this with your Anthropic SDK call.
    return AnthropicResponse(id="resp-1", model=req.model, output_text="Hi there")

response = completion(
    client,
    request,
    provider_call,
    AnthropicOptions(conversation_id="conv-1", agent_name="my-agent", agent_version="1.0.0"),
)
```

## Streaming Wrapper

```python
from sigil_sdk_anthropic import AnthropicStreamSummary, completion_stream

summary = completion_stream(
    client,
    request,
    lambda req: AnthropicStreamSummary(
        output_text="streamed final text",
        events=[{"delta": "stream"}],
    ),
)
```

Call `client.shutdown()` during process teardown to flush buffered telemetry.

## Mapper-Only Usage

```python
from sigil_sdk_anthropic import from_request_response

generation = from_request_response(request, response)
```

## Raw Provider Artifacts (Opt-In)

Raw artifacts are off by default.

```python
from sigil_sdk_anthropic import AnthropicOptions

options = AnthropicOptions(raw_artifacts=True)
```

When enabled, wrappers include request/response payload artifacts (and stream event artifacts for streaming paths).

## Public Functions

- `completion(...)`
- `completion_async(...)`
- `completion_stream(...)`
- `completion_stream_async(...)`
- `from_request_response(...)`
- `from_stream(...)`
