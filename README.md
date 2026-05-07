# Grafana Sigil SDK

<p align="center">
  <img src="./assets/readme/sigil-tri-shot.svg" alt="Sigil landing, analytics, and conversation explore views" width="100%" />
</p>

[Grafana AI observability](https://grafana.com/docs/grafana-cloud/machine-learning/ai-observability/) is a product from Grafana for teams running agents in production.

Instrument once with a thin OpenTelemetry-native SDK, then use it to see what your agents are doing, what they cost, how quality is changing, and which conversations need attention.

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
| Java | Anthropic, OpenAI, Gemini | [`java/providers/`](java/providers/) |
| .NET | Anthropic, OpenAI, Gemini | [`dotnet/src/`](dotnet/src/) |
| TypeScript/JavaScript | Anthropic, OpenAI, Gemini | [`js/docs/providers/`](js/docs/providers/) |

## Framework Integrations

| Language | Frameworks | Path |
|----------|------------|------|
| Go | Google ADK | [`go-frameworks/`](go-frameworks/) |
| Python | LangChain, LangGraph, OpenAI Agents, LlamaIndex, Google ADK, Strands Agents, LiteLLM, Pydantic AI | [`python-frameworks/`](python-frameworks/) |
| Java | Google ADK | [`java/frameworks/`](java/frameworks/) |
| TypeScript/JavaScript | LangChain, LangGraph, OpenAI Agents, LlamaIndex, Google ADK, Strands, Vercel AI SDK | [`js/docs/frameworks/`](js/docs/frameworks/) |

## Quick Examples

### TypeScript

```ts
import { SigilClient } from "@grafana/sigil-sdk-js";

const client = new SigilClient({
  generationExport: {
    protocol: "http",
    endpoint: "http://localhost:8080",
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
cfg.GenerationExport.Endpoint = "http://localhost:8080"
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
from sigil_sdk import Client, ClientConfig, GenerationExportConfig, GenerationStart, ModelRef, assistant_text_message

client = Client(
    ClientConfig(
        generation_export=GenerationExportConfig(
            protocol="http",
            endpoint="http://localhost:8080",
        ),
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

## Instrument with AI Coding Agents

Let your AI coding assistant add instrumentation for you. We provide ready-to-use prompts for Cursor, Claude Code, and GitHub Copilot that tell the agent how to find your LLM calls and wrap them with the SDK.

**From the UI** — open the AI Observability plugin in your Grafana Cloud stack, go through the onboarding wizard, and pick your IDE. The prompt is generated for you with one click.

**From this repo** — copy the prompt file for your IDE into your project:

| IDE | Prompt file | Where to put it in your project |
|-----|------------|-------------------------------|
| Cursor | [`CLAUDE.md`](CLAUDE.md) | `.cursor/rules/sigil.mdc` (or paste into Cursor chat) |
| Claude Code | [`CLAUDE.md`](CLAUDE.md) | `CLAUDE.md` at your project root |
| GitHub Copilot | [`.github/copilot-instructions.md`](.github/copilot-instructions.md) | `.github/copilot-instructions.md` in your project |

Then ask your agent: *"Instrument this codebase with Grafana AI Observability"*.

## Getting Started Examples

Minimal, self-contained examples that make a real LLM call and record the generation to Grafana Cloud AI Observability.

| Language | Example |
|----------|---------|
| Python | [`examples/getting-started/python/`](examples/getting-started/python/) |
| Python + Pydantic AI | [`examples/getting-started/python-pydantic-ai/`](examples/getting-started/python-pydantic-ai/) |
| Python + Strands | [`examples/getting-started/python-strands/`](examples/getting-started/python-strands/) |
| TypeScript | [`examples/getting-started/typescript/`](examples/getting-started/typescript/) |
| TypeScript + Strands | [`examples/getting-started/typescript-strands/`](examples/getting-started/typescript-strands/) |
| Go | [`examples/getting-started/go/`](examples/getting-started/go/) |

### Grafana Cloud credentials

You need three values to connect. The endpoint and instance ID are visible in **AI Observability → Configuration** in your Grafana Cloud stack; see the [Grafana Cloud AI Observability getting started docs](https://grafana.com/docs/grafana-cloud/machine-learning/ai-observability/get-started/grafana-cloud/) for the full setup flow.

| What | Where to find it |
|------|-----------------|
| **Instance ID** — numeric stack ID, used as tenant ID and basic-auth username | Shown under **Instance ID** in AI Observability → Configuration. Also in the Cloud Portal under your stack details. |
| **API token** — starts with `glc_`, used as the basic-auth password | Create one via **Administration → Cloud Access Policies** ([docs](https://grafana.com/docs/grafana-cloud/account-management/authentication-and-permissions/access-policies/)). Scope it to your stack with write permissions for AI Observability. |
| **Endpoint URL** — the ingest URL for your region | Shown under **API URL** in AI Observability → Configuration. |

## Plugins

Drop-in integrations for coding agents. See [`plugins/`](plugins/) for details.

- [Claude Code](plugins/claude-code/)
- [OpenCode](plugins/opencode/)
- [Pi](plugins/pi/)

## Proto

Vendored protobuf definitions used by SDKs live in [`proto/`](proto/). See [`docs/development.md`](docs/development.md#regenerating-protobuf-stubs) for how to regenerate language stubs.

## License

[Apache License 2.0](LICENSE)
