# LangChain Handler (`@grafana/sigil-sdk-js/langchain`)

Use `SigilLangChainHandler` to map LangChain callback lifecycle events to Sigil generation records.

## Install

```bash
pnpm add @grafana/sigil-sdk-js @langchain/core @langchain/openai
```

## Usage

```ts
import { SigilClient } from '@grafana/sigil-sdk-js';
import { SigilLangChainHandler } from '@grafana/sigil-sdk-js/langchain';

const client = new SigilClient();
const handler = new SigilLangChainHandler(client, { providerResolver: 'auto' });
```

## End-to-end example (invoke + stream)

```ts
import { ChatOpenAI } from '@langchain/openai';
import { SigilClient } from '@grafana/sigil-sdk-js';
import { SigilLangChainHandler } from '@grafana/sigil-sdk-js/langchain';

const client = new SigilClient();
const handler = new SigilLangChainHandler(client, {
  providerResolver: 'auto',
  agentName: 'langchain-example',
  agentVersion: '1.0.0',
});

const llm = new ChatOpenAI({ model: 'gpt-4o-mini', temperature: 0 });

// Non-stream call -> SYNC generation mode.
const result = await llm.invoke('Summarize why retry budgets matter.', {
  callbacks: [handler],
});
console.log(result.content);

// Stream call -> STREAM generation mode + TTFT tracking.
const stream = await llm.stream('Give me three short reliability tips.', {
  callbacks: [handler],
});
for await (const chunk of stream) {
  if (chunk.content) process.stdout.write(String(chunk.content));
}
process.stdout.write('\n');

await client.shutdown();
```

## Contract

- `handleLLMStart` / `handleChatModelStart` starts recorder lifecycle.
- `handleLLMNewToken` sets first-token timestamp and accumulates streamed output.
- `handleLLMEnd` maps output + usage and ends recorder.
- `handleLLMError` sets call error and ends recorder.

Framework tags and metadata are always injected:

- `sigil.framework.name=langchain`
- `sigil.framework.source=handler`
- `sigil.framework.language=javascript`
- `metadata["sigil.framework.run_id"]=<framework run id>`

Provider resolver behavior:

- explicit provider metadata when available
- model prefix inference (`gpt-`/`o1`/`o3`/`o4` -> `openai`, `claude-` -> `anthropic`, `gemini-` -> `gemini`)
- fallback `custom`
