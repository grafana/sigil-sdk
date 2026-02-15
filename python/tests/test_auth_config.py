"""Per-export auth config tests."""

from __future__ import annotations

import pytest

from sigil_sdk import AuthConfig, ClientConfig, GenerationExportConfig
from sigil_sdk.config import resolve_config


def test_resolve_config_injects_tenant_header_for_generation_export() -> None:
    cfg = resolve_config(
        ClientConfig(
            generation_export=GenerationExportConfig(
                auth=AuthConfig(mode="tenant", tenant_id="tenant-a"),
            ),
        )
    )

    assert cfg.generation_export.headers["X-Scope-OrgID"] == "tenant-a"


def test_resolve_config_keeps_explicit_headers() -> None:
    cfg = resolve_config(
        ClientConfig(
            generation_export=GenerationExportConfig(
                headers={"x-scope-orgid": "override-tenant"},
                auth=AuthConfig(mode="tenant", tenant_id="tenant-a"),
            ),
        )
    )

    assert cfg.generation_export.headers["x-scope-orgid"] == "override-tenant"


@pytest.mark.parametrize(
    "auth",
    [
        AuthConfig(mode="tenant"),
        AuthConfig(mode="bearer"),
        AuthConfig(mode="none", tenant_id="tenant-a"),
        AuthConfig(mode="none", bearer_token="token"),
        AuthConfig(mode="tenant", tenant_id="tenant-a", bearer_token="token"),
        AuthConfig(mode="bearer", tenant_id="tenant-a", bearer_token="token"),
        AuthConfig(mode="unknown", tenant_id="tenant-a"),
    ],
)
def test_resolve_config_rejects_invalid_auth_combinations(auth: AuthConfig) -> None:
    with pytest.raises(ValueError):
        resolve_config(ClientConfig(generation_export=GenerationExportConfig(auth=auth)))
