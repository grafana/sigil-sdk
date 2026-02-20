# Sigil Python Framework Module: LangGraph

`sigil-sdk-langgraph` provides callback handlers that map LangGraph lifecycle events into Sigil generation recorder lifecycles.

## Installation

```bash
pip install sigil-sdk sigil-sdk-langgraph
pip install langgraph langchain-openai
```

## Usage

```python
from sigil_sdk import Client
from sigil_sdk_langgraph import SigilLangGraphHandler, SigilAsyncLangGraphHandler

client = Client()
handler = SigilLangGraphHandler(client=client, provider_resolver="auto")
async_handler = SigilAsyncLangGraphHandler(client=client, provider_resolver="auto")
```

## End-to-end example (graph invoke + stream)

```python
from typing import TypedDict

from langchain_openai import ChatOpenAI
from langgraph.graph import END, StateGraph
from sigil_sdk import Client
from sigil_sdk_langgraph import SigilLangGraphHandler


class GraphState(TypedDict):
    prompt: str
    answer: str


client = Client()
handler = SigilLangGraphHandler(
    client=client,
    provider_resolver="auto",
    agent_name="langgraph-example",
    agent_version="1.0.0",
)
llm = ChatOpenAI(model="gpt-4o-mini", temperature=0)


def run_model(state: GraphState) -> GraphState:
    response = llm.invoke(state["prompt"], config={"callbacks": [handler]})
    return {"prompt": state["prompt"], "answer": response.content}


workflow = StateGraph(GraphState)
workflow.add_node("model", run_model)
workflow.set_entry_point("model")
workflow.add_edge("model", END)
graph = workflow.compile()

# Non-stream graph invocation.
out = graph.invoke({"prompt": "Explain SLO burn rate in one paragraph.", "answer": ""})
print(out["answer"])

# Streamed graph events.
for _event in graph.stream(
    {"prompt": "List three practical alerting tips.", "answer": ""},
):
    pass

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
  - `sigil.framework.name=langgraph`
  - `sigil.framework.source=handler`
  - `sigil.framework.language=python`
  - `metadata["sigil.framework.run_id"]=<run id>`

Call `client.shutdown()` during teardown to flush buffered telemetry.
