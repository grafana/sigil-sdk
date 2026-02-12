# Grafana Sigil Go Provider Helper: OpenAI

This module maps OpenAI Chat Completions SDK request/response payloads into the
typed Sigil `Generation` model.

## Scope
- One-liner wrappers:
  - `ChatCompletion(ctx, sigilClient, provider, req, opts...)`
  - `ChatCompletionStream(ctx, sigilClient, provider, req, opts...)`
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

## Wrapper (one-liner)
```go
resp, err := openai.ChatCompletion(ctx, sigilClient, providerClient, req,
	openai.WithConversationID("conv-1"),
	openai.WithAgentName("assistant-openai"),
	openai.WithAgentVersion("1.0.0"),
)
if err != nil {
	return err
}
_ = resp.Choices[0].Message.Content
```

## Defer Pattern (full control)
```go
ctx, rec := sigilClient.StartGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-9b2f",
	AgentName:      "assistant-openai",
	AgentVersion:   "1.0.0",
	Model:          sigil.ModelRef{Provider: "openai", Name: "gpt-5"},
})
defer rec.End()

resp, err := openaiClient.Chat.Completions.New(ctx, req)
if err != nil {
	rec.SetCallError(err)
	return err
}

rec.SetResult(openai.FromRequestResponse(req, resp))
```

## Streaming Defer Pattern
```go
ctx, rec := sigilClient.StartStreamingGeneration(ctx, sigil.GenerationStart{
	Model: sigil.ModelRef{Provider: "openai", Name: "gpt-5"},
})
defer rec.End()

stream := openaiClient.Chat.Completions.NewStreaming(ctx, req)
defer stream.Close()

summary := openai.StreamSummary{}
for stream.Next() {
	chunk := stream.Current()
	summary.Chunks = append(summary.Chunks, chunk)
	// process chunk here
}
if err := stream.Err(); err != nil {
	rec.SetCallError(err)
	return err
}

rec.SetResult(openai.FromStream(req, summary))
```

## Live SDK examples
Real end-to-end examples using the actual OpenAI SDK (no fake provider calls) are in:
- `sdk_example_test.go`

Run them with:
```bash
SIGIL_RUN_LIVE_EXAMPLES=1 OPENAI_API_KEY=... go test -run Example_withSigil -v
```
