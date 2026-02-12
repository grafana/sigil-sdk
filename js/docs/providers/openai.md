# Sigil JS Provider Helper: OpenAI

This helper maps OpenAI request/response payloads into Sigil `Generation` records.

## Scope

- Wrapper calls:
  - `openai.chatCompletion(client, request, providerCall, options?)`
  - `openai.chatCompletionStream(client, request, providerCall, options?)`
- Mapper functions:
  - `openai.fromRequestResponse(request, response, options?)`
  - `openai.fromStream(request, summary, options?)`
- Raw artifacts (debug opt-in):
  - `request`
  - `response` (sync)
  - `provider_event` (stream)

## Wrapper-first example

```ts
import { SigilClient, openai } from "@grafana/sigil-sdk-js";

const client = new SigilClient();

const response = await openai.chatCompletion(
  client,
  {
    model: "gpt-5",
    messages: [{ role: "user", content: "Hello" }],
  },
  async (request) => {
    const sdkResp = await provider.chat.completions.create(request);
    return {
      id: sdkResp.id,
      model: sdkResp.model,
      outputText: sdkResp.choices?.[0]?.message?.content ?? "",
    };
  }
);
```

## Explicit flow example

```ts
const recorder = client.startGeneration({
  model: { provider: "openai", name: "gpt-5" },
});

try {
  const response = await provider.chat.completions.create(request);
  recorder.setResult(openai.fromRequestResponse(request, response));
} catch (error) {
  recorder.setCallError(error);
  throw error;
} finally {
  recorder.end();
}
```

## Raw artifact policy

- Default OFF.
- Enable only for debug workflows with `{ rawArtifacts: true }`.
