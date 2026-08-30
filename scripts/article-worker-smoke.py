"""Run the real, network-isolated Article toolchain from the Worker image."""

from __future__ import annotations

import asyncio
import base64
import hashlib
import json
import tempfile
import zipfile
from collections.abc import Mapping
from pathlib import Path
from typing import Any

from mmdash_worker.article import ArticleBuildHandler
from mmdash_worker.jobs.handlers import HandlerContext

MANIFEST = {
    "schema_version": "1.0",
    "name": "mmdash-article-smoke",
    "version": "1.0.0",
    "entrypoint": "main.tex",
    "output": "paper.pdf",
    "content_target": "sections/generated-content.tex",
    "bibliography_target": "references.bib",
    "engine": "pdflatex",
    "bibliography_tool": "bibtex",
}
PNG = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUB"
    "AScY42YAAAAASUVORK5CYII="
)


class SmokeClient:
    def __init__(self, template: Path, output: Path) -> None:
        self.template = template
        self.output = output

    def update_article_build_progress(
        self, _job_id: str, progress_percent: int, progress_stage: str
    ) -> dict[str, Any]:
        return {
            "progress_percent": progress_percent,
            "progress_stage": progress_stage,
        }

    def get_article_build_input(self, _job_id: str) -> dict[str, Any]:
        resource_hash = hashlib.sha256(PNG).hexdigest()
        return {
            "build_id": "article-worker-smoke",
            "project_id": "article-worker-smoke",
            "build_kind": "formal",
            "manuscript": (
                "# Reproducible article smoke\n\n"
                "This paragraph is built with the frozen bibliography below.\n\n"
                "![A fixed Artifact image](mmdash://artifact/artifact-smoke/versions/version-smoke)\n"
            ),
            "references_bib": (
                "@article{doe2026,\n"
                "  author = {Doe, Jane},\n"
                "  title = {A Reproducible Result},\n"
                "  journal = {Journal of Smoke Tests},\n"
                "  year = {2026}\n"
                "}\n"
            ),
            "article_manifest": {
                "commit_id": "commit-smoke",
                "git_commit_sha": "0000000000000000000000000000000000000000",
                "template_version_id": "template-version-smoke",
                "resources": [
                    {
                        "artifact_id": "artifact-smoke",
                        "version_id": "version-smoke",
                        "sha256": resource_hash,
                    }
                ],
            },
            "template": {
                "manifest": MANIFEST,
                "transfer": {"kind": "template", "url": "job-scoped-template"},
            },
            "engine": "pdflatex",
            "bibliography_tool": "bibtex",
            "toolchain": {
                "pandoc": "pandoc 2.17.1.1",
                "latexmk": "Version 4.79",
                "texlive": "TeX Live 2022/Debian",
            },
            "limits": {
                "timeout_seconds": 180,
                "memory_bytes": 1024**3,
                "disk_bytes": 2 * 1024**3,
                "output_bytes": 64 * 1024**2,
                "network": "none",
            },
            "resources": [
                {
                    "artifact_id": "artifact-smoke",
                    "version_id": "version-smoke",
                    "title": "fixed smoke figure",
                    "filename": "smoke.png",
                    "mime_type": "image/png",
                    "size_bytes": len(PNG),
                    "sha256": resource_hash,
                    "transfer": {"kind": "resource", "url": "job-scoped-resource"},
                }
            ],
        }

    def download_transfer(
        self, grant: Mapping[str, Any], destination: Path, *, max_bytes: int
    ) -> dict[str, Any]:
        contents = (
            PNG if grant.get("kind") == "resource" else self.template.read_bytes()
        )
        if len(contents) > max_bytes:
            raise RuntimeError("smoke transfer exceeded its job-scoped grant")
        destination.write_bytes(contents)
        return {"size_bytes": len(contents)}

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
        del mime_type
        contents = source.read_bytes()
        if (
            len(contents) != size_bytes
            or hashlib.sha256(contents).hexdigest() != sha256
        ):
            raise RuntimeError(f"{role} output digest changed during upload")
        (self.output / filename).write_bytes(contents)
        return {
            "artifact_id": f"artifact-{role}",
            "version_id": f"version-{role}",
        }


def create_template(path: Path) -> None:
    document = r"""\documentclass{article}
\usepackage{graphicx}
\usepackage{hyperref}
\begin{document}
\input{sections/generated-content.tex}
\nocite{*}
\bibliographystyle{plain}
\bibliography{references}
\end{document}
"""
    with zipfile.ZipFile(path, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        archive.writestr("mmdash-template.json", json.dumps(MANIFEST, sort_keys=True))
        archive.writestr("main.tex", document)


def verify(output: Path, result: Mapping[str, Any]) -> dict[str, Any]:
    roles = {str(item["role"]) for item in result["outputs"]}
    required = {"pdf", "tex_source", "source_zip", "build_report", "log", "synctex"}
    if roles != required:
        raise RuntimeError(f"unexpected Article output roles: {sorted(roles)}")
    pdf = output / "paper.pdf"
    if not pdf.read_bytes().startswith(b"%PDF-") or pdf.stat().st_size < 1_000:
        raise RuntimeError("Article smoke did not produce a valid PDF")
    log = (output / "build.log").read_text(encoding="utf-8").casefold()
    if "bibtex" not in log:
        raise RuntimeError("latexmk did not execute the BibTeX pass")
    report = json.loads((output / "build-report.json").read_text(encoding="utf-8"))
    if report["toolchain"]["pandoc"] != "pandoc 2.17.1.1":
        raise RuntimeError("build report did not freeze the Pandoc version")
    with zipfile.ZipFile(output / "article-source.zip") as archive:
        names = set(archive.namelist())
        expected = {
            "main.tex",
            "manuscript.md",
            "references.bib",
            "figures/artifact-0001.png",
            "paper.pdf",
            "build-manifest.json",
            "CHECKSUMS.sha256",
        }
        if not expected <= names:
            raise RuntimeError(
                f"source ZIP is missing files: {sorted(expected - names)}"
            )
        if archive.read("figures/artifact-0001.png") != PNG:
            raise RuntimeError("source ZIP did not preserve the frozen Artifact image")
        if not all(
            info.date_time == (1980, 1, 1, 0, 0, 0) for info in archive.infolist()
        ):
            raise RuntimeError("source ZIP is not reproducibly timestamped")
    return {
        "status": "passed",
        "roles": sorted(roles),
        "pdf_bytes": pdf.stat().st_size,
        "source_zip_bytes": (output / "article-source.zip").stat().st_size,
        "toolchain": report["toolchain"],
        "network": "none",
    }


def main() -> None:
    with tempfile.TemporaryDirectory(
        prefix="mmdash-article-worker-smoke-"
    ) as temporary:
        root = Path(temporary)
        output = root / "output"
        output.mkdir()
        template = root / "template.zip"
        create_template(template)
        try:
            result = asyncio.run(
                ArticleBuildHandler(SmokeClient(template, output))(
                    HandlerContext(
                        job_id="article-worker-smoke", worker_id="article-worker-smoke"
                    ),
                    {},
                )
            )
        except Exception:
            build_log = output / "build.log"
            if build_log.is_file():
                print(build_log.read_text(encoding="utf-8"))
            raise
        print(json.dumps(verify(output, result), ensure_ascii=False, sort_keys=True))


if __name__ == "__main__":
    main()
