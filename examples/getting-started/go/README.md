# Getting Started — Go

Makes an OpenAI chat completion and records the generation to Grafana Cloud AI Observability.

## Setup

```bash
cd examples/getting-started/go
# Set OPENAI_API_KEY, GRAFANA_INSTANCE_ID, GRAFANA_CLOUD_TOKEN, SIGIL_ENDPOINT
# See the SDK README for where to find each value.
go mod tidy
```

## Run

```bash
go run .
```

You should see the LLM response printed, followed by `Done`. Open the AI Observability plugin in your Grafana Cloud stack to see the recorded generation.
