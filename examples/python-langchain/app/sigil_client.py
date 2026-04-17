"""Sigil client bootstrap.

Builds a `sigil_sdk.Client` that reuses the app's OTel providers, so the
`gen_ai.*` spans and metrics it emits flow through the same pipeline as
everything else. Config comes from environment variables (see `.env.example`).
"""

from __future__ import annotations

import os

from opentelemetry.metrics import MeterProvider
from opentelemetry.trace import TracerProvider

from sigil_sdk import ApiConfig, AuthConfig, Client, ClientConfig, GenerationExportConfig


def _required_env(name: str) -> str:
    value = os.getenv(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} must be set (see .env.example).")
    return value


def setup_sigil(
    *,
    tracer_provider: TracerProvider,
    meter_provider: MeterProvider,
) -> Client:
    endpoint = _required_env("SIGIL_GENERATION_EXPORT_ENDPOINT")
    api_endpoint = _required_env("SIGIL_API_ENDPOINT")
    tenant_id = _required_env("SIGIL_TENANT_ID")
    auth_token = os.getenv("SIGIL_AUTH_TOKEN", "").strip()

    if auth_token:
        # Grafana Cloud: basic auth (user = instance ID, password = access token).
        auth = AuthConfig(
            mode="basic",
            tenant_id=tenant_id,
            basic_user=tenant_id,
            basic_password=auth_token,
        )
    else:
        # Self-hosted / local dev: just X-Scope-OrgID, no credentials.
        auth = AuthConfig(mode="tenant", tenant_id=tenant_id)

    return Client(
        ClientConfig(
            generation_export=GenerationExportConfig(
                protocol="http",
                endpoint=endpoint,
                auth=auth,
            ),
            api=ApiConfig(endpoint=api_endpoint),
            tracer=tracer_provider.get_tracer("sigil-sdk"),
            meter=meter_provider.get_meter("sigil-sdk"),
        )
    )
