"""Bounded, idempotent preprocessing for archived Experiment results."""

from __future__ import annotations

import hashlib
import json
import mimetypes
import stat
import tempfile
import zipfile
from collections.abc import Mapping
from pathlib import Path, PurePosixPath
from typing import Any

from mmdash_worker.jobs.handlers import HandlerContext, HandlerError

MAX_BUNDLE_BYTES = 10 * 1024 * 1024 * 1024
MAX_MANIFEST_BYTES = 1024 * 1024
MAX_FILES = 10_000
VALID_KINDS = {"file", "log", "figure", "table", "data", "model", "summary"}


class ExperimentResultHandler:
    """Safely preprocess an immutable Bundle and request trusted finalization."""

    def __init__(self, client: Any) -> None:
        self.client = client

    def __call__(
        self,
        context: HandlerContext,
        _payload: Mapping[str, Any],
    ) -> Mapping[str, Any]:
        job_input = self.client.get_experiment_result_input(context.job_id)
        expected_size = _required_int(job_input, "bundle_size_bytes", minimum=1)
        expected_hash = _required_sha256(job_input, "bundle_sha256")
        expected_manifest_hash = _required_sha256(job_input, "manifest_sha256")
        experiment_id = _required_string(job_input, "experiment_id")
        result_directory = _required_string(job_input, "result_directory")
        transfer = job_input.get("transfer")
        if expected_size > MAX_BUNDLE_BYTES or not isinstance(transfer, Mapping):
            raise HandlerError("RESULT_BUNDLE_INVALID", "Result Bundle input is invalid")

        with tempfile.TemporaryDirectory(prefix="mmdash-experiment-result-") as temporary:
            root = Path(temporary)
            bundle_path = root / "execution-bundle.zip"
            downloaded = self.client.download_transfer(
                transfer,
                bundle_path,
                max_bytes=expected_size,
            )
            if downloaded.get("size_bytes") != expected_size:
                raise HandlerError("RESULT_BUNDLE_INVALID", "Result Bundle size does not match")
            if _file_sha256(bundle_path) != expected_hash:
                raise HandlerError("RESULT_BUNDLE_INVALID", "Result Bundle hash does not match")
            manifest, manifest_hash, files = _extract_bundle(
                bundle_path,
                root / "result",
                experiment_id=experiment_id,
                result_directory=result_directory,
            )
            if manifest_hash != expected_manifest_hash:
                raise HandlerError("RESULT_MANIFEST_INVALID", "Result Manifest hash does not match")
            total_bytes = sum(int(item["size_bytes"]) for item in files)
            kinds: dict[str, int] = {}
            for item in files:
                kind = str(item["kind"])
                kinds[kind] = kinds.get(kind, 0) + 1
            summary = str(manifest.get("summary", "")).strip()
            if not summary:
                summary = f"{len(files)} files, {total_bytes} bytes"
            response = self.client.finalize_experiment_result(
                context.job_id,
                {
                    "manifest_sha256": manifest_hash,
                    "files": files,
                    "summary": summary,
                    "analysis": json.dumps(
                        {
                            "file_count": len(files),
                            "total_bytes": total_bytes,
                            "kinds": kinds,
                        },
                        sort_keys=True,
                        separators=(",", ":"),
                    ),
                },
            )
        return {
            "experiment_id": response.get("experiment_id", experiment_id),
            "result_commit_sha": response.get("result_commit_sha", ""),
            "handled_by": context.worker_id,
        }


