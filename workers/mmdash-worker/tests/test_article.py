import asyncio
import hashlib
import json
import stat
import zipfile
from collections.abc import Mapping
from pathlib import Path
from typing import Any
from unittest.mock import patch

import pytest

from mmdash_worker.article.handler import (
    ArticleBuildHandler,
    _CommandFailure,
    _extract_template,
    _validate_template,
)
from mmdash_worker.jobs.handlers import HandlerContext, HandlerError

MANIFEST = {
    "schema_version": "1.0",
    "name": "test-template",
    "version": "1.0.0",
    "entrypoint": "main.tex",
    "output": "paper.pdf",
    "content_target": "sections/generated-content.tex",
    "bibliography_target": "references.bib",
    "engine": "pdflatex",
    "bibliography_tool": "auto",
}
PINNED_TOOLCHAIN = {
    "pandoc": "pandoc 2.17.1.1",
    "latexmk": "Latexmk Version 4.79",
    "tex_engine": "pdfTeX 3.141592653 (TeX Live 2022/Debian)",
    "engine": "pdflatex",
}


class FakeArticleClient:
    def __init__(self, template_zip: Path) -> None:
        self.template_zip = template_zip
        self.uploads: dict[str, bytes] = {}

    def get_article_build_input(self, _job_id: str) -> dict[str, Any]:
        return {
            "build_id": "build-1",
            "project_id": "project-1",
            "build_kind": "formal",
            "manuscript": "# Paper\n",
            "references_bib": "@misc{ref}\n",
            "article_manifest": {"draft_revision": 3},
            "template": {
                "manifest": MANIFEST,
                "transfer": {"url": "job-scoped", "kind": "template"},
            },
            "engine": "auto",
            "bibliography_tool": "auto",
            "toolchain": {
                "pandoc": "pandoc 2.17.1.1",
                "latexmk": "Version 4.79",
                "texlive": "TeX Live 2022/Debian",
            },
            "limits": {
                "timeout_seconds": 600,
                "memory_bytes": 1024**3,
                "disk_bytes": 2 * 1024**3,
                "output_bytes": 512 * 1024**2,
                "network": "none",
            },
            "resources": [
                {
                    "artifact_id": "artifact-1",
                    "version_id": "version-1",
                    "title": "calibration plot",
                    "filename": "plot.png",
                    "mime_type": "image/png",
                    "size_bytes": len(b"fixed-image"),
                    "sha256": hashlib.sha256(b"fixed-image").hexdigest(),
                    "transfer": {"url": "job-scoped-resource", "kind": "resource"},
                }
            ],
        }

    def download_transfer(
        self, _grant: Mapping[str, Any], destination: Path, *, max_bytes: int
    ) -> dict[str, Any]:
        if _grant.get("kind") == "resource":
            destination.write_bytes(b"fixed-image")
            return {"size_bytes": len(b"fixed-image")}
        assert self.template_zip.stat().st_size <= max_bytes
        destination.write_bytes(self.template_zip.read_bytes())
        return {"size_bytes": self.template_zip.stat().st_size}

    def upload_article_build_output(
        self,
        _job_id: str,
        role: str,
        source: Path,
        *,
        filename: str,
        mime_type: str,
        sha256: str,
        size_bytes: int,
    ) -> dict[str, Any]:
        del filename, mime_type
        contents = source.read_bytes()
        assert hashlib.sha256(contents).hexdigest() == sha256
        assert len(contents) == size_bytes
        self.uploads[role] = contents
        return {"artifact_id": f"artifact-{role}", "version_id": f"version-{role}"}


def test_template_zip_rejects_traversal_symlink_duplicate_and_scripts(tmp_path: Path) -> None:
    for filename, configure in (
        ("../escape.tex", None),
        ("/absolute.tex", None),
        ("link.tex", "symlink"),
    ):
        source = tmp_path / (filename.replace("/", "_") + ".zip")
        with zipfile.ZipFile(source, "w") as archive:
            info = zipfile.ZipInfo(filename)
            if configure == "symlink":
                info.create_system = 3
                info.external_attr = (stat.S_IFLNK | 0o777) << 16
            archive.writestr(info, b"unsafe")
        with pytest.raises(HandlerError) as caught:
            _extract_template(source, tmp_path / (source.stem + "-out"))
        assert caught.value.code == "ARTICLE_TEMPLATE_UNSAFE"

    duplicate = tmp_path / "duplicate.zip"
    with zipfile.ZipFile(duplicate, "w") as archive:
        archive.writestr("Main.tex", b"first")
        archive.writestr("main.tex", b"second")
    with pytest.raises(HandlerError, match="duplicate"):
        _extract_template(duplicate, tmp_path / "duplicate-out")

    template = tmp_path / "template"
    template.mkdir()
    (template / "mmdash-template.json").write_text(json.dumps(MANIFEST), encoding="utf-8")
    (template / "main.tex").write_text("\\documentclass{article}", encoding="utf-8")
    (template / "Makefile").write_text("all:", encoding="utf-8")
    with pytest.raises(HandlerError) as caught:
        _validate_template(template, MANIFEST)
    assert caught.value.code == "ARTICLE_TEMPLATE_SCRIPT_FORBIDDEN"


