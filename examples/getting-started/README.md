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

Each example needs an **OpenAI API key** ([platform.openai.com/api-keys](https://platform.openai.com/api-keys)) and your **Sigil / Grafana Cloud credentials** (instance ID, API token, endpoint URL).

See the [credentials section in the SDK README](../../README.md#sigil--grafana-cloud-credentials) for where to find each value in your Grafana Cloud stack.

Copy the `.env.example` in each example directory and fill in the values, or export them in your shell.

## What to look for in Sigil

After running an example, open the AI Observability plugin in your Grafana Cloud stack. You should see:

- A new generation under the conversation ID used in the example.
- Model name, provider, token usage, and latency filled in.
- The input prompt and assistant response visible in the conversation drilldown.

## Next steps

- **Provider wrappers** — reduce boilerplate by using pre-built wrappers for [OpenAI](../../go-providers/openai/), [Anthropic](../../python-providers/anthropic/), and [Gemini](../../go-providers/gemini/).
- **Framework adapters** — instrument [LangChain](../../python-frameworks/langchain/), [Vercel AI SDK](../../js/docs/frameworks/vercel-ai-sdk.md), [Google ADK](../../go-frameworks/google-adk/), and more with a single line.
- **Full example app** — see [`examples/python-langchain/`](../python-langchain/) for a FastAPI service with LangChain agent + manual instrumentation side by side.
