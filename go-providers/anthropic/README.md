# Grafana Sigil Go Provider Helper: Anthropic

This module maps Anthropic Messages SDK request/response payloads into the
typed Sigil `Generation` model.

## Embeddings support

This helper currently supports Anthropic Messages APIs only. Native Anthropic embeddings endpoints are not available in the official SDK/API surface used in this repository.

## Scope
- One-liner wrappers:
  - `Message(ctx, sigilClient, provider, req, opts...)`
  - `MessageStream(ctx, sigilClient, provider, req, opts...)`
- Request/response mapper:
  - `FromRequestResponse(req, resp, opts...)`
- Stream mapper:
  - `FromStream(req, summary, opts...)`
- Typed artifacts:
  - `request`
  - `response`
  - `tools`
  - `provider_event` (stream events)

## SDK
- Official SDK: `github.com/anthropics/anthropic-sdk-go`

## Wrapper (one-liner)
```go
resp, err := anthropic.Message(ctx, sigilClient, providerClient, req,
	anthropic.WithConversationID("conv-1"),
	anthropic.WithAgentName("assistant-anthropic"),
	anthropic.WithAgentVersion("1.0.0"),
)
if err != nil {
	return err
}
_ = resp.Content[0].Text
```

## Defer Pattern (full control)
```go
ctx, rec := sigilClient.StartGeneration(ctx, sigil.GenerationStart{
	ConversationID: "conv-9b2f",
	AgentName:      "assistant-anthropic",
	AgentVersion:   "1.0.0",
	Model:          sigil.ModelRef{Provider: "anthropic", Name: "claude-sonnet-4-5"},
})
defer rec.End()

resp, err := anthropicClient.Beta.Messages.New(ctx, req)
if err != nil {
	rec.SetCallError(err)
	return err
}

rec.SetResult(anthropic.FromRequestResponse(req, resp))
```

## Streaming Defer Pattern
```go
ctx, rec := sigilClient.StartStreamingGeneration(ctx, sigil.GenerationStart{
	Model: sigil.ModelRef{Provider: "anthropic", Name: "claude-sonnet-4-5"},
})
defer rec.End()

stream := anthropicClient.Beta.Messages.NewStreaming(ctx, req)
defer stream.Close()

summary := anthropic.StreamSummary{}
for stream.Next() {
	event := stream.Current()
	summary.Events = append(summary.Events, event)
	// process event here
}
if err := stream.Err(); err != nil {
	rec.SetCallError(err)
	return err
}

rec.SetResult(anthropic.FromStream(req, summary))
```

## Live SDK examples
Real end-to-end examples using the actual Anthropic SDK (no fake provider calls) are in:
- `sdk_example_test.go`

Run them with:
```bash
SIGIL_RUN_LIVE_EXAMPLES=1 ANTHROPIC_API_KEY=... go test -run Example_withSigil -v
```

## Provider metadata mapping

In addition to normalized usage fields, Anthropic server-tool counters are mapped into Sigil metadata when present:

- `sigil.gen_ai.usage.server_tool_use.web_search_requests`
- `sigil.gen_ai.usage.server_tool_use.web_fetch_requests`
- `sigil.gen_ai.usage.server_tool_use.total_requests`
