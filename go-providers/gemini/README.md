# Grafana Sigil Go Provider Helper: Gemini

This module maps Google Gemini GenerateContent SDK request/response payloads into the
typed Sigil `Generation` model.

## Scope
- One-liner wrappers:
  - `GenerateContent(ctx, sigilClient, provider, req, opts...)`
  - `GenerateContentStream(ctx, sigilClient, provider, req, opts...)`
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

## Wrapper (one-liner)
```go
resp, err := gemini.GenerateContent(ctx, sigilClient, providerClient, req,
	gemini.WithConversationID("conv-1"),
)
if err != nil {
	return err
}
_ = resp.Candidates[0].Content.Parts[0].Text
```

## Defer Pattern (full control)
```go
ctx, rec := sigilClient.StartGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-9b2f",
	Model:          sigil.ModelRef{Provider: "gemini", Name: "gemini-2.5-pro"},
})
defer rec.End()

resp, err := geminiClient.Models.GenerateContent(ctx, req.Model, req.Contents, req.Config)
if err != nil {
	rec.SetCallError(err)
	return err
}

rec.SetResult(gemini.FromRequestResponse(req, resp))
```

## Streaming Defer Pattern
```go
ctx, rec := sigilClient.StartGeneration(ctx, sigil.GenerationStart{
	Model: sigil.ModelRef{Provider: "gemini", Name: "gemini-2.5-pro"},
})
defer rec.End()

summary := gemini.StreamSummary{}
for response, err := range geminiClient.Models.GenerateContentStream(ctx, model, contents, config) {
	if err != nil {
		rec.SetCallError(err)
		return err
	}
	summary.Responses = append(summary.Responses, response)
	// process response here
}

rec.SetResult(gemini.FromStream(req, summary))
```

## Live SDK examples
Real end-to-end examples using the actual Gemini SDK (no fake provider calls) are in:
- `sdk_example_test.go`

Run them with:
```bash
SIGIL_RUN_LIVE_EXAMPLES=1 GOOGLE_API_KEY=... go test -run Example_withSigil -v
```
