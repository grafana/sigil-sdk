# Getting Started - Python + Strands Agents

Runs a Strands agent and records model/tool activity to Sigil Cloud.

## Setup

```bash
cd examples/getting-started/python-strands
python -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
cp .env.example .env
```

Configure Sigil and OTel endpoints from your Grafana Cloud stack. See the [Grafana Cloud AI Observability getting started docs](https://grafana.com/docs/grafana-cloud/machine-learning/ai-observability/get-started/grafana-cloud/) for where to find each value:

```bash
SIGIL_PROTOCOL=http
SIGIL_AUTH_MODE=basic
SIGIL_ENDPOINT=https://sigil-prod-<region>.grafana.net
SIGIL_AUTH_TENANT_ID=...
SIGIL_AUTH_TOKEN=...
SIGIL_CONVERSATION_ID=sigil-strands-demo
OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp-gateway-prod-<region>.grafana.net/otlp
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Basic <base64 of OTLP_INSTANCE_ID:SIGIL_AUTH_TOKEN>"
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_METRIC_EXPORT_INTERVAL_MILLIS=1000
OTEL_SERVICE_NAME=sigil-strands-example
STRANDS_MODEL_PROVIDER=openai
OPENAI_MODEL=gpt-4o-mini
OPENAI_API_KEY=...
```

The example also configures an OpenTelemetry `MeterProvider` and passes its meter
to the Sigil client. Generations go to `SIGIL_ENDPOINT`; SDK metrics go to
`OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`, or to `OTEL_EXPORTER_OTLP_ENDPOINT` with
`/v1/metrics` appended when the metrics-specific endpoint is unset.

Strands itself defaults to Amazon Bedrock when no model is provided. This example provides an explicit OpenAI model so you do not need AWS credentials. To try Bedrock instead, set `STRANDS_MODEL_PROVIDER=bedrock` and configure your AWS credentials.

## Run

```bash
python main.py
```

When the agent response prints, followed by `Done`, open Sigil in your Grafana Cloud stack to inspect the recorded generation.