def test_successful_build_uploads_reproducible_overleaf_zip(tmp_path: Path) -> None:
    template_zip = create_template(tmp_path / "template.zip")
    client = FakeArticleClient(template_zip)

    def fake_command(
        arguments: list[str],
        cwd: Path,
        *,
        timeout: int,
        limits: Mapping[str, int] | None = None,
    ) -> str:
        del timeout, limits
        if arguments[0] == "pandoc":
            assert (
                "--from=markdown+tex_math_dollars+raw_tex+table_captions" in arguments
            )
            output = Path(arguments[arguments.index("--output") + 1])
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_text("Generated TeX", encoding="utf-8")
        else:
            (cwd / "paper.pdf").write_bytes(b"%PDF-1.7\narticle\n")
            (cwd / "main.synctex.gz").write_bytes(b"synctex")
        return "$ " + " ".join(arguments) + "\nok"

    with (
        patch("mmdash_worker.article.handler._run_command", side_effect=fake_command),
        patch("mmdash_worker.article.handler._toolchain", return_value=PINNED_TOOLCHAIN),
    ):
        result = asyncio.run(
            ArticleBuildHandler(client)(HandlerContext(job_id="job-1", worker_id="worker-1"), {})
        )

    assert result["build_id"] == "build-1"
    assert {item["role"] for item in result["outputs"]} == {
        "pdf",
        "tex_source",
        "source_zip",
        "build_report",
        "log",
        "synctex",
    }
    source_zip = tmp_path / "result.zip"
    source_zip.write_bytes(client.uploads["source_zip"])
    with zipfile.ZipFile(source_zip) as archive:
        names = set(archive.namelist())
        assert {
            "main.tex",
            "sections/generated-content.tex",
            "references.bib",
            "figures/",
            "sections/",
            "tables/",
            "mmdash-template.json",
            "paper.pdf",
            "build-manifest.json",
            "CHECKSUMS.sha256",
            "README.md",
        } <= names
        assert all(info.date_time == (1980, 1, 1, 0, 0, 0) for info in archive.infolist())
        checksums = archive.read("CHECKSUMS.sha256").decode()
        assert hashlib.sha256(archive.read("paper.pdf")).hexdigest() in checksums
        assert archive.read("figures/artifact-0001.png") == b"fixed-image"


def test_failed_build_archives_sanitized_log_before_failing(tmp_path: Path) -> None:
    client = FakeArticleClient(create_template(tmp_path / "template.zip"))

    def fail_command(
        _arguments: list[str],
        cwd: Path,
        *,
        timeout: int,
        limits: Mapping[str, int] | None = None,
    ) -> str:
        del timeout, limits
        raise _CommandFailure(
            "ARTICLE_BUILD_FAILED",
            "Article document compilation failed",
            f"{cwd}/private/work/main.tex:1: bad input",
        )

    with (
        patch("mmdash_worker.article.handler._run_command", side_effect=fail_command),
        patch("mmdash_worker.article.handler._toolchain", return_value=PINNED_TOOLCHAIN),
        pytest.raises(HandlerError) as caught,
    ):
        asyncio.run(
            ArticleBuildHandler(client)(HandlerContext(job_id="job-1", worker_id="worker-1"), {})
        )
    assert caught.value.code == "ARTICLE_BUILD_FAILED"
    assert set(client.uploads) == {"log"}
    assert b"mmdash-article-" not in client.uploads["log"]
    assert b"$WORKDIR" in client.uploads["log"]


def test_build_rejects_toolchain_drift_before_running_template(tmp_path: Path) -> None:
    client = FakeArticleClient(create_template(tmp_path / "template.zip"))
    with (
        patch(
            "mmdash_worker.article.handler._toolchain",
            return_value={
                **PINNED_TOOLCHAIN,
                "pandoc": "pandoc 3.0",
            },
        ),
        patch("mmdash_worker.article.handler._run_command") as command,
        pytest.raises(HandlerError) as caught,
    ):
        asyncio.run(
            ArticleBuildHandler(client)(HandlerContext(job_id="job-1", worker_id="worker-1"), {})
        )
    assert caught.value.code == "ARTICLE_TOOLCHAIN_MISMATCH"
    command.assert_not_called()


def create_template(path: Path) -> Path:
    with zipfile.ZipFile(path, "w") as archive:
        archive.writestr("mmdash-template.json", json.dumps(MANIFEST, sort_keys=True))
        archive.writestr(
            "main.tex",
            "\\documentclass{article}\\begin{document}"
            "\\input{sections/generated-content.tex}\\end{document}",
        )
    return path
