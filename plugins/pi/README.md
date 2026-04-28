# pi-sigil

[Pi](https://github.com/badlogic/pi) agent extension that records LLM generations to Grafana AI observability.

By default only metadata is sent (token counts, cost, model, tool names, durations). Message content is included only when `contentCapture` is set to `full` or `no_tool_content`.

## Install

```bash
pnpm --filter pi-sigil run build
pi install /absolute/path/to/sigil-sdk/plugins/pi
```

## Configure

Create `~/.config/sigil-pi/config.json`:

```json
{
  "endpoint": "https://sigil.example.com",
  "auth": {
    "mode": "basic",
    "user": "123456",
    "password": "${GRAFANA_CLOUD_TOKEN}"
  },
  "otlp": {
    "endpoint": "https://otlp-gateway.grafana.net/otlp",
    "basicUser": "123456",
    "basicPassword": "${GRAFANA_CLOUD_TOKEN}"
  }
}
```

Token values support `${ENV_VAR}` interpolation.

### Auth modes

**basic** — HTTP Basic auth + X-Scope-OrgID (Grafana Cloud):
```json
{ "mode": "basic", "user": "123456", "password": "${GRAFANA_CLOUD_TOKEN}" }
```

`user` is your Grafana Cloud stack/tenant ID. `password` is a `glc_…` token created in Grafana Cloud -> Access Policies ([docs](https://grafana.com/docs/grafana-cloud/account-management/authentication-and-permissions/access-policies/)) with the appropriate Sigil scope.

**tenant** — X-Scope-OrgID header only:
```json
{ "mode": "tenant", "tenantId": "my-tenant" }
```

**bearer** — Authorization: Bearer header:
```json
{ "mode": "bearer", "bearerToken": "${SIGIL_TOKEN}" }
```

**none** — no auth (default):
```json
{ "mode": "none" }
```

### OTLP (metrics & traces)

The `otlp` block exports OTel histograms and trace spans:

```json
"otlp": {
  "endpoint": "https://otlp-gateway.grafana.net/otlp",
  "basicUser": "123456",
  "basicPassword": "${GRAFANA_CLOUD_TOKEN}"
}
```

### All options

| Field | Default | Description |
|-------|---------|-------------|
| `endpoint` | — | Sigil URL (`/api/v1/generations:export` auto-appended) |
| `auth.mode` | `"none"` | One of `basic`, `tenant`, `bearer`, `none` |
| `auth.user` | — | Basic auth user (Grafana Cloud stack ID) |
| `auth.password` | — | Basic auth password (`glc_…` token) |
| `auth.tenantId` | `auth.user` in basic mode | `X-Scope-OrgID` header (required in `tenant` mode) |
| `auth.bearerToken` | — | Bearer token (required in `bearer` mode) |
| `agentName` | `"pi"` | Agent name reported to Sigil |
| `agentVersion` | auto-detected | Pi agent version |
| `contentCapture` | `"metadata_only"` | `full`, `no_tool_content`, or `metadata_only` |
| `debug` | `false` | Log lifecycle events to stderr |
| `otlp.endpoint` | — | OTLP HTTP endpoint (e.g. `https://otlp-gateway.grafana.net/otlp`) |
| `otlp.basicUser` / `otlp.basicPassword` | — | OTLP Basic auth |
| `otlp.bearerToken` | — | OTLP Bearer token |
| `otlp.headers` | — | Custom OTLP headers |

### Environment variable overrides

Every config field can be overridden via environment variable:

| Variable | Overrides |
|----------|-----------|
| `SIGIL_PI_ENDPOINT` | `endpoint` |
| `SIGIL_PI_AUTH_MODE` | `auth.mode` |
| `SIGIL_PI_TENANT_ID` | `auth.tenantId` |
| `SIGIL_PI_BEARER_TOKEN` | `auth.bearerToken` |
| `SIGIL_PI_BASIC_USER` | `auth.user` |
| `SIGIL_PI_BASIC_PASSWORD` | `auth.password` |
| `SIGIL_PI_AGENT_NAME` | `agentName` |
| `SIGIL_PI_AGENT_VERSION` | `agentVersion` |
| `SIGIL_PI_CONTENT_CAPTURE` | `contentCapture` |
| `SIGIL_PI_DEBUG` | `debug` (`1`/`true` to enable) |
| `SIGIL_PI_OTLP_ENDPOINT` | `otlp.endpoint` |
| `SIGIL_PI_OTLP_BASIC_USER` | `otlp.basicUser` |
| `SIGIL_PI_OTLP_BASIC_PASSWORD` | `otlp.basicPassword` |
| `SIGIL_PI_OTLP_BEARER_TOKEN` | `otlp.bearerToken` |

## What gets sent

Every generation always carries model, token usage (input/output/cache), cost, stop reason, tool names and durations, conversation ID, and turn timing. Message content depends on `contentCapture`:

| Mode | Adds |
|------|------|
| `metadata_only` (default) | nothing — content is stripped by the SDK |
| `no_tool_content` | assistant text and thinking |
| `full` | assistant text, thinking, tool call arguments, tool results |

When `otlp` is configured, the SDK additionally exports `gen_ai.client.operation.duration`, `gen_ai.client.token.usage`, and `gen_ai.client.tool_calls_per_operation` histograms (provider/model/agent labels) plus one trace span per generation.
