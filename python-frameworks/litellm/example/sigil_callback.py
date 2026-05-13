"""Sigil callback for LiteLLM proxy."""

import os

from sigil_sdk import Client
from sigil_sdk.config import AuthConfig, ClientConfig, GenerationExportConfig
from sigil_sdk_litellm import SigilLiteLLMLogger

_endpoint = os.environ["SIGIL_ENDPOINT"]

client = Client(
    ClientConfig(
        generation_export=GenerationExportConfig(
            protocol="http",
            endpoint=_endpoint,
            auth=AuthConfig(
                mode="basic",
                tenant_id=os.environ.get("SIGIL_AUTH_TENANT_ID", ""),
                basic_password=os.environ.get("SIGIL_AUTH_TOKEN", ""),
            ),
        ),
    )
)

sigil_handler = SigilLiteLLMLogger(
    client=client,
    agent_name="litellm-proxy-integration-test",
)
