# Grafana Sigil SDK

<p align="center">
  <img src="./assets/readme/sigil-tri-shot.svg" alt="Sigil landing, analytics, and conversation explore views" width="100%" />
</p>

Sigil is an AI observability product from Grafana for teams running agents in production.

Instrument once with a thin OpenTelemetry-native SDK, then use Sigil to see what your agents are doing, what they cost, how quality is changing, and which conversations need attention.

## What You Get

- **Simple onboarding.** Sigil is a thin SDK layer on top of OpenTelemetry and the OTel GenAI semantic conventions, with helpers for common providers and frameworks. If you already have OTel, setup is small enough to do by hand or with coding assistants such as Claude Code or Cursor.
- **A single pane of glass for your agents.** See activity, latency, errors, token usage, cost, cache behavior, and quality in one place with filters for time range, provider, model, agent, and labels.
- **Conversation drilldown when something looks off.** Open any conversation to inspect the full thread, tool calls, traces, scores, ratings, annotations, token usage, and cost breakdowns.
- **Agent catalog and version history.** Sigil automatically groups agents, tracks versions, shows prompt and tool footprints, surfaces usage and cost per version, and helps you compare how an agent changes over time.
- **Actionable suggestions, not just dashboards.** Built-in insight bars flag anomalies and optimization opportunities around cost, cache, errors, and performance, and agent detail can rate a version's prompt/tool setup and suggest improvements.
- **Online evaluation on live traffic.** Score production generations continuously so you can monitor quality, catch regressions, and avoid manually reading every conversation.

## Why Sigil

- **OpenTelemetry-native**: follows the OTel GenAI semantic conventions, emits standard traces and metrics over OTLP, and works with existing OTel pipelines.
- **Generation-first**: normalized generation ingest lets Sigil correlate conversations, tool executions, traces, costs, and scores.
- **Version-aware agents**: prompt and tool changes become queryable agent versions, even when producers do not send a clean version string.
- **Built for production quality loops**: observability, agent understanding, ratings, annotations, and online evaluation live in the same workflow.

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

## Quick Examples

### TypeScript

```ts
import { SigilClient } from "@grafana/sigil-sdk-js";

const client = new SigilClient({
  generationExport: {
    protocol: "http",
    endpoint: "http://localhost:8080/api/v1/generations:export",
    auth: { mode: "tenant", tenantId: "dev-tenant" },
  },
});

// Configure OTEL exporters (traces/metrics) in your app OTEL setup.

await client.startGeneration(
  {
    conversationId: "conv-1",
    model: { provider: "openai", name: "gpt-5" },
  },
  async (recorder) => {
    recorder.setResult({
      output: [{ role: "assistant", content: "Hello from Sigil" }],
    });
  }
);

await client.shutdown();
```

### Go

```go
cfg := sigil.DefaultConfig()
cfg.GenerationExport.Protocol = sigil.GenerationExportProtocolHTTP
cfg.GenerationExport.Endpoint = "http://localhost:8080/api/v1/generations:export"
cfg.GenerationExport.Auth = sigil.AuthConfig{
	Mode:     sigil.ExportAuthModeTenant,
	TenantID: "dev-tenant",
}

client := sigil.NewClient(cfg)
defer func() { _ = client.Shutdown(context.Background()) }()

ctx, rec := client.StartGeneration(context.Background(), sigil.GenerationStart{
	ConversationID: "conv-1",
	Model:          sigil.ModelRef{Provider: "openai", Name: "gpt-5"},
})
defer rec.End()

rec.SetResult(sigil.Generation{
	Output: []sigil.Message{sigil.AssistantTextMessage("Hello from Sigil")},
}, nil)
```

### Python

```python
from sigil_sdk import Client, ClientConfig, GenerationStart, ModelRef, assistant_text_message

client = Client(
    ClientConfig(
        generation_export_endpoint="http://localhost:8080/api/v1/generations:export",
    )
)

with client.start_generation(
    GenerationStart(
        conversation_id="conv-1",
        model=ModelRef(provider="openai", name="gpt-5"),
    )
) as rec:
    rec.set_result(output=[assistant_text_message("Hello from Sigil")])

client.shutdown()
```

## Plugins

- [OpenCode](plugins/opencode/) — Sigil integration for OpenCode

## Proto

Vendored protobuf definitions used by SDKs live in [`proto/`](proto/).

## License

[Apache License 2.0](LICENSE)
