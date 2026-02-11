# Grafana Sigil Go SDK (Core)

Typed core SDK for normalized generation recording.

## Core model
- `Generation` is the canonical entity.
- `OperationName` defaults to `chat` and maps to `gen_ai.operation.name`.
- `ModelRef` bundles `provider + model`.
- `SystemPrompt` is separate from messages.
- `Message` contains typed `Part` values:
  - `text`
  - `thinking`
  - `tool_call`
  - `tool_result`
- `TokenUsage` includes token/cache/reasoning fields.
- `Tags` and `Metadata` are the only extension maps.
- Provider payload capture goes through typed `Artifacts`.

## Recording API

The API follows OTel-like conventions: `Start` returns a context and a recorder,
`End()` takes no arguments and is safe for `defer`.

- `StartGeneration(ctx, start)` returns `(ctx, *GenerationRecorder)`.
- `StartToolExecution(ctx, start)` returns `(ctx, *ToolExecutionRecorder)`.
- Nil client or invalid input returns a no-op recorder (instrumentation never crashes business logic).
- `End()` is idempotent, nil-safe, and returns nothing.
- `Err()` returns accumulated errors after `End` (like `sql.Rows.Err()`).
- Trace linking is bi-directional:
  - The generation span is a child of the active span in `ctx` when present.
  - `Generation.TraceID` / `Generation.SpanID` are set from the created span.
  - The span stores the generation id in attribute `sigil.generation.id`.

## Request/Response Example
```go
client := sigil.NewClient(sigil.DefaultConfig())

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
ctx, rec := client.StartGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-stream",
	Model:          sigil.ModelRef{Provider: "openai", Name: "gpt-5"},
})
defer rec.End()

stream, err := provider.StartStream(ctx, req)
if err != nil {
	rec.SetCallError(err)
	return err
}

var parts []string
for stream.Next() {
	parts = append(parts, stream.Chunk().Text)
}
if err := stream.Err(); err != nil {
	rec.SetCallError(err)
	return err
}

rec.SetResult(sigil.Generation{
	Input:  []sigil.Message{sigil.UserTextMessage("Say hello")},
	Output: []sigil.Message{sigil.AssistantTextMessage(strings.Join(parts, ""))},
}, nil)
```

## Tool Execution Example
```go
ctx, rec := client.StartToolExecution(ctx, sigil.ToolExecutionStart{
	ToolName:        "weather",
	ToolCallID:      "call_weather",
	ToolType:        "function",
	ToolDescription: "Get weather",
	ConversationID:  "conv-tools",
	IncludeContent:  true, // enables args/result attributes
})
defer rec.End()

result, err := weatherTool.Run(ctx, "Paris")
if err != nil {
	rec.SetExecError(err)
	return err
}

rec.SetResult(sigil.ToolExecutionEnd{
	Arguments: map[string]any{"city": "Paris"},
	Result:    result,
})
```

## Context Conversation Propagation
```go
// Set once, flows through all Start calls.
ctx = sigil.WithConversationID(ctx, "conv-123")

// No need to repeat ConversationID in GenerationStart.
ctx, rec := client.StartGeneration(ctx, sigil.GenerationStart{
	Model: sigil.ModelRef{Provider: "openai", Name: "gpt-5"},
})
defer rec.End()
```

## Sentinel Errors
```go
rec.End()
if errors.Is(rec.Err(), sigil.ErrStoreFailed) {
	// handle store failure
}
```
