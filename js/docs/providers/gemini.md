# Sigil JS Provider Helper: Gemini

This helper maps strict Gemini `model/contents/config` payloads into Sigil `Generation` records.

## Scope

- Wrapper calls:
  - `gemini.models.generateContent(client, model, contents, config, providerCall, options?)`
  - `gemini.models.generateContentStream(client, model, contents, config, providerCall, options?)`
- Mapper functions:
  - `gemini.models.fromRequestResponse(model, contents, config, response, options?)`
  - `gemini.models.fromStream(model, contents, config, summary, options?)`
- Raw artifacts (debug opt-in):
  - `request`
  - `response` (sync)
  - `provider_event` (stream)

## Wrapper-first example

```ts
import { SigilClient, gemini } from "@grafana/sigil-sdk-js";

const client = new SigilClient();

const model = "gemini-2.5-pro";
const contents = [{ role: "user", parts: [{ text: "Hello" }] }];
const config = { maxOutputTokens: 256 };

const response = await gemini.models.generateContent(
  client,
  model,
  contents,
  config,
  async (reqModel, reqContents, reqConfig) =>
    provider.models.generateContent({ model: reqModel, contents: reqContents, config: reqConfig })
);
```

## Explicit flow example

```ts
const recorder = client.startGeneration({
  model: { provider: "gemini", name: model },
});

try {
  const response = await provider.models.generateContent({ model, contents, config });
  recorder.setResult(gemini.models.fromRequestResponse(model, contents, config, response));
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

## Provider metadata mapping

Gemini-specific fields are mapped as follows:

- `usage.thoughtsTokenCount` -> normalized `usage.reasoningTokens`
- `usage.toolUsePromptTokenCount` -> metadata `sigil.gen_ai.usage.tool_use_prompt_tokens`
- `config.thinkingConfig.thinkingBudget` -> metadata `sigil.gen_ai.request.thinking.budget_tokens`
- `config.thinkingConfig.thinkingLevel` -> metadata `sigil.gen_ai.request.thinking.level`
