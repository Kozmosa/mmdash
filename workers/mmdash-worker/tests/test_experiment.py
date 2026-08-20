import hashlib
import json
import shutil
import zipfile
from pathlib import Path
from typing import Any

import pytest

from mmdash_worker.experiment import ExperimentResultHandler, compare_results, summarize_result
from mmdash_worker.jobs.handlers import HandlerContext, HandlerError


def test_summarize_result_is_bounded_and_deterministic() -> None:
    result = summarize_result(HandlerContext("job", "worker"), {"experiment_id": "exp", "manifest": {"files": [{"path": "a.csv", "size_bytes": 3, "kind": "table"}]}})
    assert result["summary"] == "1 files, 3 bytes"
    assert len(result["summary_hash"]) == 64


def test_compare_requires_a_bounded_set() -> None:
    try:
        compare_results(HandlerContext("job", "worker"), {"items": []})
    except HandlerError as error:
        assert error.code == "COMPARISON_INPUT_INVALID"
    else:
        raise AssertionError("empty comparison was accepted")


class ResultClient:
    def __init__(self, bundle: Path, manifest_hash: str) -> None:
        self.bundle = bundle
        self.manifest_hash = manifest_hash
        self.finalized: dict[str, Any] | None = None

    def get_experiment_result_input(self, job_id: str) -> dict[str, Any]:
        assert job_id == "job-1"
        contents = self.bundle.read_bytes()
        return {
            "experiment_id": "experiment-1",
            "result_directory": "experiments/experiment-1_20260816_1200/",
            "bundle_size_bytes": len(contents),
            "bundle_sha256": hashlib.sha256(contents).hexdigest(),
            "manifest_sha256": self.manifest_hash,
            "transfer": {"method": "GET", "url": "http://core/bundle", "headers": {}},
        }

    def download_transfer(
        self,
        _grant: dict[str, Any],
        destination: Path,
        *,
        max_bytes: int,
    ) -> dict[str, Any]:
        assert self.bundle.stat().st_size <= max_bytes
        shutil.copyfile(self.bundle, destination)
        return {"size_bytes": destination.stat().st_size}

    def finalize_experiment_result(
        self,
        job_id: str,
        result: dict[str, Any],
    ) -> dict[str, Any]:
        assert job_id == "job-1"
        self.finalized = result
        return {"experiment_id": "experiment-1", "result_commit_sha": "a" * 40}


def _bundle(tmp_path: Path, *, result_name: str = "summary.txt") -> tuple[Path, str]:
    contents = b"result"
    manifest = {
        "schema_version": "2",
        "experiment_id": "experiment-1",
        "source_commit": "b" * 40,
        "result_directory": "experiments/experiment-1_20260816_1200/",
        "status": "succeeded",
        "started_at": "2026-08-16T12:00:00Z",
        "finished_at": "2026-08-16T12:00:01Z",
        "runtime": "local-docker",
        "runtime_version": "1",
        "logs_truncated": False,
        "exit_code": 0,
        "environment": {
            "environment_key": "c" * 64,
            "base_image_id": "sha256:base",
            "environment_image_id": "sha256:environment",
            "manifest_paths": ["requirements.lock"],
            "manifest_hashes": {"requirements.lock": "d" * 64},
            "builder_version": "1",
            "cache_hit": False,
        },
        "files": [
            {
                "path": result_name,
                "sha256": hashlib.sha256(contents).hexdigest(),
                "size_bytes": len(contents),
                "kind": "summary",
                "mime_type": "text/plain",
            }
        ],
    }
    manifest_bytes = json.dumps(manifest, separators=(",", ":")).encode()
    filename = tmp_path / "execution-bundle.zip"
    with zipfile.ZipFile(filename, "w") as archive:
        archive.writestr("manifest.json", manifest_bytes)
        archive.writestr(result_name, contents)
    return filename, hashlib.sha256(manifest_bytes).hexdigest()


def test_result_handler_validates_extracts_and_finalizes(tmp_path: Path) -> None:
    bundle, manifest_hash = _bundle(tmp_path)
    client = ResultClient(bundle, manifest_hash)

    result = ExperimentResultHandler(client)(HandlerContext("job-1", "worker-1"), {})

    assert result["result_commit_sha"] == "a" * 40
    assert client.finalized is not None
    assert client.finalized["manifest_sha256"] == manifest_hash
    assert client.finalized["files"][0]["path"] == "summary.txt"


def test_result_handler_rejects_zip_slip(tmp_path: Path) -> None:
    bundle, manifest_hash = _bundle(tmp_path, result_name="../secret.txt")
    client = ResultClient(bundle, manifest_hash)

    with pytest.raises(HandlerError, match="unsafe path") as caught:
        ExperimentResultHandler(client)(HandlerContext("job-1", "worker-1"), {})

    assert caught.value.code == "RESULT_BUNDLE_INVALID"
