# Grafana Sigil Go Provider Helper: OpenAI

This module maps official OpenAI Go SDK request/response payloads into typed Sigil `Generation` records for both Chat Completions and Responses APIs.

## Scope

- One-liner wrappers:
  - `ChatCompletionsNew(ctx, sigilClient, provider, req, opts...)`
  - `ChatCompletionsNewStreaming(ctx, sigilClient, provider, req, opts...)`
  - `ResponsesNew(ctx, sigilClient, provider, req, opts...)`
  - `ResponsesNewStreaming(ctx, sigilClient, provider, req, opts...)`
- Mapper functions:
  - `ChatCompletionsFromRequestResponse(req, resp, opts...)`
  - `ChatCompletionsFromStream(req, summary, opts...)`
  - `ResponsesFromRequestResponse(req, resp, opts...)`
  - `ResponsesFromStream(req, summary, opts...)`

## Integration styles

- Strict wrappers: use `ChatCompletionsNew*` / `ResponsesNew*` for one-call instrumentation.
- Manual instrumentation: use `sigil.Client.StartGeneration` or `StartStreamingGeneration` and map strict OpenAI request/response payloads with `ChatCompletionsFrom*` or `ResponsesFrom*`.

## SDK

- Official SDK: `github.com/openai/openai-go/v3`

## Chat Completions Wrapper

```go
resp, err := openai.ChatCompletionsNew(ctx, sigilClient, providerClient, req,
	openai.WithConversationID("conv-1"),
	openai.WithAgentName("assistant-openai"),
	openai.WithAgentVersion("1.0.0"),
)
if err != nil {
	return err
}
_ = resp.Choices[0].Message.Content
```

## Responses Wrapper

```go
resp, err := openai.ResponsesNew(ctx, sigilClient, providerClient, req,
	openai.WithConversationID("conv-1"),
	openai.WithAgentName("assistant-openai"),
	openai.WithAgentVersion("1.0.0"),
)
if err != nil {
	return err
}
_ = resp.ID
```

## Defer Pattern (explicit control)

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

rec.SetResult(openai.ChatCompletionsFromRequestResponse(req, resp))
```

## Streaming Defer Pattern

```go
ctx, rec := sigilClient.StartStreamingGeneration(ctx, sigil.GenerationStart{
	Model: sigil.ModelRef{Provider: "openai", Name: "gpt-5"},
})
defer rec.End()

stream := openaiClient.Responses.NewStreaming(ctx, req)
defer stream.Close()

summary := openai.ResponsesStreamSummary{}
for stream.Next() {
	summary.Events = append(summary.Events, stream.Current())
	// process event here
}
if err := stream.Err(); err != nil {
	rec.SetCallError(err)
	return err
}

rec.SetResult(openai.ResponsesFromStream(req, summary))
```

## Raw artifact policy

- Default: raw request/response/provider-event artifacts are OFF.
- Opt-in with `WithRawArtifacts()`.

## Live SDK examples

Real end-to-end examples using the actual OpenAI SDK (no fake provider calls) are in `sdk_example_test.go`.

```bash
SIGIL_RUN_LIVE_EXAMPLES=1 OPENAI_API_KEY=... go test -run Example_withSigil -v
```
