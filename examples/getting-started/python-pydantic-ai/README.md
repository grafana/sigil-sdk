# Getting Started — Python + Pydantic AI

Runs a Pydantic AI agent and records the generation to Grafana Cloud AI Observability via the `sigil-sdk-pydantic-ai` capability.

## Setup

```bash
cd examples/getting-started/python-pydantic-ai
cp .env.example .env
# Fill in ANTHROPIC_API_KEY, SIGIL_ENDPOINT, SIGIL_AUTH_TENANT_ID, SIGIL_AUTH_TOKEN.
# See the Grafana Cloud AI Observability getting started docs for where to find each value:
# https://grafana.com/docs/grafana-cloud/machine-learning/ai-observability/get-started/grafana-cloud/
```

```bash
pip install -r requirements.txt
```

## Run

```bash
python main.py
```

When the LLM response prints, followed by `Done`, open the AI Observability plugin in your Grafana Cloud stack to view the recorded generation, then check your Grafana Cloud Traces and Metrics datasources for SDK-emitted spans and metrics.
