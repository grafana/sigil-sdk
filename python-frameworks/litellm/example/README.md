# LiteLLM Proxy + Sigil Example

Runs a LiteLLM proxy with the Sigil callback handler, exporting generations to a local Sigil instance.

## Prerequisites

- Local Sigil stack running (`mise run up` or `docker compose --profile core up` from the repo root)
- At least one LLM API key (`OPENAI_API_KEY` or `ANTHROPIC_API_KEY`)

## Start the proxy

```bash
cd sdks/python-frameworks/litellm/example
OPENAI_API_KEY=sk-... docker compose up --build
```

The proxy starts on `http://localhost:4000` and exports generations to Sigil at `localhost:8080`.

## Make a request

```bash
curlie POST http://localhost:4000/chat/completions \
  model=gpt-4o-mini \
  messages:='[{"role":"user","content":"What is 2+2?"}]'
```

Or with streaming:

```bash
curlie POST http://localhost:4000/chat/completions \
  model=gpt-4o-mini \
  messages:='[{"role":"user","content":"Give me three reliability tips."}]' \
  stream:=true
```

## Verify in Sigil

Open `http://localhost:3000/a/grafana-sigil-app/conversations`. Generations appear with:

- `agent_name`: `litellm-proxy-integration-test`
- `sigil.framework.name`: `litellm`
- `provider`: `openai` (or whichever model you called)

## Configuration

`config.yaml` defines the available models. Add more by following the [LiteLLM model list format](https://docs.litellm.ai/docs/proxy/configs).

To point at a different Sigil instance:

```bash
SIGIL_ENDPOINT=http://your-sigil:8080/api/v1/generations:export docker compose up --build
```
