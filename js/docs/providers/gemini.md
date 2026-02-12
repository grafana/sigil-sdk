# Sigil JS Provider Helper: Gemini

This helper maps Gemini request/response payloads into Sigil `Generation` records.

## Scope

- Wrapper calls:
  - `gemini.completion(client, request, providerCall, options?)`
  - `gemini.completionStream(client, request, providerCall, options?)`
- Mapper functions:
  - `gemini.fromRequestResponse(request, response, options?)`
  - `gemini.fromStream(request, summary, options?)`
- Raw artifacts (debug opt-in):
  - `request`
  - `response` (sync)
  - `provider_event` (stream)

## Wrapper-first example

```ts
import { SigilClient, gemini } from "@grafana/sigil-sdk-js";

const client = new SigilClient();

const response = await gemini.completion(
  client,
  {
    model: "gemini-2.5-pro",
    messages: [{ role: "user", content: "Hello" }],
  },
  async (request) => {
    const sdkResp = await provider.models.generateContent(request);
    return {
      id: sdkResp.responseId,
      model: sdkResp.modelVersion,
      outputText: sdkResp.text ?? "",
    };
  }
);
```

## Explicit flow example

```ts
const recorder = client.startGeneration({
  model: { provider: "gemini", name: "gemini-2.5-pro" },
});

try {
  const response = await provider.models.generateContent(request);
  recorder.setResult(gemini.fromRequestResponse(request, response));
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
