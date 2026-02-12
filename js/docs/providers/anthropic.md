# Sigil JS Provider Helper: Anthropic

This helper maps Anthropic request/response payloads into Sigil `Generation` records.

## Scope

- Wrapper calls:
  - `anthropic.completion(client, request, providerCall, options?)`
  - `anthropic.completionStream(client, request, providerCall, options?)`
- Mapper functions:
  - `anthropic.fromRequestResponse(request, response, options?)`
  - `anthropic.fromStream(request, summary, options?)`
- Raw artifacts (debug opt-in):
  - `request`
  - `response` (sync)
  - `provider_event` (stream)

## Wrapper-first example

```ts
import { SigilClient, anthropic } from "@grafana/sigil-sdk-js";

const client = new SigilClient();

const response = await anthropic.completion(
  client,
  {
    model: "claude-sonnet-4-5",
    messages: [{ role: "user", content: "Hello" }],
  },
  async (request) => {
    const sdkResp = await provider.messages.create(request);
    return {
      id: sdkResp.id,
      model: sdkResp.model,
      outputText: sdkResp.output_text ?? "",
    };
  }
);
```

## Explicit flow example

```ts
const recorder = client.startGeneration({
  model: { provider: "anthropic", name: "claude-sonnet-4-5" },
});

try {
  const response = await provider.messages.create(request);
  recorder.setResult(anthropic.fromRequestResponse(request, response));
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
