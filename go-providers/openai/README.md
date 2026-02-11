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
