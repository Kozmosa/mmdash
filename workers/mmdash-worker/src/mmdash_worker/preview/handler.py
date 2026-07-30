"""Artifact preview Job handler."""

from __future__ import annotations

import asyncio
import hashlib
import tempfile
from collections.abc import Mapping
from pathlib import Path
from typing import Any, Protocol

from mmdash_worker.jobs.handlers import HandlerContext, HandlerError
from mmdash_worker.preview.processor import PreviewFailure, PreviewProcessor


class ArtifactTransferClient(Protocol):
    """Worker-side Core transfer boundary."""

    def request_artifact_transfer(
        self,
        job_id: str,
        request: Mapping[str, Any],
    ) -> dict[str, Any]: ...

    def download_transfer(
        self,
        grant: Mapping[str, Any],
        destination: Path,
        *,
        max_bytes: int,
    ) -> dict[str, Any]: ...

    def upload_transfer(
        self,
        grant: Mapping[str, Any],
        source: Path,
        *,
        size_bytes: int,
    ) -> str: ...


class ArtifactPreviewHandler:
    """Downloads one immutable Version and returns bounded preview metadata."""

    def __init__(
        self,
        client: ArtifactTransferClient,
        processor: PreviewProcessor | None = None,
    ) -> None:
        self.client = client
        self.processor = processor or PreviewProcessor()

    async def __call__(
        self,
        context: HandlerContext,
        payload: Mapping[str, Any],
    ) -> Mapping[str, Any]:
        return await asyncio.to_thread(self._run, context, payload)

    def _run(
        self,
        context: HandlerContext,
        payload: Mapping[str, Any],
    ) -> Mapping[str, Any]:
        project_id = _required_string(payload, "project_id")
        artifact_id = _required_string(payload, "artifact_id")
        version_id = _required_string(payload, "version_id")
        preview_id = _required_string(payload, "preview_id")
        preview_type = _required_string(payload, "preview_type")
        if preview_type not in {"image", "pdf", "csv", "json", "text"}:
            raise HandlerError(
                "ARTIFACT_PREVIEW_INVALID_JOB",
                "Artifact preview target is invalid",
            )
        if context.cancellation_requested:
            raise HandlerError("JOB_CANCELLED", "Preview cancellation was requested")

        input_grant = self.client.request_artifact_transfer(
            context.job_id,
            {"direction": "input", "version_id": version_id},
        )
        with tempfile.TemporaryDirectory(prefix="mmdash-preview-") as directory:
            source_path = Path(directory) / "source"
            try:
                self.client.download_transfer(
                    input_grant,
                    source_path,
                    max_bytes=self.processor.config.max_input_bytes,
                )
                processed = self.processor.process(source_path, preview_type)
            except PreviewFailure as error:
                raise HandlerError(error.code, str(error)) from error
            if context.cancellation_requested:
                raise HandlerError("JOB_CANCELLED", "Preview cancellation was requested")

            outputs: list[dict[str, Any]] = []
            if processed.thumbnail is not None:
                thumbnail_path = Path(directory) / processed.thumbnail.filename
                thumbnail_path.write_bytes(processed.thumbnail.content)
                digest = hashlib.sha256(processed.thumbnail.content).hexdigest()
                output_grant = self.client.request_artifact_transfer(
                    context.job_id,
                    {
                        "direction": "output",
                        "version_id": version_id,
                        "preview_type": "thumbnail",
                        "filename": processed.thumbnail.filename,
                        "mime_type": processed.thumbnail.mime_type,
                        "size_bytes": len(processed.thumbnail.content),
                        "sha256": digest,
                    },
                )
                etag = self.client.upload_transfer(
                    output_grant,
                    thumbnail_path,
                    size_bytes=len(processed.thumbnail.content),
                )
                outputs.append({"preview_type": "thumbnail", "etag": etag})

        return {
            "project_id": project_id,
            "artifact_id": artifact_id,
            "version_id": version_id,
            "preview_id": preview_id,
            "preview_type": preview_type,
            "status": processed.status,
            "structural_summary": processed.structural_summary,
            "error_code": processed.error_code,
            "outputs": outputs,
        }


def _required_string(payload: Mapping[str, Any], name: str) -> str:
    value = payload.get(name)
    if not isinstance(value, str) or not value.strip() or len(value) > 255:
        raise HandlerError(
            "ARTIFACT_PREVIEW_INVALID_JOB",
            "Artifact preview Job payload is invalid",
        )
    return value.strip()
