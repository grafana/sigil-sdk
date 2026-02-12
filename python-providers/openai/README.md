# Sigil Python Provider Helper: OpenAI

`sigil-sdk-openai` provides wrapper-first helpers and explicit mappers for OpenAI request/response flows.

It is designed to keep your application code simple while producing normalized Sigil generations.

## Installation

```bash
pip install sigil-sdk sigil-sdk-openai
```

## Quick Start (Sync)

```python
from sigil_sdk import Client, ClientConfig
from sigil_sdk_openai import (
    OpenAIChatRequest,
    OpenAIChatResponse,
    OpenAIMessage,
    OpenAIOptions,
    chat_completion,
)

client = Client(ClientConfig())

request = OpenAIChatRequest(
    model="gpt-5",
    system_prompt="Be concise",
    messages=[OpenAIMessage(role="user", content="Hello")],
)

def provider_call(req: OpenAIChatRequest) -> OpenAIChatResponse:
    # Replace this with your OpenAI SDK call.
    return OpenAIChatResponse(id="resp-1", model=req.model, output_text="Hi there")

response = chat_completion(
    client,
    request,
    provider_call,
    OpenAIOptions(conversation_id="conv-1", agent_name="my-agent", agent_version="1.0.0"),
)
```

## Streaming Wrapper

```python
from sigil_sdk_openai import OpenAIStreamSummary, chat_completion_stream

summary = chat_completion_stream(
    client,
    request,
    lambda req: OpenAIStreamSummary(
        output_text="streamed final text",
        chunks=[{"delta": "stream"}],
    ),
)
```

Call `client.shutdown()` during process teardown to flush buffered telemetry.

## Mapper-Only Usage

If you manage lifecycle yourself, map directly and call `recorder.set_result(...)`.

```python
from sigil_sdk_openai import from_request_response

generation = from_request_response(request, response)
```

## Raw Provider Artifacts (Opt-In)

Raw artifacts are off by default.

```python
from sigil_sdk_openai import OpenAIOptions

options = OpenAIOptions(raw_artifacts=True)
```

When enabled, wrappers include request/response payload artifacts (and stream chunk artifacts for streaming paths).

## Public Functions

- `chat_completion(...)`
- `chat_completion_async(...)`
- `chat_completion_stream(...)`
- `chat_completion_stream_async(...)`
- `from_request_response(...)`
- `from_stream(...)`
