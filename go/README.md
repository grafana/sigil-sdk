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
- `StartGeneration(ctx, start)` returns:
  - `callCtx`: use this context for your request/response provider call.
  - `recorder`: call `recorder.End(generation, callErr)` once when done.
- `StartStreamingGeneration(ctx, start)` returns:
  - `callCtx`: use this context for your streaming provider call.
  - `recorder`: call `recorder.End(generation, callErr)` once when done.
- `StartToolExecution(ctx, start)` returns:
  - `callCtx`: use this context for your tool execution call.
  - `recorder`: call `recorder.End(toolEnd, execErr)` once when done.
- `End` sets GenAI attributes, persists artifacts, sets span status, and closes the span.
- `Generation` separates `Input` and `Output` message lists.
- Trace linking is bi-directional:
  - The generation span is a child of the active span in `ctx` when present.
  - `Generation.TraceID` / `Generation.SpanID` are set from the created span.
  - The span stores the generation id in attribute `sigil.generation.id`.

## Request/Response Example
```go
client := sigil.NewClient(sigil.DefaultConfig())

callCtx, rec, err := client.StartGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-9b2f",
	Model:          sigil.ModelRef{Provider: "anthropic", Name: "claude-sonnet-4-5"},
})
if err != nil {
	return err
}

resp, callErr := provider.Call(callCtx, req)

gen := sigil.Generation{
	Input: []sigil.Message{
		{Role: sigil.RoleUser, Parts: []sigil.Part{sigil.TextPart("Hello")}},
	},
	Output: []sigil.Message{
		{Role: sigil.RoleAssistant, Parts: []sigil.Part{sigil.TextPart(resp.Text)}},
	},
	Usage: sigil.TokenUsage{InputTokens: 120, OutputTokens: 42},
}

if err := rec.End(gen, callErr); err != nil {
	return err
}
```

## Streaming Example
```go
callCtx, rec, err := client.StartStreamingGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-stream",
	Model:          sigil.ModelRef{Provider: "openai", Name: "gpt-5"},
})
if err != nil {
	return err
}

stream, callErr := provider.StartStream(callCtx, req)
if callErr != nil {
	return rec.End(sigil.Generation{}, callErr)
}

var parts []string
for stream.Next() {
	parts = append(parts, stream.Chunk().Text)
}
streamErr := stream.Err()

gen := sigil.Generation{
	Input: []sigil.Message{
		{Role: sigil.RoleUser, Parts: []sigil.Part{sigil.TextPart("Say hello")}},
	},
	Output: []sigil.Message{
		{Role: sigil.RoleAssistant, Parts: []sigil.Part{sigil.TextPart(strings.Join(parts, ""))}},
	},
}

if err := rec.End(gen, streamErr); err != nil {
	return err
}
```

## Tool Execution Example
```go
callCtx, rec, err := client.StartToolExecution(ctx, sigil.ToolExecutionStart{
	ToolName:        "weather",
	ToolCallID:      "call_weather",
	ToolType:        "function",
	ToolDescription: "Get weather",
	ConversationID:  "conv-tools",
	IncludeContent:  true, // enables args/result attributes
})
if err != nil {
	return err
}

result, execErr := weatherTool.Run(callCtx, "Paris")

if err := rec.End(sigil.ToolExecutionEnd{
	Arguments: map[string]any{"city": "Paris"},
	Result:    result,
}, execErr); err != nil {
	return err
}
```
