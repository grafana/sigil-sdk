# Grafana Sigil Go Provider Helper: Anthropic

This module maps Anthropic SDK request/response payloads into the typed Sigil core `Generation` model.

## Scope
- Request/response mapper:
  - `FromRequestResponse(req, resp, opts...)`
- Streaming mapper:
  - `FromStream(req, summary, opts...)`
- Includes typed artifacts for:
  - `request`
  - `response`
  - `tools`
  - `provider_event` (stream events)

## Request/Response Recording
```go
callCtx, rec, err := client.StartGeneration(ctx, sigil.GenerationStart{
	ThreadID: "thread-9b2f",
	Model:    sigil.ModelRef{Provider: "anthropic", Name: "claude-sonnet-4-5"},
})
if err != nil {
	return err
}

resp, callErr := anthropicClient.Beta.Messages.New(callCtx, req)
if callErr != nil {
	return rec.End(sigil.Generation{}, callErr)
}

gen, mapErr := anthropic.FromRequestResponse(req, resp,
	anthropic.WithThreadID("thread-9b2f"),
	anthropic.WithTag("tenant", "t-123"),
)

if err := rec.End(gen, mapErr); err != nil {
	return err
}
```

## Streaming Recording
```go
callCtx, rec, err := client.StartStreamingGeneration(ctx, sigil.GenerationStart{
	ThreadID: "thread-stream",
	Model:    sigil.ModelRef{Provider: "anthropic", Name: "claude-sonnet-4-5"},
})
if err != nil {
	return err
}

stream, callErr := anthropicClient.Beta.Messages.NewStreaming(callCtx, req)
if callErr != nil {
	return rec.End(sigil.Generation{}, callErr)
}

summary := collectAnthropicStream(stream) // user code: gather final response + events

gen, mapErr := anthropic.FromStream(req, summary,
	anthropic.WithThreadID("thread-stream"),
)
// gen.Input and gen.Output are both populated by the mapper.

if err := rec.End(gen, mapErr); err != nil {
	return err
}
```
