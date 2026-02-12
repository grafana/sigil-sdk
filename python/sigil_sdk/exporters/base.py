"""Exporter protocol used by the generation runtime."""

from __future__ import annotations

from typing import Protocol

from ..models import ExportGenerationsRequest, ExportGenerationsResponse


class GenerationExporter(Protocol):
    """Exporter protocol for generation ingest transports."""

    def export_generations(self, request: ExportGenerationsRequest) -> ExportGenerationsResponse:
        """Exports one generation batch."""

    def shutdown(self) -> None:
        """Closes transport resources."""
