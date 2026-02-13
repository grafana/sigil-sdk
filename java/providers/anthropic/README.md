# Grafana Sigil Java Provider Adapter: Anthropic

This module maps Anthropic request/response and stream events into Sigil normalized generation models.

## Scope

- One-liner wrappers:
  - `AnthropicAdapter.completion(...)`
  - `AnthropicAdapter.completionStream(...)`
- Explicit mapper APIs:
  - `AnthropicAdapter.fromRequestResponse(...)`
  - `AnthropicAdapter.fromStream(...)`

## Official SDK

Designed to pair with the official Anthropic Java SDK:

- `com.anthropic:anthropic-java`

## Wrapper Example (sync)

```java
AnthropicAdapter.completion(
    sigilClient,
    request,
    r -> {
        // call official Anthropic SDK here
        return new OpenAiAdapter.OpenAiChatResponse().setOutputText("answer");
    },
    new OpenAiAdapter.OpenAiOptions()
        .setConversationId("conv-1")
        .setAgentName("assistant-anthropic")
        .setAgentVersion("1.0.0")
);
```

## Wrapper Example (stream)

```java
AnthropicAdapter.completionStream(
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

- Keep system prompt handling explicit in request mapping.
- Validate tool-call and tool-result role mapping in tests.
- Prefer callback wrapper APIs so recorder lifecycle is always closed.
