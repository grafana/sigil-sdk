# Grafana Sigil SDK

Client SDKs for [Grafana Sigil](https://github.com/grafana/sigil) — AI observability for LLM applications.

## SDKs

| Language | Package | Path |
|----------|---------|------|
| Go | `github.com/grafana/sigil-sdk/go` | [`go/`](go/) |
| Python | `sigil-sdk` | [`python/`](python/) |
| TypeScript/JavaScript | `@grafana/sigil-sdk-js` | [`js/`](js/) |
| .NET/C# | `Grafana.Sigil` | [`dotnet/`](dotnet/) |
| Java | `com.grafana.sigil` | [`java/`](java/) |

## Provider Adapters

| Language | Providers | Path |
|----------|-----------|------|
| Go | Anthropic, OpenAI, Gemini | [`go-providers/`](go-providers/) |
| Python | Anthropic, OpenAI, Gemini | [`python-providers/`](python-providers/) |

## Framework Integrations

| Language | Frameworks | Path |
|----------|------------|------|
| Go | Google ADK | [`go-frameworks/`](go-frameworks/) |
| Python | LangChain, LangGraph, OpenAI Agents, LlamaIndex, Google ADK | [`python-frameworks/`](python-frameworks/) |

## Plugins

- [OpenCode](plugins/opencode/) — Sigil integration for OpenCode

## Proto

Vendored protobuf definitions used by SDKs live in [`proto/`](proto/).

## License

[Apache License 2.0](LICENSE)
