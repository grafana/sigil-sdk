# Getting Started with AI Observability

Minimal, self-contained examples that make a real LLM call and record the generation to Grafana Cloud AI Observability. Pick your language and you should be up and running in under five minutes.

| Language | Directory | LLM provider |
|----------|-----------|--------------|
| Python | [`python/`](python/) | OpenAI |
| Python + Strands | [`python-strands/`](python-strands/) | OpenAI by default, Sigil Cloud |
| TypeScript | [`typescript/`](typescript/) | OpenAI |
| Go | [`go/`](go/) | OpenAI |

Each example:

1. Creates an OpenAI client and sends a chat completion request.
2. Creates an SDK client authenticated against Grafana Cloud.
3. Records the generation (input, output, token usage, model metadata).
4. Shuts down cleanly.

## Credentials

Each example needs an **OpenAI API key** ([platform.openai.com/api-keys](https://platform.openai.com/api-keys)) and your **Grafana Cloud credentials** (instance ID, API token, endpoint URL).

See the [credentials section in the SDK README](../../README.md#grafana-cloud-credentials) for where to find each value in your Grafana Cloud stack.

Set them as environment variables before running an example.

## What to look for

After running an example, open the AI Observability plugin in your Grafana Cloud stack. You should see:

- A new generation under the conversation ID used in the example.
- Model name, provider, token usage, and latency filled in.
- The input prompt and assistant response visible in the conversation drilldown.

## Next steps

- **Provider wrappers** — reduce boilerplate by using pre-built wrappers for [OpenAI](../../go-providers/openai/), [Anthropic](../../python-providers/anthropic/), and [Gemini](../../go-providers/gemini/).
- **Framework adapters** — instrument [LangChain](../../python-frameworks/langchain/), [Strands Agents](../../python-frameworks/strands/), [Vercel AI SDK](../../js/docs/frameworks/vercel-ai-sdk.md), [Google ADK](../../go-frameworks/google-adk/), and more with a single line.
- **Full example app** — see [`examples/python-langchain/`](../python-langchain/) for a FastAPI service with LangChain agent + manual instrumentation side by side.
