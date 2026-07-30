"""Reserved semantic-description boundary for the later Article stage."""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any, Protocol


class SemanticDescriptionGenerator(Protocol):
    """Future semantic generator; Stage 2 provides no implementation or call site."""

    def generate(
        self,
        *,
        project_id: str,
        artifact_id: str,
        version_id: str,
        structural_summary: Mapping[str, Any],
    ) -> Mapping[str, Any] | None:
        """Return semantic fields when a later product stage supplies an implementation."""
