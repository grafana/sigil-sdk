"""Shared test fixtures/helpers for Sigil Python SDK tests."""

from __future__ import annotations

import copy

from sigil_sdk.models import ExportGenerationResult, ExportGenerationsResponse


class CapturingGenerationExporter:
    """In-memory generation exporter used by runtime tests."""

    def __init__(self, failures_before_success: int = 0) -> None:
        self.failures_before_success = failures_before_success
        self.requests = []
        self.attempts = 0
        self.shutdown_calls = 0

    def export_generations(self, request):
        self.attempts += 1
        self.requests.append(copy.deepcopy(request))

        if self.failures_before_success > 0:
            self.failures_before_success -= 1
            raise RuntimeError("forced export failure")

        return ExportGenerationsResponse(
            results=[
                ExportGenerationResult(
                    generation_id=generation.id,
                    accepted=True,
                )
                for generation in request.generations
            ]
        )

    def shutdown(self) -> None:
        self.shutdown_calls += 1
