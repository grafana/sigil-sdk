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

## Contract

- `handleLLMStart` / `handleChatModelStart` starts recorder lifecycle.
- `handleLLMNewToken` sets first-token timestamp and accumulates streamed output.
- `handleLLMEnd` maps output + usage and ends recorder.
- `handleLLMError` sets call error and ends recorder.

Framework tags and metadata are always injected:

- `sigil.framework.name=langgraph`
- `sigil.framework.source=handler`
- `sigil.framework.language=javascript`
- `metadata["sigil.framework.run_id"]=<framework run id>`

Provider resolver behavior:

- explicit provider metadata when available
- model prefix inference (`gpt-`/`o1`/`o3`/`o4` -> `openai`, `claude-` -> `anthropic`, `gemini-` -> `gemini`)
- fallback `custom`
