# Sigil Python Framework Module: LangChain

`sigil-sdk-langchain` provides callback handlers that map LangChain lifecycle events into Sigil generation recorder lifecycles.

## Installation

```bash
pip install sigil-sdk sigil-sdk-langchain
pip install langchain-openai
```

## Usage

```python
from sigil_sdk import Client
from sigil_sdk_langchain import SigilLangChainHandler, SigilAsyncLangChainHandler

client = Client()
handler = SigilLangChainHandler(client=client, provider_resolver="auto")
async_handler = SigilAsyncLangChainHandler(client=client, provider_resolver="auto")
```

## End-to-end example (invoke + stream)

```python
from langchain_openai import ChatOpenAI
from sigil_sdk import Client
from sigil_sdk_langchain import SigilLangChainHandler

client = Client()
handler = SigilLangChainHandler(
    client=client,
    provider_resolver="auto",
    agent_name="langchain-example",
    agent_version="1.0.0",
)

llm = ChatOpenAI(model="gpt-4o-mini", temperature=0)

# Non-stream call -> SYNC generation mode.
result = llm.invoke(
    "Summarize why retry budgets matter.",
    config={"callbacks": [handler]},
)
print(result.content)

# Stream call -> STREAM generation mode + TTFT tracking.
for chunk in llm.stream(
    "Give me three short reliability tips.",
    config={"callbacks": [handler]},
):
    if chunk.content:
        print(chunk.content, end="", flush=True)
print()

client.shutdown()
```

## Behavior

- Lifecycle mapping: `on_*_start` -> start recorder, `on_llm_new_token` -> first-token timestamp, `on_llm_end`/`on_llm_error` -> finalize recorder.
- Mode mapping: non-stream -> `SYNC`, stream -> `STREAM`.
- Provider resolver parity:
  - explicit provider metadata when available
  - model-name inference (`gpt-`/`o1`/`o3`/`o4` -> `openai`, `claude-` -> `anthropic`, `gemini-` -> `gemini`)
  - fallback -> `custom`
- Framework tags/metadata are always set:
  - `sigil.framework.name=langchain`
  - `sigil.framework.source=handler`
  - `sigil.framework.language=python`
  - `metadata["sigil.framework.run_id"]=<run id>`

Call `client.shutdown()` during teardown to flush buffered telemetry.
