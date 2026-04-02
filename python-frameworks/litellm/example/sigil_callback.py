"""Sigil callback for LiteLLM proxy — local dev stack."""

import os

from sigil_sdk import Client
from sigil_sdk.config import AuthConfig, ClientConfig, GenerationExportConfig
from sigil_sdk_litellm import SigilLiteLLMLogger

_endpoint = os.environ.get(
    "SIGIL_ENDPOINT", "http://host.docker.internal:8080/api/v1/generations:export"
)

client = Client(
    ClientConfig(
        generation_export=GenerationExportConfig(
            protocol="http",
            endpoint=_endpoint,
            auth=AuthConfig(mode="none"),
            insecure=True,
        ),
    )
)

sigil_handler = SigilLiteLLMLogger(
    client=client,
    agent_name="litellm-proxy-integration-test",
)
