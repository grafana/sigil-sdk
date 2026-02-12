"""Runtime configuration for the Sigil Python SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timedelta
import logging
import time
from typing import Callable, Optional

from opentelemetry.trace import Tracer

from .exporters.base import GenerationExporter
from .models import utc_now


@dataclass(slots=True)
class TraceConfig:
    """OTLP trace export configuration."""

    protocol: str = "http"
    endpoint: str = "http://localhost:4318/v1/traces"
    headers: dict[str, str] = field(default_factory=dict)
    insecure: bool = True


@dataclass(slots=True)
class GenerationExportConfig:
    """Generation ingest export configuration."""

    protocol: str = "grpc"
    endpoint: str = "localhost:4317"
    headers: dict[str, str] = field(default_factory=dict)
    insecure: bool = True
    batch_size: int = 100
    flush_interval: timedelta = timedelta(seconds=1)
    queue_size: int = 2000
    max_retries: int = 5
    initial_backoff: timedelta = timedelta(milliseconds=100)
    max_backoff: timedelta = timedelta(seconds=5)
    payload_max_bytes: int = 4 << 20


@dataclass(slots=True)
class ClientConfig:
    """Top-level SDK runtime configuration."""

    trace: TraceConfig = field(default_factory=TraceConfig)
    generation_export: GenerationExportConfig = field(default_factory=GenerationExportConfig)
    tracer: Optional[Tracer] = None
    logger: Optional[logging.Logger] = None
    now: Optional[Callable[[], datetime]] = None
    sleep: Optional[Callable[[float], None]] = None
    generation_exporter: Optional[GenerationExporter] = None

    # Convenience aliases for simpler caller config wiring.
    trace_endpoint: str = ""
    generation_export_endpoint: str = ""


def default_config() -> ClientConfig:
    """Returns the default production runtime configuration."""

    return ClientConfig()


def resolve_config(config: Optional[ClientConfig]) -> ClientConfig:
    """Resolves caller config against defaults."""

    if config is None:
        out = default_config()
    else:
        out = config

    if out.trace_endpoint:
        out.trace.endpoint = out.trace_endpoint
    if out.generation_export_endpoint:
        out.generation_export.endpoint = out.generation_export_endpoint
    if out.logger is None:
        out.logger = logging.getLogger("sigil_sdk")
    if out.now is None:
        out.now = utc_now
    if out.sleep is None:
        out.sleep = time.sleep

    if out.generation_export.batch_size <= 0:
        out.generation_export.batch_size = 1
    if out.generation_export.queue_size <= 0:
        out.generation_export.queue_size = 1
    if out.generation_export.flush_interval.total_seconds() <= 0:
        out.generation_export.flush_interval = timedelta(milliseconds=1)
    if out.generation_export.max_retries < 0:
        out.generation_export.max_retries = 0
    if out.generation_export.initial_backoff.total_seconds() <= 0:
        out.generation_export.initial_backoff = timedelta(milliseconds=100)
    if out.generation_export.max_backoff.total_seconds() <= 0:
        out.generation_export.max_backoff = timedelta(milliseconds=100)

    return out
