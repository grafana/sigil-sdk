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

Configure Sigil and OTel endpoints from your Grafana Cloud stack. See the [Grafana Cloud AI Observability getting started docs](https://grafana.com/docs/grafana-cloud/machine-learning/ai-observability/get-started/grafana-cloud/) for where to find the Sigil API URL:

```bash
SIGIL_EXPORT_PROTOCOL=http
SIGIL_ENDPOINT=https://sigil-prod-<region>.grafana.net
GRAFANA_INSTANCE_ID=...
GRAFANA_CLOUD_TOKEN=...
SIGIL_CONVERSATION_ID=sigil-strands-demo
OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp-gateway-prod-<region>.grafana.net/otlp
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Basic <base64 of GRAFANA_INSTANCE_ID:GRAFANA_CLOUD_TOKEN>"
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

You should see the agent response printed, followed by `Done`. Open Sigil in your Grafana Cloud stack to inspect the recorded generation.
