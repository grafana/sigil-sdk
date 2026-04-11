"""Per-export auth config tests."""

from __future__ import annotations

import base64

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


def test_resolve_config_basic_auth_with_tenant_id() -> None:
    cfg = resolve_config(
        ClientConfig(
            generation_export=GenerationExportConfig(
                auth=AuthConfig(mode="basic", tenant_id="42", basic_password="secret"),
            ),
        )
    )

    expected = "Basic " + base64.b64encode(b"42:secret").decode()
    assert cfg.generation_export.headers["Authorization"] == expected
    assert cfg.generation_export.headers["X-Scope-OrgID"] == "42"


def test_resolve_config_basic_auth_with_explicit_user() -> None:
    cfg = resolve_config(
        ClientConfig(
            generation_export=GenerationExportConfig(
                auth=AuthConfig(
                    mode="basic",
                    tenant_id="42",
                    basic_user="probe-user",
                    basic_password="secret",
                ),
            ),
        )
    )

    expected = "Basic " + base64.b64encode(b"probe-user:secret").decode()
    assert cfg.generation_export.headers["Authorization"] == expected
    assert cfg.generation_export.headers["X-Scope-OrgID"] == "42"


def test_resolve_config_basic_auth_explicit_header_wins() -> None:
    cfg = resolve_config(
        ClientConfig(
            generation_export=GenerationExportConfig(
                headers={
                    "Authorization": "Basic override",
                    "X-Scope-OrgID": "override-tenant",
                },
                auth=AuthConfig(mode="basic", tenant_id="42", basic_password="secret"),
            ),
        )
    )

    assert cfg.generation_export.headers["Authorization"] == "Basic override"
    assert cfg.generation_export.headers["X-Scope-OrgID"] == "override-tenant"


@pytest.mark.parametrize(
    "auth",
    [
        AuthConfig(mode="tenant"),
        AuthConfig(mode="bearer"),
        AuthConfig(mode="none", tenant_id="tenant-a"),
        AuthConfig(mode="none", bearer_token="token"),
        AuthConfig(mode="none", basic_user="user"),
        AuthConfig(mode="none", basic_password="secret"),
        AuthConfig(mode="tenant", tenant_id="tenant-a", bearer_token="token"),
        AuthConfig(mode="bearer", tenant_id="tenant-a", bearer_token="token"),
        AuthConfig(mode="unknown", tenant_id="tenant-a"),
        AuthConfig(mode="basic"),
        AuthConfig(mode="basic", basic_password="secret"),
    ],
)
def test_resolve_config_rejects_invalid_auth_combinations(auth: AuthConfig) -> None:
    with pytest.raises(ValueError):
        resolve_config(ClientConfig(generation_export=GenerationExportConfig(auth=auth)))
