# @grafana/sigil-pi

[Pi](https://github.com/badlogic/pi) agent extension that records LLM generations to Grafana AI observability.

By default only metadata is sent (token counts, cost, model, tool names, durations). Message content is included only when `contentCapture` is set to `full` or `no_tool_content`.

## Install

```bash
pi install npm:@grafana/sigil-pi
```

Pin a specific version:

```bash
pi install npm:@grafana/sigil-pi@0.1.1
```

### From source (contributors)

```bash
pnpm --filter @grafana/sigil-pi run build
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

### Redaction (pre-ingest secret scrubbing)

The plugin runs every generation through the SDK's secret redaction sanitizer before it leaves the process. Matched values are replaced with `[REDACTED:<id>]`. The sanitizer covers high-confidence formats including:

- Grafana Cloud tokens (`glc_…`) and service account tokens (`glsa_…`)
- Cloud provider keys (AWS, GCP), GitHub PATs, OpenAI / Anthropic / Stripe / SendGrid / Twilio / Slack / npm / PyPI tokens
- PEM-encoded private key blocks
- Database connection strings (`postgres://user:pass@host`, `mysql://…`, `mongodb://…`, `redis://…`, `amqp://…`)
- Environment-style `PASSWORD=…` / `SECRET=…` / `TOKEN=…` / `KEY=…` / `CREDENTIAL=…` / `API_KEY=…` / `PRIVATE_KEY=…` / `ACCESS_KEY=…` pairs
- Bearer tokens
- Email addresses (optional, on by default)

User input messages are scrubbed by default, flip `redactInputMessages` if you want to leave the user side untouched.

Opt out entirely:

```json
"redaction": { "enabled": false }
```

Tweak individual knobs:

```json
"redaction": { "redactEmailAddresses": false }
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
| `redaction.enabled` | `true` | Master switch for pre-ingest secret redaction |
| `redaction.redactInputMessages` | `true` | Also scrub user-role message content (not just assistant/tool output) |
| `redaction.redactEmailAddresses` | `true` | Scrub generic email addresses |

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
| `SIGIL_PI_REDACTION_ENABLED` | `redaction.enabled` (`1`/`true`/`yes`/`on` to enable, `0`/`false`/`no`/`off` to disable) |
| `SIGIL_PI_REDACT_INPUT_MESSAGES` | `redaction.redactInputMessages` |
| `SIGIL_PI_REDACT_EMAIL_ADDRESSES` | `redaction.redactEmailAddresses` |

## What gets sent

Every generation always carries model, token usage (input/output/cache), cost, stop reason, tool names and durations, conversation ID, and turn timing. Message content depends on `contentCapture`:

| Mode | Adds |
|------|------|
| `metadata_only` (default) | nothing — content is stripped by the SDK |
| `no_tool_content` | assistant text and thinking |
| `full` | assistant text, thinking, tool call arguments, tool results |

When `otlp` is configured, the SDK additionally exports `gen_ai.client.operation.duration`, `gen_ai.client.token.usage`, and `gen_ai.client.tool_calls_per_operation` histograms (provider/model/agent labels) plus one trace span per generation. The plugin sets the OTel resource `service.name` to `sigil-pi` for both metrics and traces.
