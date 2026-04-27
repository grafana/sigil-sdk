# Getting Started with Sigil

Minimal, self-contained examples that make a real LLM call and record the generation to Grafana Sigil. Pick your language and you should be up and running in under five minutes.

| Language | Directory | LLM provider |
|----------|-----------|--------------|
| Python | [`python/`](python/) | OpenAI |
| TypeScript | [`typescript/`](typescript/) | OpenAI |
| Go | [`go/`](go/) | OpenAI |

Each example:

1. Creates an OpenAI client and sends a chat completion request.
2. Creates a Sigil client authenticated against Grafana Cloud.
3. Records the generation (input, output, token usage, model metadata).
4. Shuts down cleanly.

## Credentials

You need two sets of credentials: one for your LLM provider and one for Sigil.

### OpenAI

| Variable | What it is | Where to find it |
|----------|-----------|-----------------|
| `OPENAI_API_KEY` | Your OpenAI API key | [platform.openai.com/api-keys](https://platform.openai.com/api-keys) |

### Sigil / Grafana Cloud

| Variable | What it is | Where to find it |
|----------|-----------|-----------------|
| `GRAFANA_INSTANCE_ID` | Your Grafana Cloud stack's numeric instance ID. Used as the tenant ID and the basic-auth username. | In your Grafana Cloud stack: **Connections → AI Observability plugin → Connection tab**, shown under **Instance ID**. Also visible in the Cloud Portal under your stack details. |
| `GRAFANA_CLOUD_TOKEN` | A Grafana Cloud API token (starts with `glc_`). Used as the basic-auth password. | Create one in your Grafana Cloud stack: **Administration → Cloud Access Policies** (or via the [Cloud Portal](https://grafana.com/orgs)). The token needs write permissions for AI Observability. See [Access Policies docs](https://grafana.com/docs/grafana-cloud/account-management/authentication-and-permissions/access-policies/). |
| `SIGIL_ENDPOINT` | The Sigil ingest endpoint for your region. | Shown in the **AI Observability plugin → Connection tab** under **Sigil API URL**. Append `/api/v1/generations:export`. Example: `https://sigil-prod-eu-west-3.grafana.net/api/v1/generations:export` |

### Setting the variables

Create a `.env` file in the example directory (see `.env.example` in each folder), or export them in your shell:

```bash
export OPENAI_API_KEY="sk-..."
export GRAFANA_INSTANCE_ID="123456"
export GRAFANA_CLOUD_TOKEN="glc_..."
export SIGIL_ENDPOINT="https://sigil-prod-<region>.grafana.net/api/v1/generations:export"
```

## What to look for in Sigil

After running an example, open the AI Observability plugin in your Grafana Cloud stack. You should see:

- A new generation under the conversation ID used in the example.
- Model name, provider, token usage, and latency filled in.
- The input prompt and assistant response visible in the conversation drilldown.

## Next steps

- **Provider wrappers** — reduce boilerplate by using pre-built wrappers for [OpenAI](../../go-providers/openai/), [Anthropic](../../python-providers/anthropic/), and [Gemini](../../go-providers/gemini/).
- **Framework adapters** — instrument [LangChain](../../python-frameworks/langchain/), [Vercel AI SDK](../../js/docs/frameworks/vercel-ai-sdk.md), [Google ADK](../../go-frameworks/google-adk/), and more with a single line.
- **Full example app** — see [`examples/python-langchain/`](../python-langchain/) for a FastAPI service with LangChain agent + manual instrumentation side by side.
