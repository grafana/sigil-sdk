# Grafana Sigil Go Provider Helper: Gemini

This module maps Google Gemini (Gen AI SDK) request/response payloads into the
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
  - `provider_event` (stream responses)

## SDK
- Official SDK: `google.golang.org/genai`

## Request/Response Recording
```go
callCtx, rec, err := client.StartGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-9b2f",
	Model:    sigil.ModelRef{Provider: "gemini", Name: req.Model},
})
if err != nil {
	return err
}

resp, callErr := geminiClient.Models.GenerateContent(callCtx, req.Model, req.Contents, req.Config)
if callErr != nil {
	return rec.End(sigil.Generation{}, callErr)
}

gen, mapErr := gemini.FromRequestResponse(req, resp)
if err := rec.End(gen, mapErr); err != nil {
	return err
}
```

## Streaming Recording
```go
callCtx, rec, err := client.StartStreamingGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-stream",
	Model:    sigil.ModelRef{Provider: "gemini", Name: req.Model},
})
if err != nil {
	return err
}

stream, callErr := geminiClient.Models.GenerateContentStream(callCtx, req.Model, req.Contents, req.Config)
if callErr != nil {
	return rec.End(sigil.Generation{}, callErr)
}

summary := collectGeminiStream(stream) // user code: gather final response + events

gen, mapErr := gemini.FromStream(req, summary)
// gen.Input and gen.Output are both populated by the mapper.
if err := rec.End(gen, mapErr); err != nil {
	return err
}
```
