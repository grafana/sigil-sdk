# sigil-cc — Claude Code Stop Hook for Sigil

A Claude Code [Stop hook](https://docs.anthropic.com/en/docs/claude-code/hooks) that reads JSONL session transcripts and sends Generation records to [Grafana Sigil](https://github.com/grafana/sigil).

Replaces the sigil-cc OTLP proxy with a simpler, more reliable approach: read the transcript directly instead of intercepting OTLP telemetry.

## Install

```bash
go install github.com/grafana/sigil-sdk/plugins/claude-code/cmd/sigil-cc@latest
```

## Configure

Add to `~/.claude/settings.json`:

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
    "SIGIL_PASSWORD": "glc_..."
  }
}
```

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `SIGIL_URL` | yes | Sigil endpoint |
| `SIGIL_USER` | yes | Basic auth username (also `X-Scope-OrgID`) |
| `SIGIL_PASSWORD` | yes | Basic auth password |
| `SIGIL_CONTENT_CAPTURE` | no | `true` to include redacted conversation content (default: metadata-only) |
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

By default, only metadata is sent (model, tokens, tool names, timestamps). Set `SIGIL_CONTENT_CAPTURE=true` to include conversation content with automatic secret redaction:

- User prompts: sent as-is (user's own input)
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
