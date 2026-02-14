"""OpenTelemetry trace + metrics runtime wiring."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Callable
from urllib.parse import urlparse

from opentelemetry import metrics, trace
from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter as OTLPGRPCMetricExporter
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter as OTLPGRPCSpanExporter
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter as OTLPHTTPMetricExporter
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter as OTLPHTTPSpanExporter
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

from .config import TraceConfig


INSTRUMENTATION_NAME = "github.com/grafana/sigil/sdks/python"


@dataclass(slots=True)
class TraceRuntime:
    """Managed trace + metrics runtime resources."""

    tracer: trace.Tracer
    meter: metrics.Meter
    trace_provider: TracerProvider | None
    meter_provider: MeterProvider | None

    def flush(self) -> None:
        if self.trace_provider is not None:
            self.trace_provider.force_flush()
        if self.meter_provider is not None:
            self.meter_provider.force_flush()

    def shutdown(self) -> None:
        if self.trace_provider is not None:
            self.trace_provider.shutdown()
        if self.meter_provider is not None:
            self.meter_provider.shutdown()


def create_trace_runtime(config: TraceConfig, on_error: Callable[[str, Exception], None] | None = None) -> TraceRuntime:
    """Builds a tracer/meter runtime pair for the configured OTLP transport."""

    try:
        trace_exporter = _new_trace_exporter(config)
        trace_provider = TracerProvider()
        trace_provider.add_span_processor(BatchSpanProcessor(trace_exporter))

        metric_exporter = _new_metric_exporter(config)
        metric_reader = PeriodicExportingMetricReader(metric_exporter)
        meter_provider = MeterProvider(metric_readers=[metric_reader])

        return TraceRuntime(
            tracer=trace_provider.get_tracer(INSTRUMENTATION_NAME),
            meter=meter_provider.get_meter(INSTRUMENTATION_NAME),
            trace_provider=trace_provider,
            meter_provider=meter_provider,
        )
    except Exception as exc:  # noqa: BLE001
        if on_error is not None:
            on_error("sigil telemetry exporter init failed", exc)
        return TraceRuntime(
            tracer=trace.get_tracer(INSTRUMENTATION_NAME),
            meter=metrics.get_meter(INSTRUMENTATION_NAME),
            trace_provider=None,
            meter_provider=None,
        )


def _new_trace_exporter(config: TraceConfig):
    protocol = config.protocol.strip().lower()
    if protocol == "grpc":
        endpoint, implicit_insecure = _parse_endpoint(config.endpoint)
        insecure = config.insecure or implicit_insecure
        return OTLPGRPCSpanExporter(
            endpoint=endpoint,
            insecure=insecure,
            headers=dict(config.headers),
            timeout=10,
        )

    endpoint, implicit_insecure = _parse_endpoint(config.endpoint)
    if "://" not in endpoint:
        scheme = "http" if (config.insecure or implicit_insecure) else "https"
        endpoint = f"{scheme}://{endpoint}"
    if urlparse(endpoint).path in ("", "/"):
        endpoint = endpoint.rstrip("/") + "/v1/traces"

    return OTLPHTTPSpanExporter(
        endpoint=endpoint,
        headers=dict(config.headers),
        timeout=10,
    )


def _new_metric_exporter(config: TraceConfig):
    protocol = config.protocol.strip().lower()
    if protocol == "grpc":
        endpoint, implicit_insecure = _parse_endpoint(config.endpoint)
        insecure = config.insecure or implicit_insecure
        return OTLPGRPCMetricExporter(
            endpoint=endpoint,
            insecure=insecure,
            headers=dict(config.headers),
            timeout=10,
        )

    endpoint, implicit_insecure = _parse_endpoint(config.endpoint)
    if "://" not in endpoint:
        scheme = "http" if (config.insecure or implicit_insecure) else "https"
        endpoint = f"{scheme}://{endpoint}"

    parsed = urlparse(endpoint)
    if parsed.path in ("", "/", "/v1/traces"):
        endpoint = endpoint.rstrip("/")
        endpoint += "/v1/metrics"

    return OTLPHTTPMetricExporter(
        endpoint=endpoint,
        headers=dict(config.headers),
        timeout=10,
    )


def _parse_endpoint(endpoint: str) -> tuple[str, bool]:
    trimmed = endpoint.strip()
    if not trimmed:
        raise ValueError("trace endpoint is required")

    if "://" not in trimmed:
        return trimmed, False

    parsed = urlparse(trimmed)
    if parsed.netloc == "":
        raise ValueError("trace endpoint host is required")

    if parsed.scheme == "grpc":
        return parsed.netloc, False

    return trimmed, parsed.scheme == "http"
