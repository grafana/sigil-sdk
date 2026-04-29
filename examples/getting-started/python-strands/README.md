# Getting Started — Python + Strands Agents

Runs a Strands agent and records model/tool activity to local Sigil.

## Setup

```bash
cd examples/getting-started/python-strands
python -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
cp .env.example .env
```

By default this points at local Sigil over gRPC and uses Strands' OpenAI model provider:

```bash
SIGIL_EXPORT_PROTOCOL=grpc
SIGIL_ENDPOINT=localhost:4317
SIGIL_CONVERSATION_ID=local-sigil-strands-demo
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://localhost:4318/v1/metrics
OTEL_METRIC_EXPORT_INTERVAL_MILLIS=1000
OTEL_SERVICE_NAME=sigil-strands-example
STRANDS_MODEL_PROVIDER=openai
OPENAI_MODEL=gpt-4o-mini
OPENAI_API_KEY=...
```

The example also configures an OpenTelemetry `MeterProvider` and passes its meter
to the Sigil client. Generations go to local Sigil on `localhost:4317`; SDK
metrics go through the local Sigil dev stack's Alloy OTLP/HTTP endpoint on
`localhost:4318/v1/metrics`. If `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` is unset,
`main.py` defaults to that local Alloy endpoint.

Strands itself defaults to Amazon Bedrock when no model is provided. This example provides an explicit OpenAI model so you do not need AWS credentials. To try Bedrock instead, set `STRANDS_MODEL_PROVIDER=bedrock` and configure your AWS credentials.

## Run

```bash
python main.py
```

You should see the agent response printed, followed by `Done`. Open local Sigil to inspect the recorded generation.
