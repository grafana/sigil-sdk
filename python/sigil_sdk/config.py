"""Runtime configuration for the Sigil Python SDK."""

from __future__ import annotations

import base64
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
    basic_user: str = ""
    basic_password: str = ""


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
class ApiConfig:
    """Sigil HTTP API settings used by non-ingest helper endpoints."""

    endpoint: str = "http://localhost:8080"


@dataclass(slots=True)
class EmbeddingCaptureConfig:
    """Embedding input capture settings for span attributes."""

    capture_input: bool = False
    max_input_items: int = 20
    max_text_length: int = 1024


@dataclass(slots=True)
class ClientConfig:
    """Top-level SDK runtime configuration."""

    generation_export: GenerationExportConfig = field(default_factory=GenerationExportConfig)
    api: ApiConfig = field(default_factory=ApiConfig)
    embedding_capture: EmbeddingCaptureConfig = field(default_factory=EmbeddingCaptureConfig)
    tracer: Optional[Tracer] = None
    meter: Optional[Meter] = None
    logger: Optional[logging.Logger] = None
    now: Optional[Callable[[], datetime]] = None
    sleep: Optional[Callable[[float], None]] = None
    generation_exporter: Optional[GenerationExporter] = None

    # Convenience aliases for simpler caller config wiring.
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

    if out.generation_export_endpoint:
        out.generation_export.endpoint = out.generation_export_endpoint
    if out.api.endpoint.strip() == "":
        out.api.endpoint = "http://localhost:8080"

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

    if out.embedding_capture.max_input_items <= 0:
        out.embedding_capture.max_input_items = 20
    if out.embedding_capture.max_text_length <= 0:
        out.embedding_capture.max_text_length = 1024

    return out


def _resolve_export_headers(headers: dict[str, str], auth: AuthConfig, label: str) -> dict[str, str]:
    mode = auth.mode.strip().lower() if auth.mode else "none"
    tenant_id = auth.tenant_id.strip()
    bearer_token = auth.bearer_token.strip()
    out = dict(headers)

    if mode == "none":
        basic_user = auth.basic_user.strip()
        basic_password = auth.basic_password.strip()
        if tenant_id or bearer_token or basic_user or basic_password:
            raise ValueError(f"{label} auth mode 'none' does not allow credentials")
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
    if mode == "basic":
        password = auth.basic_password.strip()
        if not password:
            raise ValueError(f"{label} auth mode 'basic' requires basic_password")
        user = auth.basic_user.strip()
        if not user:
            user = tenant_id
        if not user:
            raise ValueError(f"{label} auth mode 'basic' requires basic_user or tenant_id")
        if not _has_header(out, AUTHORIZATION_HEADER):
            creds = base64.b64encode(f"{user}:{password}".encode()).decode()
            out[AUTHORIZATION_HEADER] = f"Basic {creds}"
        if tenant_id and not _has_header(out, TENANT_HEADER):
            out[TENANT_HEADER] = tenant_id
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