def _extract_bundle(
    bundle_path: Path,
    output_root: Path,
    *,
    experiment_id: str,
    result_directory: str,
) -> tuple[dict[str, Any], str, list[dict[str, Any]]]:
    try:
        archive = zipfile.ZipFile(bundle_path)
    except (OSError, zipfile.BadZipFile) as error:
        raise HandlerError("RESULT_BUNDLE_INVALID", "Result Bundle is not a valid ZIP") from error
    with archive:
        entries = archive.infolist()
        if not 1 <= len(entries) <= MAX_FILES + 1:
            raise HandlerError("RESULT_BUNDLE_INVALID", "Result Bundle file count is invalid")
        by_name: dict[str, zipfile.ZipInfo] = {}
        manifest_entry: zipfile.ZipInfo | None = None
        expanded = 0
        for entry in entries:
            if entry.filename == "manifest.json":
                if manifest_entry is not None or entry.file_size > MAX_MANIFEST_BYTES:
                    raise HandlerError("RESULT_MANIFEST_INVALID", "Result Manifest is invalid")
                manifest_entry = entry
                continue
            if (
                not _safe_result_path(entry.filename)
                or entry.is_dir()
                or stat.S_ISLNK(entry.external_attr >> 16)
                or entry.flag_bits & 0x1
                or entry.filename in by_name
            ):
                raise HandlerError("RESULT_BUNDLE_INVALID", "Result Bundle contains an unsafe path")
            expanded += entry.file_size
            if expanded > MAX_BUNDLE_BYTES:
                raise HandlerError("RESULT_BUNDLE_INVALID", "Result Bundle expands beyond its limit")
            by_name[entry.filename] = entry
        if manifest_entry is None:
            raise HandlerError("RESULT_MANIFEST_INVALID", "Result Manifest is missing")
        manifest_bytes = archive.read(manifest_entry)
        manifest_hash = hashlib.sha256(manifest_bytes).hexdigest()
        try:
            manifest = json.loads(manifest_bytes)
        except (json.JSONDecodeError, UnicodeDecodeError) as error:
            raise HandlerError("RESULT_MANIFEST_INVALID", "Result Manifest is invalid JSON") from error
        if not isinstance(manifest, dict):
            raise HandlerError("RESULT_MANIFEST_INVALID", "Result Manifest must be an object")
        allowed = {
            "schema_version", "experiment_id", "source_commit", "result_directory",
            "status", "started_at", "finished_at", "runtime", "runtime_version",
            "logs_truncated", "summary", "exit_code", "files",
        }
        required = allowed - {"summary", "exit_code"}
        if (
            set(manifest) - allowed
            or not required.issubset(manifest)
            or manifest.get("schema_version") != "2"
            or manifest.get("experiment_id") != experiment_id
            or manifest.get("result_directory") != result_directory
            or manifest.get("status") != "succeeded"
        ):
            raise HandlerError("RESULT_MANIFEST_INVALID", "Result Manifest identity is invalid")
        raw_files = manifest.get("files")
        if not isinstance(raw_files, list) or len(raw_files) != len(by_name):
            raise HandlerError("RESULT_MANIFEST_INVALID", "Result Manifest file list is invalid")
        output_root.mkdir(mode=0o700)
        prepared: list[dict[str, Any]] = []
        seen: set[str] = set()
        for raw in raw_files:
            if not isinstance(raw, Mapping):
                raise HandlerError("RESULT_MANIFEST_INVALID", "Result Manifest file is invalid")
            name = raw.get("path")
            digest = raw.get("sha256")
            size = raw.get("size_bytes")
            kind = raw.get("kind")
            if (
                not isinstance(name, str)
                or not _safe_result_path(name)
                or name in seen
                or not isinstance(digest, str)
                or len(digest) != 64
                or any(character not in "0123456789abcdef" for character in digest)
                or not isinstance(size, int)
                or isinstance(size, bool)
                or size < 0
                or kind not in VALID_KINDS
            ):
                raise HandlerError("RESULT_MANIFEST_INVALID", "Result Manifest file is invalid")
            entry = by_name.get(name)
            if entry is None or entry.file_size != size:
                raise HandlerError("RESULT_MANIFEST_INVALID", "Result Manifest does not match Bundle")
            destination = output_root.joinpath(*PurePosixPath(name).parts)
            if output_root.resolve() not in destination.resolve().parents:
                raise HandlerError("RESULT_BUNDLE_INVALID", "Result Bundle path escaped extraction root")
            destination.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            hasher = hashlib.sha256()
            copied = 0
            with archive.open(entry) as source, destination.open("xb") as target:
                while chunk := source.read(64 * 1024):
                    copied += len(chunk)
                    if copied > size:
                        raise HandlerError("RESULT_BUNDLE_INVALID", "Result file expanded unexpectedly")
                    hasher.update(chunk)
                    target.write(chunk)
            if copied != size or hasher.hexdigest() != digest:
                raise HandlerError("RESULT_MANIFEST_INVALID", "Result file hash does not match")
            media_type = raw.get("mime_type")
            if not isinstance(media_type, str) or not media_type.strip() or len(media_type) > 255:
                media_type = mimetypes.guess_type(name)[0] or "application/octet-stream"
            prepared.append(
                {
                    "path": name,
                    "sha256": digest,
                    "size_bytes": size,
                    "kind": kind,
                    "media_type": media_type.strip(),
                }
            )
            seen.add(name)
        return manifest, manifest_hash, prepared


def _safe_result_path(value: str) -> bool:
    if not value or len(value) > 4096 or "\\" in value or ":" in value or "\x00" in value:
        return False
    path = PurePosixPath(value)
    return not path.is_absolute() and str(path) == value and all(part not in {"", ".", ".."} for part in path.parts)


def _file_sha256(filename: Path) -> str:
    digest = hashlib.sha256()
    with filename.open("rb") as source:
        while chunk := source.read(64 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


def _required_string(value: Mapping[str, Any], name: str) -> str:
    result = value.get(name)
    if not isinstance(result, str) or not result.strip():
        raise HandlerError("RESULT_JOB_INPUT_INVALID", f"Result Job input {name} is invalid")
    return result.strip()


def _required_int(value: Mapping[str, Any], name: str, *, minimum: int) -> int:
    result = value.get(name)
    if not isinstance(result, int) or isinstance(result, bool) or result < minimum:
        raise HandlerError("RESULT_JOB_INPUT_INVALID", f"Result Job input {name} is invalid")
    return result


def _required_sha256(value: Mapping[str, Any], name: str) -> str:
    result = _required_string(value, name)
    if len(result) != 64 or any(character not in "0123456789abcdef" for character in result):
        raise HandlerError("RESULT_JOB_INPUT_INVALID", f"Result Job input {name} is invalid")
    return result


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
