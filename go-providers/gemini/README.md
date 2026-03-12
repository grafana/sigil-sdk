# Grafana Sigil Go Provider Helper: Gemini

This module maps Google Gemini GenerateContent SDK request/response payloads into the
typed Sigil `Generation` model.

## Scope
- One-liner wrappers:
  - `GenerateContent(ctx, sigilClient, provider, model, contents, config, opts...)`
  - `GenerateContentStream(ctx, sigilClient, provider, model, contents, config, opts...)`
  - `EmbedContent(ctx, sigilClient, provider, model, contents, config, opts...)`
- Request/response mapper:
  - `FromRequestResponse(model, contents, config, resp, opts...)`
  - `EmbeddingFromResponse(model, contents, config, resp)`
- Stream mapper:
  - `FromStream(model, contents, config, summary, opts...)`
- Typed artifacts:
  - `request`
  - `response`
  - `tools`
  - `provider_event` (stream responses)

## SDK
- Official SDK: `google.golang.org/genai`

## Wrapper (one-liner)
```go
resp, err := gemini.GenerateContent(ctx, sigilClient, providerClient, model, contents, config,
	gemini.WithConversationID("conv-1"),
	gemini.WithConversationTitle("Weather follow-up"),
	gemini.WithAgentName("assistant-gemini"),
	gemini.WithAgentVersion("1.0.0"),
)
if err != nil {
	return err
}
_ = resp.Candidates[0].Content.Parts[0].Text
```

## Embedding Wrapper

```go
embedResp, err := gemini.EmbedContent(ctx, sigilClient, providerClient, "gemini-embedding-001", contents, &genai.EmbedContentConfig{})
if err != nil {
	return err
}
_ = embedResp.Embeddings
```

## Defer Pattern (full control)
```go
ctx, rec := sigilClient.StartGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-9b2f",
	AgentName:      "assistant-gemini",
	AgentVersion:   "1.0.0",
	Model:          sigil.ModelRef{Provider: "gemini", Name: "gemini-2.5-pro"},
})
defer rec.End()

resp, err := geminiClient.Models.GenerateContent(ctx, model, contents, config)
if err != nil {
	rec.SetCallError(err)
	return err
}

rec.SetResult(gemini.FromRequestResponse(model, contents, config, resp))
```

## Streaming Defer Pattern
```go
ctx, rec := sigilClient.StartStreamingGeneration(ctx, sigil.GenerationStart{
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

rec.SetResult(gemini.FromStream(model, contents, config, summary))
```

## Live SDK examples
Real end-to-end examples using the actual Gemini SDK (no fake provider calls) are in:
- `sdk_example_test.go`

Run them with:
```bash
SIGIL_RUN_LIVE_EXAMPLES=1 GOOGLE_API_KEY=... go test -run Example_withSigil -v
```

## Provider metadata mapping

Gemini-specific fields are mapped as follows:

- `usage.thoughtsTokenCount` -> normalized `usage.reasoning_tokens`
- `usage.toolUsePromptTokenCount` -> metadata `sigil.gen_ai.usage.tool_use_prompt_tokens`
- `config.thinkingConfig.thinkingBudget` -> metadata `sigil.gen_ai.request.thinking.budget_tokens`
- `config.thinkingConfig.thinkingLevel` -> metadata `sigil.gen_ai.request.thinking.level`
- `function_response.id` -> normalized `tool_result.tool_call_id` when present
- Gemini helper constructors can surface `function_response` parts without an ID; in that case the mapper preserves `tool_result.name` as the fallback correlation key
