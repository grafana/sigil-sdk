# Grafana Sigil Go Provider Helper: OpenAI

This module maps OpenAI Chat Completions SDK request/response payloads into the
typed Sigil `Generation` model.

## Scope
- Request/response mapper:
  - `FromRequestResponse(req, resp, opts...)`
- Stream mapper:
  - `FromStream(req, summary, opts...)`
- Typed artifacts:
  - `request`
  - `response`
  - `tools`
  - `provider_event` (stream chunks)

## SDK
- Official SDK: `github.com/openai/openai-go`

## Request/Response Recording
```go
callCtx, rec, err := client.StartGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-9b2f",
	Model:    sigil.ModelRef{Provider: "openai", Name: "gpt-5"},
})
if err != nil {
	return err
}

resp, callErr := openaiClient.Chat.Completions.New(callCtx, req)
if callErr != nil {
	return rec.End(sigil.Generation{}, callErr)
}

gen, mapErr := openai.FromRequestResponse(req, resp)
if err := rec.End(gen, mapErr); err != nil {
	return err
}
```

## Streaming Recording
```go
callCtx, rec, err := client.StartStreamingGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-stream",
	Model:    sigil.ModelRef{Provider: "openai", Name: "gpt-5"},
})
if err != nil {
	return err
}

stream, callErr := openaiClient.Chat.Completions.NewStreaming(callCtx, req)
if callErr != nil {
	return rec.End(sigil.Generation{}, callErr)
}

summary := collectOpenAIStream(stream) // user code: gather final response + chunks

gen, mapErr := openai.FromStream(req, summary)
// gen.Input and gen.Output are both populated by the mapper.
if err := rec.End(gen, mapErr); err != nil {
	return err
}
```
