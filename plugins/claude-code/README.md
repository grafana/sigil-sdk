# sigil-cc — Claude Code Stop Hook for Sigil

A Claude Code [Stop hook](https://docs.anthropic.com/en/docs/claude-code/hooks) that reads JSONL session transcripts and sends Generation records to [Grafana Sigil](https://github.com/grafana/sigil).

Replaces the sigil-cc OTLP proxy with a simpler, more reliable approach: read the transcript directly instead of intercepting OTLP telemetry.

## Install

### Plugin (recommended)

Install the binary, then add the plugin:

```bash
go install github.com/grafana/sigil-sdk/plugins/claude-code/cmd/sigil-cc@latest
```

From within Claude Code:

```
/plugin marketplace add grafana/sigil-sdk
/plugin install sigil-cc@grafana-sigil
```

The Stop hook registers automatically. Set the required environment variables in `~/.claude/settings.json`:

```json
{
  "env": {
    "SIGIL_URL": "https://sigil.example.com",
    "SIGIL_USER": "your-tenant-id",
    "SIGIL_PASSWORD": "glc_..."
  }
}
```

### Manual

Install the binary and configure everything in `~/.claude/settings.json`:

```bash
go install github.com/grafana/sigil-sdk/plugins/claude-code/cmd/sigil-cc@latest
```

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
  },
  "env": {
    "SIGIL_URL": "https://sigil.example.com",
    "SIGIL_USER": "your-tenant-id",
    "SIGIL_PASSWORD": "glc_...",
    "SIGIL_CONTENT_CAPTURE_MODE": "metadata_only"
  }
}
```

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `SIGIL_URL` | yes | Sigil endpoint |
| `SIGIL_USER` | yes | Basic auth username (also `X-Scope-OrgID`) |
| `SIGIL_PASSWORD` | yes | Basic auth password |
| `SIGIL_CONTENT_CAPTURE_MODE` | no | Content capture mode: `full`, `metadata_only`, `no_tool_content` (default: `metadata_only`) |
| `SIGIL_EXTRA_TAGS` | no | Comma-separated `key=value` tags added to every generation (e.g. `account=work,env=dev`). Built-in tags (`git.branch`, `cwd`, `entrypoint`, `subagent`) take precedence on collision. |
| `SIGIL_OTEL_ENDPOINT` | no | OTLP HTTP endpoint for metrics + traces (e.g. `https://otlp-gateway.grafana.net/otlp` or `host:4318`) |
| `SIGIL_OTEL_USER` | no | OTLP auth username (defaults to `SIGIL_USER`) |
| `SIGIL_OTEL_PASSWORD` | no | OTLP auth password (defaults to `SIGIL_PASSWORD`) |
| `SIGIL_OTEL_INSECURE` | no | `true` to disable TLS (default: TLS enabled) |

## How It Works

1. Claude Code fires the Stop hook after each turn, passing `{session_id, transcript_path}` on stdin
2. `sigil-cc` loads the byte offset from the last run (or 0 for first run)
3. Reads new JSONL lines from the transcript starting at that offset
4. Maps assistant messages with token usage to Sigil Generation records
5. Sends via the sigil-sdk HTTP client
6. On successful flush, saves the new offset for next invocation

Each assistant API response becomes one Generation with model, tokens, tools, timestamps, tags, and conversation title. The hook always exits 0 — telemetry is best-effort.

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
