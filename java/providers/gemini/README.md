# Grafana Sigil Java Provider Adapter: Gemini

This module maps Gemini request/response and stream events into Sigil normalized generation models.

## Scope

- One-liner wrappers:
  - `GeminiAdapter.completion(...)`
  - `GeminiAdapter.completionStream(...)`
- Explicit mapper APIs:
  - `GeminiAdapter.fromRequestResponse(...)`
  - `GeminiAdapter.fromStream(...)`

## Official SDK

Designed to pair with the official Google GenAI Java SDK:

- `com.google.genai:google-genai`

## Wrapper Example (sync)

```java
GeminiAdapter.completion(
    sigilClient,
    request,
    r -> {
        // call official Gemini SDK here
        return new OpenAiAdapter.OpenAiChatResponse().setOutputText("answer");
    },
    new OpenAiAdapter.OpenAiOptions()
        .setConversationId("conv-1")
        .setAgentName("assistant-gemini")
        .setAgentVersion("1.0.0")
);
```

## Wrapper Example (stream)

```java
GeminiAdapter.completionStream(
    sigilClient,
    request,
    r -> new OpenAiAdapter.OpenAiStreamSummary()
        .setOutputText("stitched")
        .setChunks(java.util.List.of(/* stream events */)),
    new OpenAiAdapter.OpenAiOptions()
);
```

## Raw Artifact Policy

- Default: OFF
- Opt-in: `OpenAiAdapter.OpenAiOptions#setRawArtifacts(true)`

## Best Practices

- Keep model/version mapping explicit to avoid provider drift.
- Keep stream event payload artifacts bounded in size.
- Use `withStreamingGeneration(...)` when mapping streaming flows.
