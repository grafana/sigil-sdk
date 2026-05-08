# Getting Started - TypeScript + Strands Agents

Runs a Strands TypeScript agent and records model/tool activity to Sigil Cloud.

## Setup

```bash
cd examples/getting-started/typescript-strands
npm install
cp .env.example .env
```

Configure Sigil, OpenTelemetry, and OpenAI from your Grafana Cloud stack. See the [Grafana Cloud AI Observability getting started docs](https://grafana.com/docs/grafana-cloud/machine-learning/ai-observability/get-started/grafana-cloud/) for where to find each value:

```bash
SIGIL_PROTOCOL=http
SIGIL_AUTH_MODE=basic
SIGIL_ENDPOINT=https://sigil-prod-<region>.grafana.net
SIGIL_AUTH_TENANT_ID=...
SIGIL_AUTH_TOKEN=...
SIGIL_CONVERSATION_ID=sigil-strands-demo
OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp-gateway-prod-<region>.grafana.net/otlp
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Basic <base64 of OTLP_INSTANCE_ID:SIGIL_AUTH_TOKEN>"
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_METRIC_EXPORT_INTERVAL_MILLIS=1000
OTEL_SERVICE_NAME=sigil-strands-typescript-example
OPENAI_MODEL=gpt-4o-mini
OPENAI_API_KEY=...
```

The example configures OpenTelemetry tracing and metrics. Generations go to
`SIGIL_ENDPOINT`; SDK metrics go through the OpenTelemetry HTTP exporter.

## Run

```bash
npm start
```

When the agent response prints, followed by `Done`, open Sigil in your Grafana Cloud stack to inspect the recorded generation and tool execution.
