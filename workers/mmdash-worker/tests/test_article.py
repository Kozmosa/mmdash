import asyncio
import hashlib
import json
import shutil
import stat
import subprocess
import sys
import zipfile
from collections.abc import Mapping
from contextlib import ExitStack
from pathlib import Path
from typing import Any
from unittest.mock import patch

import pytest
from PIL import Image

from mmdash_worker.article import handler as handler_module
from mmdash_worker.article.handler import (
    ArticleBuildHandler,
    _beautify_longtables,
    _center_standalone_images,
    _CommandFailure,
    _convert_resource_for_latex,
    _extract_template,
    _prepare_native_citations,
    _preserve_image_aspect_ratios,
    _replace_resource_references,
    _resource_filename,
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
        self.progress: list[tuple[int, str]] = []

    def update_article_build_progress(
        self, _job_id: str, progress_percent: int, progress_stage: str
    ) -> dict[str, Any]:
        self.progress.append((progress_percent, progress_stage))
        return {
            "progress_percent": progress_percent,
            "progress_stage": progress_stage,
        }

    def get_article_build_input(self, _job_id: str) -> dict[str, Any]:
        return {
            "build_id": "build-1",
            "project_id": "project-1",
            "build_kind": "formal",
            "manuscript": "# Paper\n",
            "references_bib": "@misc{ref,\n  author = {Author, A.},\n  title = {Title},\n  year = {2026},\n}\n",
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
            assert "--from=markdown+tex_math_dollars+raw_tex+table_captions" in arguments
            assert "--no-highlight" in arguments
            output = Path(arguments[arguments.index("--output") + 1])
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_text("Generated TeX", encoding="utf-8")
        else:
            (cwd / "paper.pdf").write_bytes(b"%PDF-1.7\narticle\n")
            (cwd / "main.synctex.gz").write_bytes(b"synctex")
            cache = cwd / ".cache" / "fontconfig"
            cache.mkdir(parents=True)
            (cache / "generated.cache-8").write_bytes(b"host-specific-cache")
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
    assert [percent for percent, _stage in client.progress] == sorted(
        percent for percent, _stage in client.progress
    )
    assert {stage for _percent, stage in client.progress} == {
        "preparing",
        "resources",
        "converting",
        "compiling",
        "packaging",
        "uploading",
    }
    assert client.progress[-1] == (95, "uploading")
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
        assert not any(name.startswith(".cache/") for name in names)
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


def test_webp_resource_is_converted_to_latex_compatible_png(tmp_path: Path) -> None:
    resource = {
        "filename": "figure.webp",
        "mime_type": "image/webp",
    }
    assert _resource_filename(7, resource) == "artifact-0007.png"
    source = tmp_path / "artifact-0007.png"
    with Image.new("RGBA", (4, 3), (25, 50, 75, 128)) as image:
        image.save(source.with_suffix(".webp"), format="WEBP")
    source.write_bytes(source.with_suffix(".webp").read_bytes())
    source.with_suffix(".webp").unlink()

    _convert_resource_for_latex(source, resource)

    with Image.open(source) as converted:
        assert converted.format == "PNG"
        assert converted.size == (4, 3)
        assert converted.mode == "RGBA"


def test_webp_resource_paths_are_rewritten_even_for_legacy_markdown() -> None:
    resource = {"filename": "figure.webp", "mime_type": "image/webp"}
    manuscript = (
        "![new](mmdash://artifact/artifact-7/versions/version-7)\n"
        "![legacy](figures/artifact-0007.webp)\n"
        "![named](./figures/figure.webp)\n"
    )

    rewritten = _replace_resource_references(
        manuscript, 7, resource, "artifact-7", "version-7", "artifact-0007.png"
    )

    assert rewritten.count("figures/artifact-0007.png") == 3
    assert ".webp" not in rewritten


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


SPLIT_MANIFEST = {
    **MANIFEST,
    "schema_version": "1.1",
    "content_target": "texfile/body.tex",
    "bibliography_mode": "native",
    "body_layout": "single",
}

SPLIT_MANUSCRIPT = "# 问题重述\n\n正文A\n\n## 子节留在本文件\n\n内容\n\n# 模型求解\n\n正文B\n"
SPLIT_HEADINGS = [
    {"block_id": "h1-restatement", "level": 1, "text": "问题重述", "ordinal": 0},
    {"block_id": "h2-sub", "level": 2, "text": "子节留在本文件", "ordinal": 1},
    {"block_id": "h1-solving", "level": 1, "text": "模型求解", "ordinal": 2},
]


class SplitArticleClient(FakeArticleClient):
    def __init__(
        self,
        template_zip: Path,
        split_sections: bool | None = True,
        manifest: dict | None = None,
    ) -> None:
        super().__init__(template_zip)
        self.split_sections = split_sections
        self.manifest = manifest if manifest is not None else SPLIT_MANIFEST

    def get_article_build_input(self, _job_id: str) -> dict[str, Any]:
        build = super().get_article_build_input(_job_id)
        build["manuscript"] = SPLIT_MANUSCRIPT
        build["headings"] = SPLIT_HEADINGS
        if self.split_sections is not None:
            build["split_sections"] = self.split_sections
        build["template"]["manifest"] = self.manifest
        return build


def create_split_template(path: Path, manifest: dict | None = None) -> Path:
    template_manifest = manifest if manifest is not None else SPLIT_MANIFEST
    with zipfile.ZipFile(path, "w") as zipfile_open:
        zipfile_open.writestr("mmdash-template.json", json.dumps(template_manifest, sort_keys=True))
        zipfile_open.writestr(
            "main.tex",
            "\\documentclass{article}\\begin{document}\\input{texfile/body.tex}\\end{document}",
        )
    return path


def run_split_build(tmp_path: Path, client: FakeArticleClient) -> dict[str, bytes]:
    def fake_command(
        arguments: list[str],
        cwd: Path,
        *,
        timeout: int,
        limits: Mapping[str, int] | None = None,
    ) -> str:
        del timeout, limits
        if arguments[0] == "pandoc":
            source = Path(arguments[1])
            output = Path(arguments[arguments.index("--output") + 1])
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_text(
                "% pandoc:" + source.name + "\n" + source.read_text(encoding="utf-8"),
                encoding="utf-8",
            )
        else:
            (cwd / "paper.pdf").write_bytes(b"%PDF-1.7\narticle\n")
        return "$ ok"

    with (
        patch("mmdash_worker.article.handler._run_command", side_effect=fake_command),
        patch("mmdash_worker.article.handler._toolchain", return_value=PINNED_TOOLCHAIN),
    ):
        asyncio.run(
            ArticleBuildHandler(client)(HandlerContext(job_id="job-1", worker_id="worker-1"), {})
        )
    source_zip = tmp_path / "split-result.zip"
    source_zip.write_bytes(client.uploads["source_zip"])
    files: dict[str, bytes] = {}
    with zipfile.ZipFile(source_zip) as archive:
        for name in archive.namelist():
            if not name.endswith("/"):
                files[name] = archive.read(name)
    return files


def test_split_sections_setting_writes_one_tex_file_per_h1(tmp_path: Path) -> None:
    files = run_split_build(
        tmp_path, SplitArticleClient(create_split_template(tmp_path / "template.zip"))
    )

    # One file per H1, named after the contest template convention.
    assert "texfile/1-问题重述.tex" in files
    assert "texfile/2-模型求解.tex" in files
    # H2 headings stay inside their chapter file instead of starting one.
    assert not any(name.startswith("texfile/2-子节") for name in files)
    assert "## 子节留在本文件" in files["texfile/1-问题重述.tex"].decode("utf-8")
    assert files["texfile/2-模型求解.tex"].decode("utf-8").count("正文B") == 1
    # The content target becomes the ordered \input index into main.tex.
    assert files["texfile/body.tex"].decode("utf-8").splitlines() == [
        "\\input{texfile/1-问题重述}",
        "\\input{texfile/2-模型求解}",
    ]


def test_split_sections_opt_out_keeps_single_content_target(tmp_path: Path) -> None:
    sections_manifest = {**SPLIT_MANIFEST, "body_layout": "sections"}
    files = run_split_build(
        tmp_path,
        SplitArticleClient(
            create_split_template(tmp_path / "template.zip", sections_manifest),
            split_sections=False,
            manifest=sections_manifest,
        ),
    )
    body = files["texfile/body.tex"].decode("utf-8")
    assert "# 问题重述" in body
    assert "# 模型求解" in body
    assert not any(name.startswith("texfile/") and name != "texfile/body.tex" for name in files)


def test_split_sections_missing_setting_falls_back_to_manifest(tmp_path: Path) -> None:
    sections_manifest = {**SPLIT_MANIFEST, "body_layout": "sections"}
    files = run_split_build(
        tmp_path,
        SplitArticleClient(
            create_split_template(tmp_path / "template.zip", sections_manifest),
            split_sections=None,
            manifest=sections_manifest,
        ),
    )
    assert "texfile/1-问题重述.tex" in files
    assert files["texfile/body.tex"].decode("utf-8").splitlines()[0] == (
        "\\input{texfile/1-问题重述}"
    )


def test_split_sections_downgrades_for_inline_bibliography(tmp_path: Path) -> None:
    inline_manifest = {**SPLIT_MANIFEST, "bibliography_mode": "inline"}
    inline_zip = tmp_path / "inline-template.zip"
    with zipfile.ZipFile(inline_zip, "w") as archive:
        archive.writestr("mmdash-template.json", json.dumps(inline_manifest, sort_keys=True))
        archive.writestr(
            "main.tex",
            "\\documentclass{article}\\begin{document}\\input{texfile/body.tex}\\end{document}",
        )
    client = SplitArticleClient(inline_zip, manifest=inline_manifest)
    files = run_split_build(tmp_path, client)
    assert "# 问题重述" in files["texfile/body.tex"].decode("utf-8")
    assert not any(name.startswith("texfile/") and name != "texfile/body.tex" for name in files)


def test_native_citations_use_plain_cite_command() -> None:
    rendered = _prepare_native_citations(
        "正文引用 [@rossRadiativeForcingCaused2014; @smithModel2026].",
        {"bibliography_mode": "native", "field_profile": "cumcm"},
    )
    assert rendered == "正文引用 \\cite{rossRadiativeForcingCaused2014,smithModel2026}."


def test_includegraphics_width_keeps_aspect_ratio(tmp_path: Path) -> None:
    fragment = tmp_path / "section.tex"
    fragment.write_text(
        "\\includegraphics[width=0.5\\textwidth,height=\\textheight]{figures/a.jpg}\n"
        "\\includegraphics[height=0.5\\textheight]{figures/b.jpg}\n",
        encoding="utf-8",
    )
    _preserve_image_aspect_ratios(fragment)
    assert fragment.read_text(encoding="utf-8") == (
        "\\includegraphics[width=0.5\\textwidth]{figures/a.jpg}\n"
        "\\includegraphics[height=0.5\\textheight]{figures/b.jpg}\n"
    )


def test_center_standalone_images_without_touching_figures_or_subfigures(tmp_path: Path) -> None:
    fragment = tmp_path / "section.tex"
    fragment.write_text(
        "text before\n\n"
        "\\includegraphics[width=0.45\\linewidth]{figures/single.jpg}\n\n"
        "\\begin{figure}[htbp]\n"
        "\\centering\n"
        "\\includegraphics[width=0.45\\linewidth]{figures/captioned.jpg}\n"
        "\\caption{已有图注}\n"
        "\\end{figure}\n\n"
        "\\begin{subfigure}[b]{0.45\\linewidth}\n"
        "  \\includegraphics[width=\\linewidth]{figures/grouped.jpg}\n"
        "\\end{subfigure}\n",
        encoding="utf-8",
    )

    _center_standalone_images(fragment)

    assert fragment.read_text(encoding="utf-8") == (
        "text before\n\n"
        "{\\centering\n"
        "\\includegraphics[width=0.45\\linewidth]{figures/single.jpg}\\par}\n\n"
        "\\begin{figure}[htbp]\n"
        "\\centering\n"
        "\\includegraphics[width=0.45\\linewidth]{figures/captioned.jpg}\n"
        "\\caption{已有图注}\n"
        "\\end{figure}\n\n"
        "\\begin{subfigure}[b]{0.45\\linewidth}\n"
        "  \\includegraphics[width=\\linewidth]{figures/grouped.jpg}\n"
        "\\end{subfigure}\n"
    )


def create_template(path: Path) -> Path:
    with zipfile.ZipFile(path, "w") as archive:
        archive.writestr("mmdash-template.json", json.dumps(MANIFEST, sort_keys=True))
        archive.writestr(
            "main.tex",
            "\\documentclass{article}\\begin{document}"
            "\\input{sections/generated-content.tex}\\end{document}",
        )
    return path


MANIFEST_11_INLINE = {
    "schema_version": "1.1",
    "name": "inline-abstract-template",
    "version": "1.0.0",
    "entrypoint": "main.tex",
    "output": "paper.pdf",
    "content_target": "generated-content.tex",
    "bibliography_target": "references.bib",
    "engine": "pdflatex",
    "bibliography_tool": "auto",
    "abstract_target": ".mmdash/abstract-block.tex",
    "body_layout": "single",
    "field_profile": "default",
    "bibliography_mode": "inline",
}


def create_inline_template(path: Path) -> Path:
    with zipfile.ZipFile(path, "w") as archive:
        archive.writestr("mmdash-template.json", json.dumps(MANIFEST_11_INLINE, sort_keys=True))
        archive.writestr(
            "main.tex",
            "\\documentclass{article}\\begin{document}"
            "\\input{.mmdash/abstract-block.tex}"
            "\\input{generated-content.tex}\\end{document}",
        )
    return path


class InlineAbstractClient(FakeArticleClient):
    def __init__(
        self,
        template_zip: Path,
        manuscript: str | None = None,
        pandoc_version: str | None = None,
    ) -> None:
        super().__init__(template_zip)
        self.manuscript = manuscript
        self.pandoc_version = pandoc_version
        self.pandoc_commands: list[list[str]] = []

    def get_article_build_input(self, job_id: str) -> dict[str, Any]:
        build = super().get_article_build_input(job_id)
        build["manuscript"] = self.manuscript or "# Paper\n\nSee [@ref].\n"
        build["abstract"] = "Abstract cites [@ref] too.\n"
        build["template"]["manifest"] = MANIFEST_11_INLINE
        if self.pandoc_version:
            build["toolchain"]["pandoc"] = self.pandoc_version
        return build


def test_inline_abstract_citations_render_without_bibliography(tmp_path: Path) -> None:
    client = InlineAbstractClient(create_inline_template(tmp_path / "template.zip"))

    def citeproc_command(
        arguments: list[str],
        cwd: Path,
        *,
        timeout: int,
        limits: Mapping[str, int] | None = None,
    ) -> str:
        del timeout, limits
        if arguments[0] == "pandoc":
            client.pandoc_commands.append(arguments)
            output = Path(arguments[arguments.index("--output") + 1])
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_text(
                "cited text\n"
                "\\protect\\phantomsection\\label{refs}\n"
                "\\begin{CSLReferences}{1}{1}\nbibitem entry\n\\end{CSLReferences}\n",
                encoding="utf-8",
            )
        else:
            (cwd / "paper.pdf").write_bytes(b"%PDF-1.7\narticle\n")
            (cwd / "main.synctex.gz").write_bytes(b"synctex")
        return "$ " + " ".join(arguments) + "\nok"

    with (
        patch("mmdash_worker.article.handler._run_command", side_effect=citeproc_command),
        patch("mmdash_worker.article.handler._toolchain", return_value=PINNED_TOOLCHAIN),
    ):
        asyncio.run(
            ArticleBuildHandler(client)(HandlerContext(job_id="job-1", worker_id="worker-1"), {})
        )

    outputs = {Path(c[c.index("--output") + 1]).name: c for c in client.pandoc_commands}
    assert set(outputs) == {"generated-content.tex", "abstract-block.tex"}
    for command in client.pandoc_commands:
        assert "--no-highlight" in command
        assert "--citeproc" in command

    source_zip = tmp_path / "result.zip"
    source_zip.write_bytes(client.uploads["source_zip"])
    with zipfile.ZipFile(source_zip) as archive:
        abstract = archive.read(".mmdash/abstract-block.tex").decode()
        body = archive.read("generated-content.tex").decode()
    assert abstract == "cited text\n"
    assert "CSLReferences" not in abstract
    assert "phantomsection" not in abstract
    # The body fragment keeps the reference list and gains the compatibility
    # definitions the non-standalone output needs.
    assert "cited text" in body
    assert "\\begin{CSLReferences}{1}{1}" in body
    assert body.startswith("% mmdash Pandoc")


def test_pandoc_fragment_stays_within_the_template_contract(tmp_path: Path) -> None:
    if shutil.which("pandoc") is None:
        pytest.skip("pandoc is not installed; run inside the worker image for coverage")
    manuscript = (
        "# Paper\n\n"
        "Code stays plain [@ref] with math $x_i^2$.\n\n"
        "```python\nimport numpy as np\n```\n\n"
        "| a | b |\n| --- | --- |\n| 1 | 2 |\n"
    )
    template = create_inline_template(tmp_path / "template.zip")
    real_run_command = handler_module._run_command

    def real_pandoc_command(
        arguments: list[str],
        cwd: Path,
        *,
        timeout: int,
        limits: Mapping[str, int] | None = None,
    ) -> str:
        if arguments[0] == "pandoc":
            return real_run_command(arguments, cwd, timeout=timeout, limits=limits)
        (cwd / "paper.pdf").write_bytes(b"%PDF-1.7\narticle\n")
        (cwd / "main.synctex.gz").write_bytes(b"synctex")
        return "$ " + " ".join(arguments) + "\nok (simulated latexmk)\n"

    pandoc_version = subprocess.run(
        ["pandoc", "--version"], capture_output=True, text=True, check=True
    ).stdout.splitlines()[0]
    toolchain = {
        "pandoc": pandoc_version,
        "latexmk": "Latexmk Version 4.79",
        "tex_engine": "pdfTeX 3.141592653 (TeX Live 2022/Debian)",
        "engine": "pdflatex",
    }

    def real_toolchain(engine: str) -> dict[str, str]:
        return {**toolchain, "engine": engine}

    patches = [
        patch("mmdash_worker.article.handler._run_command", side_effect=real_pandoc_command),
        patch("mmdash_worker.article.handler._toolchain", side_effect=real_toolchain),
    ]
    if sys.platform == "darwin":
        # setrlimit(RLIMIT_AS) raises EINVAL on macOS; limits stay enforced in
        # the Linux worker image.
        patches.append(patch("mmdash_worker.article.handler._limit_process", lambda limits: None))
    with ExitStack() as stack:
        for patcher in patches:
            stack.enter_context(patcher)
        client = InlineAbstractClient(template, manuscript=manuscript, pandoc_version="pandoc")
        asyncio.run(
            ArticleBuildHandler(client)(HandlerContext(job_id="job-1", worker_id="worker-1"), {})
        )

    source_zip = tmp_path / "contract.zip"
    source_zip.write_bytes(client.uploads["source_zip"])
    with zipfile.ZipFile(source_zip) as archive:
        fragments = {
            name: archive.read(name).decode()
            for name in ("generated-content.tex", ".mmdash/abstract-block.tex")
        }
    for name, text in fragments.items():
        for banned in (
            "\\begin{Shaded}",
            "\\begin{Highlighting}",
            "\\ImportTok",
            "\\pandocbounded",
            "{[}@",
        ):
            assert banned not in text, f"{banned} leaked into {name}"
    assert "\\begin{verbatim}" in fragments["generated-content.tex"]
    assert "CSLReferences" not in fragments[".mmdash/abstract-block.tex"]
    assert "CSLReferences" in fragments["generated-content.tex"]


def test_beautify_longtables_centers_and_bolds_contest_headers(tmp_path: Path) -> None:
    fragment = tmp_path / "fragment.tex"
    fragment.write_text(
        "\\begin{longtable}[]{@{}ccc@{}}\n"
        "\\caption{符号说明}\\tabularnewline\n"
        "\\toprule\n"
        "符号 & 意义 & 单位 \\\\\n"
        "\\midrule\n"
        "\\endfirsthead\n"
        "\\toprule\n"
        "符号 & 意义 & 单位 \\\\\n"
        "\\midrule\n"
        "\\endhead\n"
        "\\(t\\) & 当前时间 & s \\\\\n"
        "\\bottomrule\n"
        "\\end{longtable}\n",
        encoding="utf-8",
    )

    _beautify_longtables(fragment)

    body = fragment.read_text(encoding="utf-8")
    # A narrower-than-textwidth longtable must center on the page.
    assert (
        body.count("\\setlength\\LTleft{\\fill}\\setlength\\LTright{\\fill}\n\\begin{longtable}")
        == 1
    )
    # Both the first-page header and the page-continuation header are bold.
    assert body.count("\\textbf{符号} & \\textbf{意义} & \\textbf{单位} \\\\") == 2
    # Body rows stay untouched.
    assert "\\(t\\) & 当前时间 & s \\\\" in body
    assert "\\textbf{\\(t\\)}" not in body


def test_beautify_longtables_keeps_author_bolding_and_skips_plain_fragments(
    tmp_path: Path,
) -> None:
    bolded = tmp_path / "bolded.tex"
    bolded.write_text(
        "\\begin{longtable}[]{@{}cc@{}}\n"
        "\\toprule\n"
        "\\textbf{方法} & \\\\\n"
        "\\midrule\n"
        "\\endhead\n"
        "A & B \\\\\n"
        "\\bottomrule\n"
        "\\end{longtable}\n",
        encoding="utf-8",
    )
    _beautify_longtables(bolded)
    body = bolded.read_text(encoding="utf-8")
    assert "\\textbf{\\textbf{方法}}" not in body
    assert body.count("\\textbf{方法}") == 1

    plain = tmp_path / "plain.tex"
    plain.write_text("no tables here\n", encoding="utf-8")
    _beautify_longtables(plain)
    assert plain.read_text(encoding="utf-8") == "no tables here\n"
