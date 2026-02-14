# Grafana Sigil Java SDK (Core)

If you already use OpenTelemetry, Sigil is a thin extension for AI observability.

The Java SDK records normalized generation payloads, correlates them with traces, and exports both through transport-aware clients.

## Requirements

- Java 17+
- OpenTelemetry SDK already in your app (optional but recommended)

## Modules

- `:core`: runtime client, models, validation, generation exporters, trace wiring
- `:providers:openai`: OpenAI wrapper + mapper helpers
- `:providers:anthropic`: Anthropic wrapper + mapper helpers
- `:providers:gemini`: Gemini wrapper + mapper helpers
- `:benchmarks`: JMH benchmark suite

## Core Model

- `Generation` is the canonical record.
- `GenerationMode` is explicit: `SYNC` or `STREAM`.
- Operation defaults are mode-aware:
  - `SYNC` -> `generateText`
  - `STREAM` -> `streamText`
- `ModelRef` is `provider + model`.
- `Message` has typed `MessagePart` variants: `text`, `thinking`, `tool_call`, `tool_result`.
- Raw provider artifacts are optional and default OFF.

## Recording API

- `startGeneration(start)`
- `startStreamingGeneration(start)`
- `withGeneration(start, fn)`
- `withStreamingGeneration(start, fn)`
- `startToolExecution(start)`
- `withToolExecution(start, fn)`
- `flush()` and `shutdown()`

`GenerationRecorder#error()` reports local SDK failures only (validation/enqueue/shutdown), not provider call exceptions.

## Quick Start (sync)

```java
SigilClient client = new SigilClient(new SigilClientConfig()
    .setApi(new ApiConfig()
        .setEndpoint("http://localhost:8080"))
    .setGenerationExport(new GenerationExportConfig()
        .setProtocol(GenerationExportProtocol.HTTP)
        .setEndpoint("http://localhost:8080/api/v1/generations:export")
        .setAuth(new AuthConfig().setMode(AuthMode.TENANT).setTenantId("dev-tenant")))
    .setTrace(new TraceConfig()
        .setProtocol(TraceProtocol.OTLP_HTTP)
        .setEndpoint("http://localhost:4318/v1/traces")));

try {
    client.withGeneration(
        new GenerationStart()
            .setConversationId("conv-1")
            .setModel(new ModelRef().setProvider("openai").setName("gpt-5")),
        recorder -> {
            recorder.setResult(new GenerationResult()
                .setOutput(java.util.List.of(
                    new Message().setRole(MessageRole.ASSISTANT)
                        .setParts(java.util.List.of(MessagePart.text("Hello from Sigil"))))));
            return null;
        }
    );
} finally {
    client.shutdown();
}
```

## Streaming Pattern

Use `startStreamingGeneration(...)` or `withStreamingGeneration(...)`. The SDK sets mode to `STREAM` and keeps operation naming consistent.

## Auth Modes

Auth is configured independently for trace and generation export:

- `NONE`
- `TENANT` (injects `X-Scope-OrgID`)
- `BEARER` (injects `Authorization: Bearer <token>`)

Invalid combinations fail fast at client construction. If explicit headers already contain `Authorization` or `X-Scope-OrgID`, explicit headers win.

Generation export transport protocols:

- `GenerationExportProtocol.HTTP`
- `GenerationExportProtocol.GRPC`
- `GenerationExportProtocol.NONE` (instrumentation-only; no generation transport)

## Context Defaults

You can set defaults via OTel context and override per-call in `GenerationStart`:

```java
try (Scope ignored = SigilContext.withConversationId("conv-ctx")) {
    GenerationRecorder rec = client.startGeneration(new GenerationStart()
        .setModel(new ModelRef().setProvider("openai").setName("gpt-5")));
    // ...
    rec.end();
}
```

Helpers:

- `SigilContext.withConversationId(...)`
- `SigilContext.withAgentName(...)`
- `SigilContext.withAgentVersion(...)`

## Conversation Ratings

Use the SDK helper to submit user-facing ratings:

```java
SubmitConversationRatingResponse result = client.submitConversationRating(
    "conv-123",
    new SubmitConversationRatingRequest()
        .setRatingId("rat-123")
        .setRating(ConversationRatingValue.BAD)
        .setComment("Answer ignored user context")
        .setMetadata(Map.of("channel", "assistant-ui"))
        .setSource("sdk-java"));

System.out.println(result.getRating().getRating() + " hasBad=" + result.getSummary().isHasBadRating());
```

`submitConversationRating(...)` sends requests to `ApiConfig.endpoint` (default `http://localhost:8080`) and uses the same generation-export auth headers (`tenant` or `bearer`) already configured on the SDK client.

## Lifecycle

- Always call `shutdown()` before process exit.
- `shutdown()` flushes generation batches and closes trace exporter resources.
- `flush()` is available for explicit synchronization points.

## Instrumentation-only mode (no generation send)

```java
SigilClient client = new SigilClient(new SigilClientConfig()
    .setGenerationExport(new GenerationExportConfig()
        .setProtocol(GenerationExportProtocol.NONE))
    .setTrace(new TraceConfig()
        .setProtocol(TraceProtocol.OTLP_HTTP)
        .setEndpoint("http://localhost:4318/v1/traces")));
```

## SDK metrics

The SDK emits these OTel histograms automatically on the trace OTLP endpoint:

- `gen_ai.client.operation.duration`
- `gen_ai.client.token.usage`
- `gen_ai.client.time_to_first_token`
- `gen_ai.client.tool_calls_per_operation`

## Provider Wrappers

Provider modules are wrapper-first for ergonomics, with explicit mapper APIs for full control:

- OpenAI: `providers/openai/README.md`
- Anthropic: `providers/anthropic/README.md`
- Gemini: `providers/gemini/README.md`

## Best Practices

- Keep raw artifacts disabled in production unless actively debugging.
- Prefer callback APIs (`withGeneration` / `withStreamingGeneration`) to guarantee `end()` runs.
- Separate trace auth config from generation auth config; most deployments need different values.
- Keep `batchSize` and `queueSize` conservative first, then tune with benchmark data.

## Build, Test, Benchmark

From `sdks/java`:

```bash
./gradlew test
./gradlew :benchmarks:jmh
```

Or from repo root:

```bash
mise run test:java:sdk-all
mise run benchmark:java:sdk
```
