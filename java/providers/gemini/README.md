# Grafana Sigil Java Provider Helpers: Gemini

This module provides strict wrappers around official Google GenAI Java SDK types for `models.generateContent`.

No simplified public DTO layer is exposed.

## Public API

- Wrappers:
  - `GeminiAdapter.completion(...)`
  - `GeminiAdapter.completionStream(...)`
- Manual mappers:
  - `GeminiAdapter.fromRequestResponse(...)`
  - `GeminiAdapter.fromStream(...)`

## Official SDK Types

These wrappers use official types from `com.google.genai:google-genai`:

- `Content`
- `GenerateContentConfig`
- `GenerateContentResponse`

## Wrapper Example (sync)

```java
List<Content> contents = List.of(
    Content.builder().role("user").parts(Part.fromText("Summarize this run.")).build()
);

GenerateContentResponse response = GeminiAdapter.completion(
    sigilClient,
    "gemini-2.5-pro",
    contents,
    GenerateContentConfig.builder().maxOutputTokens(512).build(),
    (model, input, cfg) -> genai.models.generateContent(model, input, cfg),
    new GeminiOptions()
        .setConversationId("conv-1")
        .setAgentName("assistant-gemini")
        .setAgentVersion("1.0.0")
);
```

## Wrapper Example (stream)

```java
GeminiStreamSummary summary = GeminiAdapter.completionStream(
    sigilClient,
    "gemini-2.5-pro",
    contents,
    GenerateContentConfig.builder().maxOutputTokens(512).build(),
    (model, input, cfg) -> genai.models.generateContentStream(model, input, cfg),
    new GeminiOptions()
);
```

## Raw Artifact Policy

- Default: OFF
- Opt-in: `new GeminiOptions().setRawArtifacts(true)`

## Provider metadata mapping

Gemini-specific fields are mapped as follows:

- `usage.thoughtsTokenCount` -> normalized `usage.reasoning_tokens`
- `usage.toolUsePromptTokenCount` -> metadata `sigil.gen_ai.usage.tool_use_prompt_tokens`
- `config.thinkingConfig.thinkingBudget` -> metadata `sigil.gen_ai.request.thinking.budget_tokens`
- `config.thinkingConfig.thinkingLevel` -> metadata `sigil.gen_ai.request.thinking.level`
