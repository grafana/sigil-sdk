# LangGraph Handler (`@grafana/sigil-sdk-js/langgraph`)

Use `SigilLangGraphHandler` to map LangGraph callback lifecycle events to Sigil generation records.

## Install

```bash
pnpm add @grafana/sigil-sdk-js @langchain/core @langchain/langgraph @langchain/openai
```

## Usage

```ts
import { SigilClient } from '@grafana/sigil-sdk-js';
import { SigilLangGraphHandler } from '@grafana/sigil-sdk-js/langgraph';

const client = new SigilClient();
const handler = new SigilLangGraphHandler(client, { providerResolver: 'auto' });
```

## End-to-end example (graph invoke + stream)

```ts
import { ChatOpenAI } from '@langchain/openai';
import { END, START, StateGraph, Annotation } from '@langchain/langgraph';
import { SigilClient } from '@grafana/sigil-sdk-js';
import { SigilLangGraphHandler } from '@grafana/sigil-sdk-js/langgraph';

const GraphState = Annotation.Root({
  prompt: Annotation<string>(),
  answer: Annotation<string>(),
});

const client = new SigilClient();
const handler = new SigilLangGraphHandler(client, {
  providerResolver: 'auto',
  agentName: 'langgraph-example',
  agentVersion: '1.0.0',
});
const llm = new ChatOpenAI({ model: 'gpt-4o-mini', temperature: 0 });

const graph = new StateGraph(GraphState)
  .addNode('model', async (state) => {
    const response = await llm.invoke(state.prompt, { callbacks: [handler] });
    return { answer: String(response.content) };
  })
  .addEdge(START, 'model')
  .addEdge('model', END)
  .compile();

// Non-stream graph invocation.
const out = await graph.invoke({ prompt: 'Explain SLO burn rate in one paragraph.', answer: '' });
console.log(out.answer);

// Streamed graph updates.
for await (const _event of graph.stream({ prompt: 'List three practical alerting tips.', answer: '' })) {
  // Consume events to drive streamed model execution.
}

await client.shutdown();
```

## Persistent thread example (LangGraph checkpointer)

```ts
import { MemorySaver } from '@langchain/langgraph';

const checkpointer = new MemorySaver();
const persistedGraph = new StateGraph(GraphState)
  .addNode('model', async (state) => {
    const response = await llm.invoke(state.prompt, { callbacks: [handler] });
    return { answer: String(response.content) };
  })
  .addEdge(START, 'model')
  .addEdge('model', END)
  .compile({ checkpointer });
const threadConfig = {
  callbacks: [handler],
  configurable: { thread_id: 'customer-42' },
};

await persistedGraph.invoke({ prompt: 'Remember that my timezone is UTC+1.', answer: '' }, threadConfig);
await persistedGraph.invoke({ prompt: 'What timezone did I just give you?', answer: '' }, threadConfig);
```

When `thread_id` is present, the handler records:

- `conversationId=<thread_id>`
- `metadata["sigil.framework.run_id"]=<run id>`
- `metadata["sigil.framework.thread_id"]=<thread id>`
- generation span attributes `sigil.framework.run_id` and `sigil.framework.thread_id`

## Contract

- `handleLLMStart` / `handleChatModelStart` starts recorder lifecycle.
- `handleLLMNewToken` sets first-token timestamp and accumulates streamed output.
- `handleLLMEnd` maps output + usage and ends recorder.
- `handleLLMError` sets call error and ends recorder.
- `handleToolStart` / `handleToolEnd` / `handleToolError` maps into `startToolExecution(...)`.
- `handleChainStart` / `handleChainEnd` / `handleChainError` emits framework chain spans.
- `handleRetrieverStart` / `handleRetrieverEnd` / `handleRetrieverError` emits framework retriever spans.

Framework tags and metadata are always injected:

- `sigil.framework.name=langgraph`
- `sigil.framework.source=handler`
- `sigil.framework.language=javascript`
- `metadata["sigil.framework.run_id"]=<framework run id>`
- `metadata["sigil.framework.thread_id"]=<thread id>` (when present in callback metadata/config)
- `metadata["sigil.framework.parent_run_id"]=<parent run id>` (when available)
- `metadata["sigil.framework.component_name"]=<serialized component name>`
- `metadata["sigil.framework.run_type"]=<llm|chat|tool|chain|retriever>`
- `metadata["sigil.framework.tags"]=<normalized callback tags>`
- `metadata["sigil.framework.retry_attempt"]=<attempt>` (when available)
- `metadata["sigil.framework.langgraph.node"]=<node id>` (when callback context exposes it)
- generation span attributes mirror low-cardinality framework metadata keys

Provider resolver behavior:

- explicit provider metadata when available
- model prefix inference (`gpt-`/`o1`/`o3`/`o4` -> `openai`, `claude-` -> `anthropic`, `gemini-` -> `gemini`)
- fallback `custom`
