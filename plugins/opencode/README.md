# @grafana/sigil-opencode

OpenCode plugin that records LLM generations to Grafana Sigil for AI observability.

## What it does

Hooks into OpenCode's chat lifecycle to capture assistant messages and send them to Sigil as generation telemetry. Tracks conversation context, tool usage, model metadata, and optionally full message content with PII redaction.

## Setup

1. Create `~/.config/opencode/opencode-sigil.json`:

```json
{
  "enabled": true,
  "endpoint": "http://localhost:8080",
  "auth": { "mode": "none" },
  "agentName": "opencode",
  "contentCapture": true
}
```

2. Register the plugin in your OpenCode configuration.

### Auth modes

- `none` -- no authentication (local dev)
- `bearer` -- `{ "mode": "bearer", "bearerToken": "..." }`
- `tenant` -- `{ "mode": "tenant", "tenantId": "..." }`
- `basic` -- `{ "mode": "basic", "tenantId": "...", "token": "..." }`

## Development

```bash
# From the repo root
pnpm install
pnpm --filter @grafana/sigil-opencode build
pnpm --filter @grafana/sigil-opencode test
```

The `@grafana/sigil-sdk-js` dependency resolves via pnpm workspace linking to `sdks/js`.
