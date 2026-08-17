import asyncio
import hashlib
from collections.abc import Mapping
from pathlib import Path
from typing import Any

from mmdash_worker.jobs.handlers import HandlerContext, worker_registry
from mmdash_worker.preview.handler import ArtifactPreviewHandler
from mmdash_worker.preview.processor import (
    PreviewConfig,
    PreviewProcessor,
    ProcessedPreview,
    Thumbnail,
)


class FakeTransferClient:
    def __init__(self, input_contents: bytes = b"source") -> None:
        self.input_contents = input_contents
        self.requests: list[tuple[str, dict[str, Any]]] = []
        self.uploaded = b""

    def request_artifact_transfer(
        self,
        job_id: str,
        request: Mapping[str, Any],
    ) -> dict[str, Any]:
        self.requests.append((job_id, dict(request)))
        return {
            "method": "GET" if request["direction"] == "input" else "PUT",
            "url": "http://core/transfer",
            "headers": {},
            "expires_at": "2026-07-30T12:00:00Z",
        }

    def download_transfer(
        self,
        _grant: Mapping[str, Any],
        destination: Path,
        *,
        max_bytes: int,
    ) -> dict[str, Any]:
        assert len(self.input_contents) <= max_bytes
        destination.write_bytes(self.input_contents)
        return {"size_bytes": len(self.input_contents), "content_type": "image/png"}

    def upload_transfer(
        self,
        _grant: Mapping[str, Any],
        source: Path,
        *,
        size_bytes: int,
    ) -> str:
        self.uploaded = source.read_bytes()
        assert len(self.uploaded) == size_bytes
        return "thumbnail-etag"

    def execute_artifact_semantic_description(self, job_id: str) -> dict[str, Any]:
        return {
            "description": "A calibrated source artifact.",
            "recommended_usage": ["Use as a fixed article reference."],
            "agent_session_id": "11111111-1111-4111-8111-111111111111",
            "agent_run_id": "22222222-2222-4222-8222-222222222222",
            "job_id_seen": job_id,
        }


class StaticProcessor(PreviewProcessor):
    def __init__(self) -> None:
        super().__init__(PreviewConfig(max_input_bytes=1024))

    def process(self, _path: Path, _preview_type: str) -> ProcessedPreview:
        return ProcessedPreview(
            status="available",
            structural_summary={"width": 10, "height": 5},
            thumbnail=Thumbnail(b"thumbnail", "thumbnail.png", "image/png"),
        )


def test_preview_handler_uses_job_bound_input_and_output_transfers() -> None:
    client = FakeTransferClient()
    handler = ArtifactPreviewHandler(client, StaticProcessor())
    payload = {
        "project_id": "project-1",
        "artifact_id": "artifact-1",
        "version_id": "version-1",
        "preview_id": "preview-1",
        "preview_type": "image",
    }

    result = asyncio.run(handler(HandlerContext(job_id="job-1", worker_id="worker-1"), payload))

    assert client.requests[0] == (
        "job-1",
        {"direction": "input", "version_id": "version-1"},
    )
    assert client.requests[1][1] == {
        "direction": "output",
        "version_id": "version-1",
        "preview_type": "thumbnail",
        "filename": "thumbnail.png",
        "mime_type": "image/png",
        "size_bytes": len(b"thumbnail"),
        "sha256": hashlib.sha256(b"thumbnail").hexdigest(),
    }
    assert result["outputs"] == [{"preview_type": "thumbnail", "etag": "thumbnail-etag"}]
    assert "url" not in str(result)


def test_production_registry_advertises_artifact_preview() -> None:
    registry = worker_registry(FakeTransferClient())
    assert registry.names() == (
        "article.build",
        "artifact.preview",
        "artifact.semantic.describe",
        "experiment.result.compare",
        "experiment.result.process",
        "model.notion.discover",
        "model.notion.snapshot",
        "progress.evaluate",
        "system.test",
    )


def test_semantic_handler_delegates_to_core_instead_of_a_provider() -> None:
    registry = worker_registry(FakeTransferClient())
    result = asyncio.run(
        registry.dispatch(
            "artifact.semantic.describe",
            HandlerContext(job_id="job-1", worker_id="worker-1"),
            {"untrusted": "payload is not sent to a provider"},
        )
    )
    assert result["job_id_seen"] == "job-1"
