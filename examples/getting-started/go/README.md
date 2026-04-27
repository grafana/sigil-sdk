# Getting Started — Go

Makes an OpenAI chat completion and records the generation to Sigil.

## Setup

```bash
cd examples/getting-started/go
cp .env.example .env
# Fill in your credentials (see ../README.md for where to find each value)
```

```bash
source .env
go mod tidy
```

## Run

```bash
source .env && go run .
```

You should see the LLM response printed, followed by `Done`. Open the AI Observability plugin in your Grafana Cloud stack to see the recorded generation.
