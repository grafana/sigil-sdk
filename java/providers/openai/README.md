# Grafana Sigil Java Provider Adapter: OpenAI

This module maps OpenAI request/response and stream events into Sigil normalized generation models.

## Scope

- One-liner wrappers:
  - `OpenAiAdapter.chatCompletion(...)`
  - `OpenAiAdapter.chatCompletionStream(...)`
- Explicit mapper APIs:
  - `OpenAiAdapter.fromRequestResponse(...)`
  - `OpenAiAdapter.fromStream(...)`

## Official SDK

Designed to pair with the official OpenAI Java SDK:

- `com.openai:openai-java`

## Wrapper Example (sync)

```java
OpenAiAdapter.OpenAiChatResponse response = OpenAiAdapter.chatCompletion(
    sigilClient,
    new OpenAiAdapter.OpenAiChatRequest()
        .setModel("gpt-5")
        .setMessages(java.util.List.of(
            new OpenAiAdapter.OpenAiMessage().setRole("user").setContent("hello"))),
    request -> {
        // call official SDK here
        return new OpenAiAdapter.OpenAiChatResponse().setOutputText("hello");
    },
    new OpenAiAdapter.OpenAiOptions()
        .setConversationId("conv-1")
        .setAgentName("assistant-openai")
        .setAgentVersion("1.0.0")
);
```

## Wrapper Example (stream)

```java
OpenAiAdapter.OpenAiStreamSummary summary = OpenAiAdapter.chatCompletionStream(
    sigilClient,
    request,
    r -> {
        // collect stream events from official SDK
        return new OpenAiAdapter.OpenAiStreamSummary()
            .setOutputText("stitched output")
            .setChunks(java.util.List.of(/* events */));
    },
    new OpenAiAdapter.OpenAiOptions()
);
```

## Explicit Recorder Pattern

Use explicit start/end when you want full manual control:

```java
GenerationRecorder rec = sigilClient.startGeneration(new GenerationStart()
    .setModel(new ModelRef().setProvider("openai").setName("gpt-5")));
try {
    var mapped = OpenAiAdapter.fromRequestResponse(request, response, new OpenAiAdapter.OpenAiOptions());
    rec.setResult(mapped);
} catch (Exception ex) {
    rec.setCallError(ex);
    throw ex;
} finally {
    rec.end();
}
```

## Raw Artifact Policy

- Default: OFF
- Opt-in: `OpenAiOptions#setRawArtifacts(true)`

When enabled:

- sync flow adds `request` + `response`
- stream flow adds `request` + `provider_event`

## Best Practices

- Keep `rawArtifacts=false` in production paths.
- Filter/trim provider events before storing them as artifacts.
- Keep mapper logic deterministic so parity tests remain stable.
- Set explicit `agentName` and `agentVersion` for better fleet attribution.

## Optional Typed Adapter Layer

If you need a strict compile-time bridge to official SDK object models, add a typed adapter interface in your app module and delegate into `fromRequestResponse(...)` / `fromStream(...)`.
