# Grafana Sigil Go SDK (Core)

Typed core SDK for normalized generation recording with OTEL traces and generation-first export.

## Core model

- `Generation` is the canonical entity.
- `Generation.Mode` is explicit: `SYNC` or `STREAM`.
- `OperationName` defaults are mode-aware:
  - `SYNC` -> `generateText`
  - `STREAM` -> `streamText`
- `ModelRef` bundles `provider + model`.
- `SystemPrompt` is separate from messages.
- `Message` contains typed parts: `text`, `thinking`, `tool_call`, `tool_result`.
- `TokenUsage` includes token/cache/reasoning fields.
- Raw provider `Artifacts` are optional debug payloads.

## Recording API

- `StartGeneration(ctx, start)` -> `(ctx, *GenerationRecorder)`
- `StartStreamingGeneration(ctx, start)` -> `(ctx, *GenerationRecorder)`
- `StartToolExecution(ctx, start)` -> `(ctx, *ToolExecutionRecorder)`
- `End()` is defer-safe and idempotent.
- `rec.Err()` reports only local validation/enqueue failures.
- Background export failures are retried and logged.

## Configuration

```go
cfg := sigil.DefaultConfig()

// Trace export (OTLP)
cfg.Trace.Protocol = sigil.TraceProtocolHTTP // or sigil.TraceProtocolGRPC
cfg.Trace.Endpoint = "http://localhost:4318/v1/traces" // grpc example: "localhost:4317"

// Generation export (custom ingest)
cfg.GenerationExport.Protocol = sigil.GenerationExportProtocolGRPC // default
cfg.GenerationExport.Endpoint = "localhost:4317"                  // HTTP parity: "http://localhost:8080/api/v1/generations:export"
cfg.GenerationExport.BatchSize = 100
cfg.GenerationExport.FlushInterval = time.Second
cfg.GenerationExport.QueueSize = 2000
cfg.GenerationExport.MaxRetries = 5
cfg.GenerationExport.InitialBackoff = 100 * time.Millisecond
cfg.GenerationExport.MaxBackoff = 5 * time.Second

client := sigil.NewClient(cfg)
defer func() {
	_ = client.Shutdown(context.Background())
}()
```

## Lifecycle Requirement

- Always call `client.Shutdown(ctx)` before process exit.
- `Shutdown` flushes pending generation batches and shuts down the trace provider.
- Optional `client.Flush(ctx)` is available for explicit flush points.

## Request/Response Example

```go
ctx, rec := client.StartGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-9b2f",
	Model:          sigil.ModelRef{Provider: "anthropic", Name: "claude-sonnet-4-5"},
})
defer rec.End()

resp, err := provider.Call(ctx, req)
if err != nil {
	rec.SetCallError(err)
	return err
}

rec.SetResult(sigil.Generation{
	Input:  []sigil.Message{sigil.UserTextMessage("Hello")},
	Output: []sigil.Message{sigil.AssistantTextMessage(resp.Text)},
	Usage:  sigil.TokenUsage{InputTokens: 120, OutputTokens: 42},
}, nil)
```

## Streaming Example

```go
ctx, rec := client.StartStreamingGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-stream",
	Model:          sigil.ModelRef{Provider: "openai", Name: "gpt-5"},
})
defer rec.End()

// accumulate stream output...
rec.SetResult(sigil.Generation{
	Input:  []sigil.Message{sigil.UserTextMessage("Say hello")},
	Output: []sigil.Message{sigil.AssistantTextMessage(stitchedOutput)},
}, nil)
```

## Raw Artifact Policy

- Default: raw artifacts OFF in provider wrappers.
- Opt-in only for debug workflows (`WithRawArtifacts()` in provider helper packages).
- Normalized generation fields remain always on (system prompt, tools schema, stitched output, usage, etc.).
