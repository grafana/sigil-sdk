# Getting Started - TypeScript + Strands Agents

Runs a Strands TypeScript agent and records model/tool activity to Sigil Cloud.

## Setup

```bash
cd examples/getting-started/typescript-strands
npm install
cp .env.example .env
```

Configure Sigil, OpenTelemetry, and OpenAI from your Grafana Cloud stack:

```bash
SIGIL_EXPORT_PROTOCOL=http
SIGIL_ENDPOINT=https://sigil-prod-<region>.grafana.net/api/v1/generations:export
GRAFANA_INSTANCE_ID=...
GRAFANA_CLOUD_TOKEN=...
SIGIL_CONVERSATION_ID=sigil-strands-demo
OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp-gateway-prod-<region>.grafana.net/otlp
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Basic <base64 of GRAFANA_INSTANCE_ID:GRAFANA_CLOUD_TOKEN>"
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

You should see the agent response printed, followed by `Done`. Open Sigil in your Grafana Cloud stack to inspect the recorded generation and tool execution.
