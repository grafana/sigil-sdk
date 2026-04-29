# Getting Started — TypeScript

Makes an OpenAI chat completion and records the generation to Grafana Cloud AI Observability.

## Setup

```bash
cd examples/getting-started/typescript
# Set OPENAI_API_KEY, GRAFANA_INSTANCE_ID, GRAFANA_CLOUD_TOKEN, SIGIL_ENDPOINT
# See the SDK README for where to find each value.
#
# For traces and metrics, set the OTLP endpoint.
# Option A — Direct to Cloud (get URL from Cloud portal → stack Details):
#   OTEL_EXPORTER_OTLP_ENDPOINT=https://<your-otlp-gateway-url>
#   OTEL_EXPORTER_OTLP_HEADERS="Authorization=Basic <base64(instance_id:cloud_api_token)>"
# Option B — Via local Alloy/collector:
#   OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
```

```bash
npm install
```

> The `@grafana/sigil-sdk-js` package is installed from the local monorepo via a `file:` reference. If you're working outside the monorepo, replace it with the published package once available.

## Run

```bash
npx tsx main.ts
```

You should see the LLM response printed, followed by `Done`. Open the AI Observability plugin in your Grafana Cloud stack to see the recorded generation, and check your Grafana Cloud Traces and Metrics datasources for SDK-emitted spans and metrics.
