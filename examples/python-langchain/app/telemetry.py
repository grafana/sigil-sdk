"""OpenTelemetry bootstrap.

Sets up a TracerProvider + MeterProvider that export to an OTLP gRPC
target (Sigil's OTLP ingest on :4317 by default) and registers them
globally. Standard OTel — nothing Sigil-specific lives here.
"""

from __future__ import annotations

import os
from dataclasses import dataclass

from opentelemetry import metrics, trace
from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor


@dataclass
class OpenTelemetry:
    tracer_provider: TracerProvider
    meter_provider: MeterProvider

    def shutdown(self) -> None:
        self.tracer_provider.shutdown()
        self.meter_provider.shutdown()


def _env(name: str, default: str) -> str:
    value = os.getenv(name, default).strip()
    return value or default


def setup_opentelemetry() -> OpenTelemetry:
    service_name = _env("OTEL_SERVICE_NAME", "sigil-langchain-weather-example")
    otlp_endpoint = _env("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")
    otlp_insecure = _env("OTEL_EXPORTER_OTLP_INSECURE", "true").lower() == "true"

    resource = Resource.create({"service.name": service_name})

    tracer_provider = TracerProvider(resource=resource)
    tracer_provider.add_span_processor(
        BatchSpanProcessor(
            OTLPSpanExporter(endpoint=otlp_endpoint, insecure=otlp_insecure)
        )
    )
    trace.set_tracer_provider(tracer_provider)

    meter_provider = MeterProvider(
        resource=resource,
        metric_readers=[
            PeriodicExportingMetricReader(
                OTLPMetricExporter(endpoint=otlp_endpoint, insecure=otlp_insecure)
            )
        ],
    )
    metrics.set_meter_provider(meter_provider)

    return OpenTelemetry(
        tracer_provider=tracer_provider,
        meter_provider=meter_provider,
    )
