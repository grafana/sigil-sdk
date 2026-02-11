# Grafana Sigil Go SDK (Core)

Typed core SDK for normalized generation recording.

## Core model
- `Generation` is the canonical entity.
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
- One-shot: `RecordGeneration(ctx, generation)`
- Lifecycle:
  - `StartGeneration(ctx, start)`
  - `(*GenerationHandle).SetGeneration(g)`
  - `(*GenerationHandle).Finish(ctx, callErr)`
- Trace linking:
  - `TraceID`/`SpanID` are auto-linked from the active OTel span in `ctx` when recording.

## Example
```go
client := sigil.NewClient(sigil.DefaultConfig())

gen := sigil.Generation{
	ID:       "gen-1",
	ThreadID: "thread-9b2f",
	Model: sigil.ModelRef{
		Provider: "anthropic",
		Name:     "claude-sonnet-4-5",
	},
	Messages: []sigil.Message{
		{Role: sigil.RoleUser, Parts: []sigil.Part{sigil.TextPart("Hello")}},
		{Role: sigil.RoleAssistant, Parts: []sigil.Part{sigil.TextPart("Hi!")}},
	},
	Usage: sigil.TokenUsage{InputTokens: 120, OutputTokens: 42},
}

ref, err := client.RecordGeneration(ctx, gen)
_ = ref
_ = err
```
