# Sigil plugins for coding agents

Plugins that send generations from coding agents to [Grafana AI Observability](https://grafana.com/docs/grafana-cloud/machine-learning/ai-observability/). They capture model, tool calls, traces, optionally full conversation content, and tokens/cost where the host agent exposes reliable usage data.

| Agent | Plugin | Status |
|-------|--------|--------|
| [Claude Code](https://docs.anthropic.com/en/docs/claude-code) | [`claude-code/`](claude-code/) | Available |
| [Codex](https://developers.openai.com/codex) | [`codex/`](codex/) | Experimental |
| [Cursor](https://cursor.com) | [`cursor/`](cursor/) | Available |
| [OpenCode](https://opencode.ai) | [`opencode/`](opencode/) | Available |
| [Pi](https://github.com/badlogic/pi) | [`pi/`](pi/) | Available |

Each plugin's README covers install, config, auth, and content-capture options.

Codex support is experimental because Codex hooks and plugin-provided lifecycle
config remain feature-flagged.
