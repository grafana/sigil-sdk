"""Runtime configuration for the Sigil Python SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timedelta
import logging
import time
from typing import Callable, Optional

from opentelemetry.metrics import Meter
from opentelemetry.trace import Tracer

from .exporters.base import GenerationExporter
from .models import utc_now

TENANT_HEADER = "X-Scope-OrgID"
AUTHORIZATION_HEADER = "Authorization"


@dataclass(slots=True)
class AuthConfig:
    """Per-export auth configuration."""

    mode: str = "none"
    tenant_id: str = ""
    bearer_token: str = ""


@dataclass(slots=True)
class TraceConfig:
    """OTLP trace export configuration."""

    protocol: str = "http"
    endpoint: str = "http://localhost:4318/v1/traces"
    headers: dict[str, str] = field(default_factory=dict)
    auth: AuthConfig = field(default_factory=AuthConfig)
    insecure: bool = True


@dataclass(slots=True)
class GenerationExportConfig:
    """Generation ingest export configuration."""

    protocol: str = "grpc"
    endpoint: str = "localhost:4317"
    headers: dict[str, str] = field(default_factory=dict)
    auth: AuthConfig = field(default_factory=AuthConfig)
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
    meter: Optional[Meter] = None
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

    out.trace.headers = _resolve_export_headers(out.trace.headers, out.trace.auth, "trace")
    out.generation_export.headers = _resolve_export_headers(
        out.generation_export.headers,
        out.generation_export.auth,
        "generation export",
    )

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


def _resolve_export_headers(headers: dict[str, str], auth: AuthConfig, label: str) -> dict[str, str]:
    mode = auth.mode.strip().lower() if auth.mode else "none"
    tenant_id = auth.tenant_id.strip()
    bearer_token = auth.bearer_token.strip()
    out = dict(headers)

    if mode == "none":
        if tenant_id or bearer_token:
            raise ValueError(f"{label} auth mode 'none' does not allow tenant_id or bearer_token")
        return out
    if mode == "tenant":
        if not tenant_id:
            raise ValueError(f"{label} auth mode 'tenant' requires tenant_id")
        if bearer_token:
            raise ValueError(f"{label} auth mode 'tenant' does not allow bearer_token")
        if not _has_header(out, TENANT_HEADER):
            out[TENANT_HEADER] = tenant_id
        return out
    if mode == "bearer":
        if not bearer_token:
            raise ValueError(f"{label} auth mode 'bearer' requires bearer_token")
        if tenant_id:
            raise ValueError(f"{label} auth mode 'bearer' does not allow tenant_id")
        if not _has_header(out, AUTHORIZATION_HEADER):
            out[AUTHORIZATION_HEADER] = _format_bearer_token(bearer_token)
        return out

    raise ValueError(f"unsupported {label} auth mode {auth.mode!r}")


def _has_header(headers: dict[str, str], key: str) -> bool:
    target = key.lower()
    return any(existing.lower() == target for existing in headers.keys())


def _format_bearer_token(token: str) -> str:
    value = token.strip()
    if value.lower().startswith("bearer "):
        value = value[7:].strip()
    return f"Bearer {value}"
