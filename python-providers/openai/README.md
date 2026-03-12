# Sigil Python Provider Helper: OpenAI

`sigil-sdk-openai` exposes strict OpenAI-shaped wrappers and mappers for both Chat Completions and Responses.

## Installation

```bash
pip install sigil-sdk sigil-sdk-openai
```

## Public API

- Chat Completions namespace:
  - `chat.completions.create(...)`
  - `chat.completions.create_async(...)`
  - `chat.completions.stream(...)`
  - `chat.completions.stream_async(...)`
  - `chat.completions.from_request_response(...)`
  - `chat.completions.from_stream(...)`

- Responses namespace:
  - `responses.create(...)`
  - `responses.create_async(...)`
  - `responses.stream(...)`
  - `responses.stream_async(...)`
  - `responses.from_request_response(...)`
  - `responses.from_stream(...)`

- Embeddings namespace:
  - `embeddings.create(...)`
  - `embeddings.create_async(...)`
  - `embeddings.from_request_response(...)`

## Integration styles

- Strict wrappers: call OpenAI and record in one step.
- Manual instrumentation: call OpenAI directly, then map strict OpenAI request/response payloads with `from_request_response` or `from_stream`.

## Responses-first wrapper example

```python
from openai import OpenAI
from sigil_sdk import Client, ClientConfig
from sigil_sdk_openai import OpenAIOptions, responses

sigil = Client(ClientConfig())
provider = OpenAI()

response = responses.create(
    sigil,
    {
        "model": "gpt-5",
        "instructions": "Be concise",
        "input": "Summarize rollout status in 3 bullets",
        "max_output_tokens": 300,
    },
    lambda request: provider.responses.create(**request),
    OpenAIOptions(conversation_id="conv-1", agent_name="assistant", agent_version="1.0.0"),
)
```

## Chat Completions stream example

```python
from sigil_sdk_openai import ChatCompletionsStreamSummary, chat

summary = chat.completions.stream(
    sigil,
    {
        "model": "gpt-5",
        "stream": True,
        "messages": [{"role": "user", "content": "Stream a short status update"}],
    },
    lambda request: ChatCompletionsStreamSummary(events=[]),
)
```

## Embeddings example

```python
from sigil_sdk_openai import embeddings

embedding_response = embeddings.create(
    sigil,
    {
        "model": "text-embedding-3-small",
        "input": ["hello", "world"],
    },
    lambda request: provider.embeddings.create(**request),
)
```

## Manual instrumentation example (strict mapper)

```python
from sigil_sdk import GenerationStart, ModelRef
from sigil_sdk_openai import OpenAIOptions, responses

request = {
    "model": "gpt-5",
    "instructions": "Be concise",
    "input": "Summarize rollout status in 3 bullets",
}
opts = OpenAIOptions(
    conversation_id="conv-1",
    agent_name="assistant",
    agent_version="1.0.0",
)

with sigil.start_generation(
    GenerationStart(
        conversation_id=opts.conversation_id,
        agent_name=opts.agent_name,
        agent_version=opts.agent_version,
        model=ModelRef(provider=opts.provider_name, name=request["model"]),
    )
) as rec:
    try:
        response = provider.responses.create(**request)
        rec.set_result(responses.from_request_response(request, response, opts))
    except Exception as exc:
        rec.set_call_error(exc)
        raise
```

## Raw artifacts (debug opt-in)

Raw artifacts are off by default.

Enable with:

```python
OpenAIOptions(raw_artifacts=True)
```

Artifact names:

- Chat: `openai.chat.request`, `openai.chat.response`, `openai.chat.tools`, `openai.chat.stream_events`
- Responses: `openai.responses.request`, `openai.responses.response`, `openai.responses.tools`, `openai.responses.stream_events`

Call `client.shutdown()` during teardown to flush buffered telemetry.
