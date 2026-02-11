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
