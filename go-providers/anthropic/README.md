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

## Example
```go
gen, err := anthropic.FromRequestResponse(req, resp,
	anthropic.WithThreadID("thread-9b2f"),
	anthropic.WithTag("tenant", "t-123"),
)
if err != nil {
	return err
}

ref, err := client.RecordGeneration(ctx, gen)
_ = ref
```
