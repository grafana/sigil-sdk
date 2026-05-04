# sigil-cursor — Cursor → Grafana Sigil plugin

Forwards Cursor agent generations (prompts, assistant replies, tool calls,
token usage) to Grafana Sigil for AI observability.

## Install

Cursor's hook runtime does not manage dependencies. Install the binary
yourself:

```sh
go install github.com/grafana/sigil-sdk/plugins/cursor/cmd/sigil-cursor@latest
```

This places `sigil-cursor` in `$GOBIN` (default `$HOME/go/bin`). The
`scripts/run.sh` shim probes `$SIGIL_CURSOR_BIN`, `$HOME/go/bin`,
`/usr/local/bin`, `$HOME/.local/bin`, and `$PATH` to find it under Cursor's
stripped hook environment.

Then register the plugin in Cursor:

```
/add-plugin grafana/sigil-sdk
```

## Configure

Configuration is read from environment variables and from a dotenv file at
`${XDG_CONFIG_HOME:-$HOME/.config}/sigil-cursor/config.env`. OS env wins
per-key; the file fills in unset keys. Cursor runs hooks under a clean
process environment that does not inherit your shell profile, so the file
is the reliable place to put credentials.

| Variable | Required | Purpose |
|---|---|---|
| `SIGIL_URL` | yes | Sigil endpoint root, e.g. `https://sigil.grafana.net` |
| `SIGIL_USER` | yes | Basic-auth user / tenant id |
| `SIGIL_PASSWORD` | yes | Basic-auth token |
| `SIGIL_CONTENT_CAPTURE_MODE` | no | `full`, `no_tool_content`, or `metadata_only` (default) |
| `SIGIL_EXTRA_TAGS` | no | Comma-separated `k=v` pairs added to every generation |
| `SIGIL_USER_ID` | no | Override the user id (default: `user_email` from Cursor's payload) |
| `SIGIL_DEBUG` | no | `true` writes a log to `$XDG_STATE_HOME/sigil-cursor/sigil-cursor.log` |
| `SIGIL_OTEL_ENDPOINT` | no | OTLP HTTP endpoint for the SDK's own self-telemetry |
| `SIGIL_OTEL_USER` | no | OTLP basic-auth user (defaults to `SIGIL_USER`) |
| `SIGIL_OTEL_PASSWORD` | no | OTLP basic-auth password (defaults to `SIGIL_PASSWORD`) |
| `SIGIL_OTEL_INSECURE` | no | `true` to disable TLS for the OTLP endpoint |
| `SIGIL_CURSOR_BIN` | no | Override the binary path used by `scripts/run.sh` |

Without `SIGIL_URL` / `SIGIL_USER` / `SIGIL_PASSWORD`, hooks still run but
nothing is emitted to Sigil.

### Where to find these values

- `SIGIL_URL` — Grafana Cloud → AI Observability → Configuration, **API
  URL** field.
- `SIGIL_USER` — Grafana Cloud → AI Observability → Configuration,
  **Instance ID** field. Numeric Grafana Cloud stack id.
- `SIGIL_PASSWORD` — a Grafana Cloud access policy token (grafana.com →
  Security → Access Policies), with the realm set to the stack's region.
  Required scope: `sigil:write`. Add `metrics:write`, `metrics:import`,
  `traces:write`, and `logs:write` when also setting
  `SIGIL_OTEL_ENDPOINT` — one token can cover both channels.
- `SIGIL_OTEL_ENDPOINT` — Grafana Cloud → your stack → OpenTelemetry
  card, **OTLP Endpoint URL**. The OTLP gateway region is tied to the
  stack's region, which can differ from the Sigil region.

## Content capture

| Mode | User prompt | Assistant text | Thinking | Tool args / results |
|---|---|---|---|---|
| `full` | included | included | presence-only | included |
| `no_tool_content` | included | included | presence-only | structure kept, content stripped |
| `metadata_only` (default) | stripped | stripped | presence-only | stripped |

Thinking content is never exported regardless of mode — only
`thinking_enabled: true` is set on the Generation.

## Built-in tags

The plugin sets these tags automatically; user values from
`SIGIL_EXTRA_TAGS` lose to built-ins on key collision:

- `git.branch` — best-effort branch resolution. Walks up to 6 parent
  directories from the workspace root looking for a `.git` entry; follows
  `gitdir: <path>` indirection used by worktrees and submodules; reads
  HEAD from the resolved git directory. Returns the branch name on a
  symbolic ref, the first 12 hex chars on detached HEAD, or nothing on
  failure.
- `cwd` — first `cwd` observed in `postToolUse`, falling back to the first
  workspace root.
- `subagent` — `"true"` for background-agent runs.
- `entrypoint` — never auto-populated. Set via `SIGIL_EXTRA_TAGS` if you
  want a value here. Cursor's `composer_mode` is **not** auto-mapped; the
  equivalence has not been validated.

## How it works

Per-generation state is buffered on disk under
`$XDG_STATE_HOME/sigil-cursor/<conversation>/gen-<generation>.json` (mode
`0600`) until the `stop` event flushes it to Sigil. `sessionEnd` removes
any stranded fragments left by crashed turns.

## Development

```sh
cd plugins/cursor
make build      # → ./sigil-cursor
make test       # go test ./...
make lint       # golangci-lint run ./...
make install    # go install ./cmd/sigil-cursor
```
