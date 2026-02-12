"""gRPC transport for generation export."""

from __future__ import annotations

from urllib.parse import urlparse

import grpc

from ..internal.gen.sigil.v1 import generation_ingest_pb2 as sigil_pb2
from ..internal.gen.sigil.v1 import generation_ingest_pb2_grpc as sigil_pb2_grpc
from ..models import ExportGenerationResult, ExportGenerationsRequest, ExportGenerationsResponse
from ..proto_mapping import generation_to_proto


class GRPCGenerationExporter:
    """Sends generation batches to `sigil.v1.GenerationIngestService`."""

    def __init__(self, endpoint: str, headers: dict[str, str] | None = None, insecure: bool = False) -> None:
        host, implicit_insecure = _parse_endpoint(endpoint)
        self._headers = list((headers or {}).items())
        self._channel = grpc.insecure_channel(host) if (insecure or implicit_insecure) else grpc.secure_channel(host, grpc.ssl_channel_credentials())
        self._stub = sigil_pb2_grpc.GenerationIngestServiceStub(self._channel)

    def export_generations(self, request: ExportGenerationsRequest) -> ExportGenerationsResponse:
        grpc_request = sigil_pb2.ExportGenerationsRequest(
            generations=[generation_to_proto(generation) for generation in request.generations]
        )
        response = self._stub.ExportGenerations(grpc_request, timeout=10, metadata=self._headers)
        return ExportGenerationsResponse(
            results=[
                ExportGenerationResult(
                    generation_id=result.generation_id,
                    accepted=result.accepted,
                    error=result.error,
                )
                for result in response.results
            ]
        )

    def shutdown(self) -> None:
        self._channel.close()


def _parse_endpoint(endpoint: str) -> tuple[str, bool]:
    trimmed = endpoint.strip()
    if not trimmed:
        raise ValueError("endpoint is required")

    if "://" not in trimmed:
        return trimmed, False

    parsed = urlparse(trimmed)
    if parsed.netloc == "":
        raise ValueError("endpoint host is required")

    return parsed.netloc, parsed.scheme == "http"
