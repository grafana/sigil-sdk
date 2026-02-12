"""OpenTelemetry trace runtime wiring."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Callable
from urllib.parse import urlparse

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter as OTLPGRPCSpanExporter
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter as OTLPHTTPSpanExporter
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

from .config import TraceConfig


INSTRUMENTATION_NAME = "github.com/grafana/sigil/sdks/python"


@dataclass(slots=True)
class TraceRuntime:
    """Managed trace runtime resources."""

    tracer: trace.Tracer
    provider: TracerProvider | None

    def flush(self) -> None:
        if self.provider is not None:
            self.provider.force_flush()

    def shutdown(self) -> None:
        if self.provider is not None:
            self.provider.shutdown()


def create_trace_runtime(config: TraceConfig, on_error: Callable[[str, Exception], None] | None = None) -> TraceRuntime:
    """Builds a tracer/provider pair for the configured OTLP transport."""

    try:
        exporter = _new_exporter(config)
        provider = TracerProvider()
        provider.add_span_processor(BatchSpanProcessor(exporter))
        return TraceRuntime(tracer=provider.get_tracer(INSTRUMENTATION_NAME), provider=provider)
    except Exception as exc:  # noqa: BLE001
        if on_error is not None:
            on_error("sigil trace exporter init failed", exc)
        return TraceRuntime(tracer=trace.get_tracer(INSTRUMENTATION_NAME), provider=None)


def _new_exporter(config: TraceConfig):
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
