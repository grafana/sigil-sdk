# Getting Started — Python + Pydantic AI

Runs a Pydantic AI agent and records the generation to Grafana Cloud AI Observability via the `sigil-sdk-pydantic-ai` capability.

## Setup

```bash
cd examples/getting-started/python-pydantic-ai
# Set ANTHROPIC_API_KEY, GRAFANA_INSTANCE_ID, GRAFANA_CLOUD_TOKEN, SIGIL_ENDPOINT
# See the SDK README for where to find each value.
```

```bash
pip install -r requirements.txt
```

## Run

```bash
python main.py
```

You should see the LLM response printed, followed by `Done`. Open the AI Observability plugin in your Grafana Cloud stack to see the recorded generation, and check your Grafana Cloud Traces and Metrics datasources for SDK-emitted spans and metrics.
