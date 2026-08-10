"""Bounded, idempotent preprocessing for archived Experiment results."""

from __future__ import annotations

import hashlib
import json
from collections.abc import Mapping
from typing import Any

from mmdash_worker.jobs.handlers import HandlerContext, HandlerError


def summarize_result(context: HandlerContext, payload: Mapping[str, Any]) -> Mapping[str, Any]:
    """Create a deterministic summary without reading business storage."""
    manifest = payload.get("manifest")
    if not isinstance(manifest, Mapping):
        raise HandlerError("RESULT_MANIFEST_INVALID", "Result manifest must be an object")
    files = manifest.get("files", [])
    if not isinstance(files, list) or len(files) > 10_000:
        raise HandlerError("RESULT_MANIFEST_INVALID", "Result manifest file count is invalid")
    total_bytes = 0
    kinds: dict[str, int] = {}
    for item in files:
        if not isinstance(item, Mapping):
            raise HandlerError("RESULT_MANIFEST_INVALID", "Result manifest contains an invalid file")
        size = item.get("size_bytes", 0)
        if not isinstance(size, int) or size < 0:
            raise HandlerError("RESULT_MANIFEST_INVALID", "Result manifest contains an invalid size")
        total_bytes += size
        kind = str(item.get("kind", "other"))
        kinds[kind] = kinds.get(kind, 0) + 1
    canonical = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
    return {
        "experiment_id": str(payload.get("experiment_id", "")),
        "summary": f"{len(files)} files, {total_bytes} bytes",
        "file_count": len(files),
        "total_bytes": total_bytes,
        "kinds": kinds,
        "summary_hash": hashlib.sha256(canonical).hexdigest(),
        "handled_by": context.worker_id,
    }


def compare_results(context: HandlerContext, payload: Mapping[str, Any]) -> Mapping[str, Any]:
    """Compare bounded numeric metrics supplied by Core job payload."""
    items = payload.get("items")
    if not isinstance(items, list) or not 2 <= len(items) <= 20:
        raise HandlerError("COMPARISON_INPUT_INVALID", "Comparison requires between 2 and 20 results")
    metrics: dict[str, list[dict[str, Any]]] = {}
    for item in items:
        if not isinstance(item, Mapping):
            raise HandlerError("COMPARISON_INPUT_INVALID", "Comparison item is invalid")
        experiment_id = str(item.get("experiment_id", ""))
        values = item.get("metrics", {})
        if not experiment_id or not isinstance(values, Mapping):
            raise HandlerError("COMPARISON_INPUT_INVALID", "Comparison item has no metrics")
        for key, value in values.items():
            if isinstance(value, (int, float)) and not isinstance(value, bool):
                metrics.setdefault(str(key), []).append({"experiment_id": experiment_id, "value": value})
    return {"items": items, "metrics": metrics, "handled_by": context.worker_id}
