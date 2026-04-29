"""Minimal local Sigil example using Strands Agents."""

from __future__ import annotations

import os

from dotenv import load_dotenv
from opentelemetry import metrics
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from sigil_sdk import Client, ClientConfig, GenerationExportConfig
from sigil_sdk_strands import with_sigil_strands_hooks
from strands import Agent, tool
from strands.models.openai import OpenAIModel

load_dotenv()


@tool
def add_numbers(left: int, right: int) -> int:
    """Add two integers."""
    return left + right


def env(name: str, default: str) -> str:
    value = os.getenv(name, default).strip()
    return value or default


def otlp_metrics_endpoint() -> str:
    endpoint = os.getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "").strip()
    if endpoint:
        return endpoint

    endpoint = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "").strip()
    if not endpoint:
        return "http://localhost:4318/v1/metrics"
    if endpoint.endswith("/v1/metrics"):
        return endpoint
    return f"{endpoint.rstrip('/')}/v1/metrics"


def setup_metrics() -> MeterProvider:
    resource = Resource.create({"service.name": env("OTEL_SERVICE_NAME", "sigil-strands-example")})
    exporter = OTLPMetricExporter(
        endpoint=otlp_metrics_endpoint(),
    )
    reader = PeriodicExportingMetricReader(
        exporter,
        export_interval_millis=int(env("OTEL_METRIC_EXPORT_INTERVAL_MILLIS", "1000")),
    )
    meter_provider = MeterProvider(resource=resource, metric_readers=[reader])
    metrics.set_meter_provider(meter_provider)
    return meter_provider


def create_model():
    provider = os.getenv("STRANDS_MODEL_PROVIDER", "openai").strip().lower()
    if provider == "bedrock":
        # Returning None lets Strands use its default BedrockModel.
        return None
    if provider != "openai":
        raise ValueError("STRANDS_MODEL_PROVIDER must be 'openai' or 'bedrock'.")
    if not os.getenv("OPENAI_API_KEY"):
        raise RuntimeError("OPENAI_API_KEY is required when STRANDS_MODEL_PROVIDER=openai.")
    return OpenAIModel(
        model_id=os.getenv("OPENAI_MODEL", "gpt-4o-mini"),
        params={"temperature": 0.2},
    )


meter_provider = setup_metrics()
sigil = Client(
    ClientConfig(
        generation_export=GenerationExportConfig(
            protocol=os.getenv("SIGIL_EXPORT_PROTOCOL", "grpc"),
            endpoint=os.getenv("SIGIL_ENDPOINT", "localhost:4317"),
            insecure=True,
        ),
        meter=meter_provider.get_meter("sigil-strands-example"),
    )
)

try:
    agent_config = with_sigil_strands_hooks(
        {
            "name": "local-strands-demo",
            "model": create_model(),
            "tools": [add_numbers],
            "system_prompt": "You are concise and show the final answer.",
        },
        client=sigil,
        provider_resolver="auto",
    )
    agent = Agent(**agent_config)

    result = agent(
        "Use the add_numbers tool to add 19 and 23, then answer in one sentence.",
        invocation_state={"conversation_id": env("SIGIL_CONVERSATION_ID", "local-sigil-strands-demo")},
    )

    print(result.message)
finally:
    sigil.shutdown()
    meter_provider.shutdown()

print("Done")
