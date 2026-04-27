# sigil-cc: Claude Code Stop Hook for Sigil

A Claude Code [Stop hook](https://docs.anthropic.com/en/docs/claude-code/hooks) that reads each session's JSONL transcript and forwards Generation records to [Grafana AI Observability](https://grafana.com/docs/grafana-cloud/machine-learning/ai-observability/). Ships as a Claude Code plugin.

## Install via plugin

```bash
go install github.com/grafana/sigil-sdk/plugins/claude-code/cmd/sigil-cc@latest
```

Then, from inside Claude Code:

```
/plugin marketplace add grafana/sigil-sdk
/plugin install sigil-cc@grafana-sigil
```

The plugin registers the Stop hook for you; future hook updates ship with the plugin so you don't re-edit `settings.json`.

> `go install` drops the binary in `$GOBIN` (usually `~/go/bin`). That directory must be on your `$PATH` or the hook silently does nothing. Verify with `which sigil-cc`.

## Install manually

If you'd rather not use the plugin marketplace, install the binary the same way and wire the hook yourself in `~/.claude/settings.json`:

```json
{
  "hooks": {
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "sigil-cc",
            "timeout": 10000
          }
        ]
      }
    ]
  }
}
```

## Configure

`sigil-cc` reads its config from the environment:

| Variable | What it is |
|----------|-----------|
| `SIGIL_URL` | Your Sigil endpoint (e.g. `https://sigil.example.com`). |
| `SIGIL_USER` | Your Grafana Cloud stack/tenant ID. Sent as Basic auth user and `X-Scope-OrgID`. |
| `SIGIL_PASSWORD` | A `glc_…` token minted at Grafana Cloud → Access Policies ([docs](https://grafana.com/docs/grafana-cloud/account-management/authentication-and-permissions/access-policies/)). The token needs the appropriate Sigil scope. |
| `SIGIL_OTEL_ENDPOINT` | OTLP HTTP endpoint for metrics and traces (e.g. `https://otlp-gateway.grafana.net/otlp`). Technically optional, but without it you only get generations, no spans or metrics. |
| `SIGIL_CONTENT_CAPTURE_MODE` | no | Content capture mode: `full`, `metadata_only`, `no_tool_content` (default: `metadata_only`) |

These don't have to live in `settings.json`. `sigil-cc` reads from its process environment, so anything that's in Claude Code's environment when the hook fires works. Pick whichever fits your secret-management style.

### Shell environment (recommended)

Keeps tokens out of any global config file.

```bash
export SIGIL_URL=https://sigil.example.com
export SIGIL_USER=123456
export SIGIL_PASSWORD=glc_...
export SIGIL_OTEL_ENDPOINT=https://otlp-gateway.grafana.net/otlp
export SIGIL_CONTENT_CAPTURE_MODE=full
claude
```

### `~/.claude/settings.json` `env` block

Persistent across all Claude Code sessions.

```json
{
  "env": {
    "SIGIL_URL": "https://sigil.example.com",
    "SIGIL_OTEL_ENDPOINT": "https://otel.example.com",
    "SIGIL_USER": "your-tenant-id",
    "SIGIL_PASSWORD": "glc_...",
    "SIGIL_CONTENT_CAPTURE_MODE": "full"
  }
}
```

## Verify it worked

Run any single turn:

```bash
claude  # ask it anything, then exit
```

Then either:

- Check the Sigil UI. A new generation should appear.
- Or enable debug logging and tail the log:

  ```bash
  export SIGIL_DEBUG=true
  claude  # one turn
  tail -f ~/.claude/state/sigil-cc.log
  ```

If nothing appears, the most common causes are: `sigil-cc` not on `$PATH`, missing `SIGIL_URL`/`SIGIL_USER`/`SIGIL_PASSWORD`, or a token without the right scope.

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `SIGIL_URL` | yes | Sigil endpoint |
| `SIGIL_USER` | yes | Basic auth username (also `X-Scope-OrgID`) |
| `SIGIL_PASSWORD` | yes | Basic auth password |
| `SIGIL_OTEL_ENDPOINT` | recommended | OTLP HTTP endpoint for metrics + traces (e.g. `https://otlp-gateway.grafana.net/otlp` or `host:4318`). Without it you only get generations (no spans or metrics). |
| `SIGIL_OTEL_USER` | no | OTLP auth username (defaults to `SIGIL_USER`) |
| `SIGIL_OTEL_PASSWORD` | no | OTLP auth password (defaults to `SIGIL_PASSWORD`) |
| `SIGIL_OTEL_INSECURE` | no | `true` to disable TLS (default: TLS enabled) |
| `SIGIL_CONTENT_CAPTURE_MODE` | no | Content capture mode: `full`, `metadata_only`, `no_tool_content` (default: `metadata_only`) |
| `SIGIL_EXTRA_TAGS` | no | Comma-separated `key=value` tags added to every generation (e.g. `account=work,env=dev`). Built-in tags (`git.branch`, `cwd`, `entrypoint`, `subagent`) take precedence on collision. |
| `SIGIL_USER_ID` | no | Explicit override for the per-generation user id. When set to a non-whitespace value it wins over `~/.claude.json` and ignores `SIGIL_USER_ID_SOURCE`. |
| `SIGIL_USER_ID_SOURCE` | no | Field to read from `~/.claude.json` when `SIGIL_USER_ID` is unset: `email` (default, uses `oauthAccount.emailAddress`) or `accountUuid` (uses `oauthAccount.accountUuid`). Unknown values fall back to `email`. |
| `SIGIL_DEBUG` | no | Set to `true` to log to `~/.claude/state/sigil-cc.log` (otherwise silent). |

## How It Works

1. Claude Code fires the Stop hook after each turn, passing `{session_id, transcript_path}` on stdin
2. `sigil-cc` loads the byte offset from the last run (or 0 for first run)
3. Reads new JSONL lines from the transcript starting at that offset
4. Maps assistant messages with token usage to Sigil Generation records
5. Sends via the sigil-sdk HTTP client
6. On successful flush, saves the new offset for next invocation

Each assistant API response becomes one Generation with model, tokens, tools, timestamps, tags, and conversation title. The hook always exits 0; telemetry is best-effort.

## Content Capture

Content capture is controlled by `SIGIL_CONTENT_CAPTURE_MODE` (default: `metadata_only`):

| Mode | What's sent |
|------|-------------|
| `metadata_only` | Model, tokens, tool names, timestamps, tags. All text content stripped by the SDK. |
| `full` | Full conversation content with automatic secret redaction (see below). |
| `no_tool_content` | Full generation content but tool execution arguments/results excluded from spans. |

When content is included (`full` or `no_tool_content`), automatic redaction is applied:

- User prompts: Tier 1 redaction (known token formats)
- Assistant text: Tier 1 redaction (known token formats)
- Tool inputs/outputs: Tier 1 + Tier 2 redaction (tokens + env-file heuristics)
- Thinking blocks: omitted (noted in metadata)

## Development

```bash
cd plugins/claude-code
go test ./...
go build -o sigil-cc ./cmd/sigil-cc
```

Manual test:

```bash
echo '{"session_id":"test","transcript_path":"/path/to/session.jsonl"}' | \
  SIGIL_URL=https://sigil.example.com SIGIL_USER=123 SIGIL_PASSWORD=glc_... \
  ./sigil-cc
```
