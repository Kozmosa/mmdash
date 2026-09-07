"""Job-scoped Markdown -> LaTeX -> PDF Article build pipeline."""

from __future__ import annotations

import asyncio
import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import tempfile
import zipfile
from collections.abc import Mapping
from functools import partial
from pathlib import Path, PurePosixPath
from typing import Any, Protocol

from PIL import Image, ImageOps, UnidentifiedImageError

try:
    import resource
except ImportError:  # pragma: no cover - Windows unit-test host
    resource = None  # type: ignore[assignment]

from mmdash_worker.jobs.handlers import HandlerContext, HandlerError

MAX_TEMPLATE_BYTES = 128 * 1024 * 1024
MAX_TEMPLATE_FILES = 5_000
MAX_TEMPLATE_EXPANDED_BYTES = 512 * 1024 * 1024
MAX_OUTPUT_BYTES = 512 * 1024 * 1024
FIXED_ZIP_TIME = (1980, 1, 1, 0, 0, 0)
FORBIDDEN_TEMPLATE_NAMES = {".latexmkrc", "latexmkrc", "makefile"}
FORBIDDEN_TEMPLATE_SUFFIXES = {
    ".bat",
    ".cmd",
    ".com",
    ".dll",
    ".exe",
    ".jar",
    ".js",
    ".pl",
    ".ps1",
    ".py",
    ".rb",
    ".sh",
}
LATEX_NATIVE_IMAGE_SUFFIXES = {".jpeg", ".jpg", ".pdf", ".png"}
LATEX_PNG_IMAGE_SUFFIXES = {
    ".avif",
    ".bmp",
    ".gif",
    ".heic",
    ".heif",
    ".ico",
    ".j2c",
    ".jp2",
    ".jpf",
    ".jpx",
    ".tif",
    ".tiff",
    ".webp",
}


PANDOC_CSL_COMPATIBILITY = r"""% mmdash Pandoc 2.17.1.1 citeproc compatibility
\providecommand{\citeproctext}{}
\providecommand{\citeproc}[2]{%
  \begingroup\def\citeproctext{#2}\cite{#1}\endgroup}
\providecommand{\phantomsection}{}

\makeatletter
% Match Pandoc 2.17.1.1's paragraph-based CSLReferences environment.
% Do not use a list environment here: Pandoc 2.17 citeproc bibliography
% entries are paragraphs and do not necessarily contain \item.
\@ifundefined{cslhangindent}{\newlength{\cslhangindent}}{}
\setlength{\cslhangindent}{1.5em}
\@ifundefined{csllabelwidth}{\newlength{\csllabelwidth}}{}
\setlength{\csllabelwidth}{3em}
\@ifundefined{cslentryspacingunit}{\newlength{\cslentryspacingunit}}{}
\setlength{\cslentryspacingunit}{\parskip}
\@ifundefined{CSLReferences}{%
  \newenvironment{CSLReferences}[2]
   {%
    \setlength{\parindent}{0pt}%
    \ifodd #1
      \let\oldpar\par
      \def\par{\hangindent=\cslhangindent\oldpar}%
    \fi
    \setlength{\parskip}{#2\cslentryspacingunit}%
   }
   {}
}{}
\makeatother

\providecommand{\CSLBlock}[1]{#1\hfill\break}
\providecommand{\CSLLeftMargin}[1]{%
  \parbox[t]{\csllabelwidth}{#1}}
\providecommand{\CSLRightInline}[1]{%
  \parbox[t]{\dimexpr\linewidth-\csllabelwidth\relax}{#1}\break}
\providecommand{\CSLIndent}[1]{\hspace{\cslhangindent}#1}
"""


class ArticleClient(Protocol):
    def get_article_build_input(self, job_id: str) -> dict[str, Any]: ...
    def update_article_build_progress(
        self, job_id: str, progress_percent: int, progress_stage: str
    ) -> dict[str, Any]: ...
    def download_transfer(
        self, grant: Mapping[str, Any], destination: Path, *, max_bytes: int
    ) -> dict[str, Any]: ...
    def upload_article_build_output(
        self,
        job_id: str,
        role: str,
        source: Path,
        *,
        filename: str,
        mime_type: str,
        sha256: str,
        size_bytes: int,
    ) -> dict[str, Any]: ...


class ArticleBuildHandler:
    def __init__(self, client: ArticleClient) -> None:
        self.client = client

    async def __call__(
        self, context: HandlerContext, payload: Mapping[str, Any]
    ) -> Mapping[str, Any]:
        del payload
        return await asyncio.to_thread(self._run, context)

    def _run(self, context: HandlerContext) -> Mapping[str, Any]:
        if context.cancellation_requested:
            raise HandlerError("JOB_CANCELLED", "Article build was cancelled")
        build = self.client.get_article_build_input(context.job_id)
        _validate_input(build)
        self.client.update_article_build_progress(context.job_id, 10, "preparing")
        limits = _limits(build)
        with tempfile.TemporaryDirectory(prefix="mmdash-article-") as temporary:
            # Resolve once so _safe_child outputs (always resolved) stay
            # relative_to-compatible with template_root on hosts where the
            # temp dir lives behind a symlink (macOS /var -> /private/var).
            root = Path(temporary).resolve()
            template_zip = root / "template.zip"
            template = _mapping(build["template"])
            self.client.download_transfer(
                _mapping(template["transfer"]), template_zip, max_bytes=MAX_TEMPLATE_BYTES
            )
            template_root = root / "template"
            _extract_template(template_zip, template_root)
            manifest = _mapping(template["manifest"])
            _validate_template(template_root, manifest)
            self.client.update_article_build_progress(context.job_id, 20, "resources")
            manuscript = root / "manuscript.md"
            bibliography = root / "references.bib"
            article_manifest = root / "article.json"
            manuscript.write_text(str(build["manuscript"]), encoding="utf-8", newline="\n")
            bibliography.write_text(str(build["references_bib"]), encoding="utf-8", newline="\n")
            article_manifest.write_text(
                json.dumps(build["article_manifest"], ensure_ascii=False, sort_keys=True, indent=2)
                + "\n",
                encoding="utf-8",
                newline="\n",
            )
            figure_dir = str(manifest.get("figure_dir", "figures") or "figures")
            resources = template_root / figure_dir
            resources.mkdir(parents=True, exist_ok=True)
            manuscript_text = manuscript.read_text(encoding="utf-8")
            for index, raw_resource in enumerate(build.get("resources", []), start=1):
                resource = _mapping(raw_resource)
                transfer = _mapping(resource["transfer"])
                artifact_id = _required(resource, "artifact_id")
                version_id = _required(resource, "version_id")
                filename = _resource_filename(index, resource)
                destination = resources / filename
                max_bytes = min(int(resource["size_bytes"]), MAX_OUTPUT_BYTES)
                downloaded = self.client.download_transfer(
                    transfer, destination, max_bytes=max_bytes
                )
                if int(downloaded.get("size_bytes", -1)) != int(resource["size_bytes"]):
                    raise HandlerError("ARTICLE_RESOURCE_CHANGED", "Article resource size changed")
                if _sha256(destination) != str(resource["sha256"]):
                    raise HandlerError("ARTICLE_RESOURCE_CHANGED", "Article resource hash changed")
                _convert_resource_for_latex(destination, resource)
                manuscript_text = _replace_resource_references(
                    manuscript_text,
                    index,
                    resource,
                    artifact_id,
                    version_id,
                    filename,
                    figure_dir,
                )
            manuscript.write_text(manuscript_text, encoding="utf-8", newline="\n")
            self.client.update_article_build_progress(context.job_id, 35, "converting")
            content_target = _safe_child(template_root, _required(manifest, "content_target"))
            bibliography_target = _safe_child(
                template_root, _required(manifest, "bibliography_target")
            )
            entrypoint = _safe_child(template_root, _required(manifest, "entrypoint"))
            expected_pdf = _safe_child(template_root, _required(manifest, "output"))
            content_target.parent.mkdir(parents=True, exist_ok=True)
            bibliography_target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(bibliography, bibliography_target)
            raw_abstract = str(build.get("abstract", ""))
            raw_paper_fields = _raw_paper_fields(build)
            if "abstract" in raw_paper_fields:
                abstract_enabled = raw_paper_fields["abstract"].get("enabled") is True
            else:
                abstract_enabled = bool(raw_abstract.strip())
            _write_metadata_blocks(template_root, manifest, _paper_fields(build), abstract_enabled)
            body_layout = str(manifest.get("body_layout", "single"))
            bibliography_mode = str(manifest.get("bibliography_mode", "inline"))
            if body_layout == "sections" and bibliography_mode == "inline":
                # citeproc must render the reference list once per document;
                # a per-section split would duplicate or lose it. Sections are
                # therefore only split for native-bibliography templates.
                body_layout = "single"
            _inject_bibliography_block(
                template_root,
                manifest,
                entrypoint.read_text(encoding="utf-8", errors="replace"),
                bibliography_target,
            )
            pandoc_runs: list[tuple[str, Path]] = []
            if body_layout == "sections":
                chunks = _split_markdown_sections(
                    manuscript_text, [_mapping(h) for h in build.get("headings", [])]
                )
                input_lines: list[str] = []
                front_chunks: list[str] = []
                for block_id, chunk in chunks:
                    if not block_id:
                        front_chunks.append(chunk)
                        continue
                    section_path = content_target.parent / f"{_section_filename(block_id)}.tex"
                    if section_path == content_target:
                        front_chunks.append(chunk)
                        continue
                    pandoc_runs.append((chunk, section_path))
                    input_lines.append(
                        "\\input{{{}}}".format(
                            section_path.relative_to(template_root).with_suffix("").as_posix()
                        )
                    )
                if front_chunks or not input_lines:
                    pandoc_runs.append(("\n\n".join(front_chunks), content_target))
                content_target.write_text(
                    "\n".join(input_lines) + "\n", encoding="utf-8", newline="\n"
                )
            else:
                pandoc_runs.append((manuscript_text, content_target))
            abstract_declared = manifest.get("abstract_target")
            abstract_path: Path | None = None
            if abstract_declared and abstract_enabled and raw_abstract.strip():
                abstract_path = _safe_child(template_root, str(abstract_declared))
                abstract_path.parent.mkdir(parents=True, exist_ok=True)
                pandoc_runs.append((raw_abstract, abstract_path))
            elif abstract_declared:
                abstract_path = _safe_child(template_root, str(abstract_declared))
                abstract_path.parent.mkdir(parents=True, exist_ok=True)
                abstract_path.write_text("", encoding="utf-8", newline="\n")

            log_parts: list[str] = []
            engine = _engine(build, manifest)
            _verify_toolchain(build, engine)
            latexmk_mode = {"pdflatex": "-pdf", "xelatex": "-xelatex", "lualatex": "-lualatex"}[
                engine
            ]
            latexmk = [
                "latexmk",
                latexmk_mode,
                "-interaction=nonstopmode",
                "-halt-on-error",
                "-file-line-error",
                "-synctex=1",
                "-recorder",
                "-no-shell-escape",
                entrypoint.name,
            ]
            if str(build["bibliography_tool"]) == "none":
                latexmk.insert(-1, "-bibtex-")
            try:
                for run_index, (source_text, output_path) in enumerate(pandoc_runs):
                    source_file = root / f"chunk-{run_index:04d}.md"
                    source_file.write_text(source_text, encoding="utf-8", newline="\n")
                    command = [
                        "pandoc",
                        str(source_file),
                        # Pandoc's GFM reader explicitly disables table_captions; the
                        # default Markdown reader keeps GFM-compatible pipe tables and
                        # supports the `Table:` captions emitted by Core.
                        "--from=markdown+tex_math_dollars+raw_tex+table_captions",
                        "--to=latex",
                        # Syntax highlighting would emit Shaded/Highlighting and
                        # \ImportTok-style commands whose definitions live only in
                        # Pandoc's standalone template; a plain verbatim block keeps
                        # the fragment self-contained. Code-block languages remain an
                        # editor concern only.
                        "--no-highlight",
                        "--wrap=none",
                        "--resource-path",
                        str(template_root),
                        "--output",
                        str(output_path),
                    ]
                    is_citeproc_target = output_path in (content_target, abstract_path)
                    if (
                        is_citeproc_target
                        and bibliography.stat().st_size
                        and bibliography_mode != "native"
                    ):
                        command.extend(["--citeproc", "--bibliography", str(bibliography)])
                    log_parts.append(
                        _run_command(
                            command,
                            root,
                            timeout=min(300, limits["timeout_seconds"]),
                            limits=limits,
                        )
                    )
                if (
                    bibliography.stat().st_size
                    and bibliography_mode != "native"
                    and any(path == content_target for _, path in pandoc_runs)
                ):
                    _inject_pandoc_citeproc_compatibility(content_target)
                if (
                    bibliography.stat().st_size
                    and bibliography_mode != "native"
                    and any(path == abstract_path for _, path in pandoc_runs)
                ):
                    # The abstract renders citations as inline text but never owns
                    # the reference list: it belongs to the body fragment only.
                    _strip_csl_references(abstract_path)
                _check_disk(root, limits["disk_bytes"])
                self.client.update_article_build_progress(context.job_id, 55, "compiling")
                log_parts.append(
                    _run_command(
                        latexmk,
                        entrypoint.parent,
                        timeout=limits["timeout_seconds"],
                        limits=limits,
                    )
                )
                _check_disk(root, limits["disk_bytes"])
            except _CommandFailure as error:
                build_log = root / "build.log"
                build_log.write_text(
                    _sanitize_log("\n\n".join([*log_parts, error.log]), root),
                    encoding="utf-8",
                    newline="\n",
                )
                _upload_output(
                    self.client, context.job_id, "log", build_log, "build.log", "text/plain"
                )
                raise HandlerError(error.code, str(error), retryable=error.retryable) from error
            if not expected_pdf.is_file():
                fallback = entrypoint.with_suffix(".pdf")
                if fallback.is_file() and fallback != expected_pdf:
                    expected_pdf.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(fallback, expected_pdf)
                else:
                    raise HandlerError(
                        "ARTICLE_PDF_MISSING", "LaTeX did not produce the declared PDF"
                    )
            build_log = root / "build.log"
            build_log.write_text(
                _sanitize_log("\n\n".join(log_parts), root), encoding="utf-8", newline="\n"
            )
            report = root / "build-report.json"
            toolchain = _toolchain(engine)
            report.write_text(
                json.dumps(
                    {
                        "schema_version": "1.0",
                        "build_id": build["build_id"],
                        "build_kind": build["build_kind"],
                        "engine": engine,
                        "bibliography_tool": build["bibliography_tool"],
                        "toolchain": toolchain,
                        "source_files": sorted(
                            source.relative_to(template_root).as_posix()
                            for source in template_root.rglob("*")
                            if source.is_file()
                        ),
                    },
                    ensure_ascii=False,
                    sort_keys=True,
                    indent=2,
                )
                + "\n",
                encoding="utf-8",
                newline="\n",
            )
            self.client.update_article_build_progress(context.job_id, 75, "packaging")
            source_zip = root / "article-source.zip"
            _create_source_zip(
                source_zip,
                manuscript,
                bibliography,
                article_manifest,
                template_root,
                expected_pdf,
                entrypoint,
                report,
                build,
                toolchain,
            )
            _check_disk(root, limits["disk_bytes"])
            outputs = [
                ("pdf", expected_pdf, expected_pdf.name, "application/pdf"),
                ("tex_source", entrypoint, entrypoint.name, "application/x-tex"),
                ("source_zip", source_zip, "article-source.zip", "application/zip"),
                ("build_report", report, "build-report.json", "application/json"),
                ("log", build_log, "build.log", "text/plain"),
            ]
            synctex = entrypoint.with_suffix(".synctex.gz")
            if synctex.is_file():
                outputs.append(("synctex", synctex, synctex.name, "application/gzip"))
            uploaded: list[dict[str, Any]] = []
            self.client.update_article_build_progress(context.job_id, 85, "uploading")
            for index, (role, source, filename, mime_type) in enumerate(outputs):
                if context.cancellation_requested:
                    raise HandlerError("JOB_CANCELLED", "Article build was cancelled")
                if source.stat().st_size > limits["output_bytes"]:
                    raise HandlerError("ARTICLE_OUTPUT_LIMIT", "Article build output is too large")
                uploaded.append(
                    _upload_output(self.client, context.job_id, role, source, filename, mime_type)
                )
                self.client.update_article_build_progress(
                    context.job_id,
                    min(95, 85 + round(((index + 1) / len(outputs)) * 10)),
                    "uploading",
                )
            return {"build_id": build["build_id"], "outputs": uploaded, "toolchain": toolchain}


def _validate_input(value: Mapping[str, Any]) -> None:
    for field in (
        "build_id",
        "project_id",
        "build_kind",
        "manuscript",
        "references_bib",
        "engine",
        "bibliography_tool",
    ):
        if not isinstance(value.get(field), str):
            raise HandlerError("ARTICLE_BUILD_INVALID_INPUT", "Article build input is invalid")
    if value["build_kind"] not in {"preview", "formal", "template_test"}:
        raise HandlerError("ARTICLE_BUILD_INVALID_INPUT", "Article build kind is invalid")
    if not isinstance(value.get("article_manifest"), Mapping) or not isinstance(
        value.get("template"), Mapping
    ):
        raise HandlerError("ARTICLE_BUILD_INVALID_INPUT", "Article build manifest is invalid")
    if not isinstance(value.get("abstract", ""), str):
        raise HandlerError("ARTICLE_BUILD_INVALID_INPUT", "Article build input is invalid")
    if value.get("paper_info") is not None and not isinstance(value.get("paper_info"), Mapping):
        raise HandlerError("ARTICLE_BUILD_INVALID_INPUT", "Article paper info is invalid")
    headings = value.get("headings", [])
    if not isinstance(headings, list) or len(headings) > 2000:
        raise HandlerError("ARTICLE_BUILD_INVALID_INPUT", "Article headings are invalid")
    resources = value.get("resources", [])
    if not isinstance(resources, list) or len(resources) > 500:
        raise HandlerError("ARTICLE_BUILD_INVALID_INPUT", "Article resources are invalid")


def _inject_pandoc_citeproc_compatibility(content_target: Path) -> None:
    """Make a non-standalone Pandoc LaTeX fragment self-contained for citeproc.

    `pandoc --to=latex --citeproc` emits `CSLReferences`, `citeproctext`,
    `CSLBlock`, and related commands in the body, while their definitions live
    in Pandoc's standalone LaTeX template. mmdash intentionally generates only
    a fragment and then includes it from the selected Article template, so we
    must carry the citeproc compatibility definitions with that fragment.
    """
    try:
        generated = content_target.read_text(encoding="utf-8")
        content_target.write_text(
            PANDOC_CSL_COMPATIBILITY + "\n" + generated,
            encoding="utf-8",
            newline="\n",
        )
    except OSError as error:
        raise HandlerError(
            "ARTICLE_BUILD_FAILED",
            "Pandoc citation output could not be prepared for LaTeX",
        ) from error


def _strip_csl_references(target: Path) -> None:
    r"""Remove the citeproc reference list from a non-body fragment.

    `--citeproc` appends `\phantomsection\label{refs}` plus a `CSLReferences`
    environment to any fragment that cites something. The abstract must render
    citations as inline text while the reference list stays in the body
    fragment, so the trailing bibliography block is cut from abstract output.
    """
    try:
        generated = target.read_text(encoding="utf-8")
        stripped = _CSL_REFERENCES_BLOCK.sub("", generated).rstrip() + "\n"
        target.write_text(stripped, encoding="utf-8", newline="\n")
    except OSError as error:
        raise HandlerError(
            "ARTICLE_BUILD_FAILED",
            "Pandoc citation output could not be prepared for LaTeX",
        ) from error


_CSL_REFERENCES_BLOCK = re.compile(
    r"(?:\\protect)?\\phantomsection\\label\{refs\}\s*"
    r"\\begin\{CSLReferences\}\{[0-9]+\}\{[0-9]+\}.*?\\end\{CSLReferences\}\s*",
    re.DOTALL,
)


def _resource_filename(index: int, resource: Mapping[str, Any]) -> str:
    source = PurePosixPath(str(resource.get("filename", "resource.bin"))).name
    suffix = PurePosixPath(source).suffix.casefold()[:16]
    if _resource_requires_png(resource):
        suffix = ".png"
    return f"artifact-{index:04d}{suffix}"


def _resource_requires_png(resource: Mapping[str, Any]) -> bool:
    source = PurePosixPath(str(resource.get("filename", "resource.bin"))).suffix.casefold()
    mime_type = str(resource.get("mime_type", "")).split(";", 1)[0].casefold()
    return source in LATEX_PNG_IMAGE_SUFFIXES or mime_type in {
        "image/avif",
        "image/bmp",
        "image/gif",
        "image/heic",
        "image/heif",
        "image/webp",
    }


def _replace_resource_references(
    manuscript: str,
    index: int,
    resource: Mapping[str, Any],
    artifact_id: str,
    version_id: str,
    filename: str,
    figure_dir: str = "figures",
) -> str:
    prefix = f"{figure_dir.strip('/')}/"
    replacement = f"{prefix}{filename}"
    references = {
        f"mmdash://artifact/{artifact_id}/versions/{version_id}",
    }
    source = PurePosixPath(str(resource.get("filename", "resource.bin"))).name
    source_suffix = PurePosixPath(source).suffix.casefold()[:16]
    legacy = f"artifact-{index:04d}{source_suffix}"
    for candidate in {source, legacy}:
        references.update({f"figures/{candidate}", f"./figures/{candidate}"})
    for reference in references:
        manuscript = manuscript.replace(reference, replacement)
    return manuscript


def _convert_resource_for_latex(destination: Path, resource: Mapping[str, Any]) -> None:
    """Convert raster formats unsupported by graphicx/XeLaTeX to PNG.

    The resource hash is checked before this function runs, so the conversion
    cannot mask a changed transfer. The generated PNG is then the canonical
    resource included in the manuscript and reproducible source ZIP.
    """
    if not _resource_requires_png(resource):
        return
    temporary = destination.with_name(f".{destination.name}.converted")
    try:
        with Image.open(destination) as image:
            image.seek(0)
            converted = ImageOps.exif_transpose(image)
            if "A" in converted.getbands():
                converted = converted.convert("RGBA")
            else:
                converted = converted.convert("RGB")
            converted.save(temporary, format="PNG", optimize=False)
        os.replace(temporary, destination)
    except (OSError, UnidentifiedImageError, ValueError) as error:
        temporary.unlink(missing_ok=True)
        raise HandlerError(
            "ARTICLE_RESOURCE_INVALID_IMAGE",
            "Article image could not be converted to a LaTeX-compatible PNG",
        ) from error


def _extract_template(source: Path, destination: Path) -> None:
    destination.mkdir()
    try:
        archive = zipfile.ZipFile(source)
    except (OSError, zipfile.BadZipFile) as error:
        raise HandlerError("ARTICLE_TEMPLATE_INVALID", "Template is not a ZIP archive") from error
    with archive:
        members = archive.infolist()
        if not members or len(members) > MAX_TEMPLATE_FILES:
            raise HandlerError("ARTICLE_TEMPLATE_INVALID", "Template file count is invalid")
        total = 0
        names: set[str] = set()
        for member in members:
            normalized = PurePosixPath(member.filename)
            normalized_name = normalized.as_posix().rstrip("/").casefold()
            mode = member.external_attr >> 16
            if (
                not member.filename
                or normalized.is_absolute()
                or ".." in normalized.parts
                or "\\" in member.filename
                or "\x00" in member.filename
                or (normalized.parts and normalized.parts[0].endswith(":"))
                or stat.S_ISLNK(mode)
                or member.flag_bits & 0x1
            ):
                raise HandlerError("ARTICLE_TEMPLATE_UNSAFE", "Template contains an unsafe path")
            if normalized_name in names:
                raise HandlerError("ARTICLE_TEMPLATE_UNSAFE", "Template contains duplicate paths")
            names.add(normalized_name)
            total += member.file_size
            compressed = max(member.compress_size, 1)
            if (
                total > MAX_TEMPLATE_EXPANDED_BYTES
                or member.file_size > MAX_TEMPLATE_EXPANDED_BYTES
                or member.file_size > 8 * 1024 * 1024
                and member.file_size / compressed > 200
            ):
                raise HandlerError(
                    "ARTICLE_TEMPLATE_TOO_LARGE", "Expanded template exceeds its limit"
                )
            target = destination.joinpath(*normalized.parts)
            if member.is_dir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            target.parent.mkdir(parents=True, exist_ok=True)
            with archive.open(member) as reader, target.open("wb") as writer:
                shutil.copyfileobj(reader, writer, length=64 * 1024)


def _validate_template(root: Path, registered: Mapping[str, Any]) -> None:
    manifest_path = root / "mmdash-template.json"
    if not manifest_path.is_file():
        raise HandlerError("ARTICLE_TEMPLATE_MANIFEST_MISSING", "Template manifest is missing")
    try:
        archived = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise HandlerError(
            "ARTICLE_TEMPLATE_MANIFEST_INVALID", "Template manifest is invalid"
        ) from error
    if not isinstance(archived, dict) or archived != dict(registered):
        raise HandlerError(
            "ARTICLE_TEMPLATE_MANIFEST_MISMATCH", "Registered manifest does not match ZIP"
        )
    for key in (
        "schema_version",
        "name",
        "version",
        "entrypoint",
        "output",
        "content_target",
        "bibliography_target",
        "engine",
        "bibliography_tool",
    ):
        _required(registered, key)
    if registered["schema_version"] not in {"1.0", "1.1"}:
        raise HandlerError(
            "ARTICLE_TEMPLATE_MANIFEST_INVALID", "Template schema version is invalid"
        )
    if registered["engine"] not in {"auto", "pdflatex", "xelatex", "lualatex"}:
        raise HandlerError("ARTICLE_TEMPLATE_MANIFEST_INVALID", "Template engine is invalid")
    if registered["bibliography_tool"] not in {"auto", "bibtex", "biber", "none"}:
        raise HandlerError("ARTICLE_TEMPLATE_MANIFEST_INVALID", "Bibliography tool is invalid")
    _validate_manifest_11(root, registered)
    entrypoint = _safe_child(root, str(registered["entrypoint"]))
    content = _safe_child(root, str(registered["content_target"]))
    bibliography = _safe_child(root, str(registered["bibliography_target"]))
    _safe_child(root, str(registered["output"]))
    abstract_target = registered.get("abstract_target")
    abstract = _safe_child(root, str(abstract_target)) if abstract_target else None
    if (
        not entrypoint.is_file()
        or content in {entrypoint, bibliography}
        or bibliography == entrypoint
        or (abstract is not None and abstract in {entrypoint, content, bibliography})
    ):
        raise HandlerError("ARTICLE_TEMPLATE_MANIFEST_INVALID", "Template targets are invalid")
    if content.exists() or bibliography.exists() or (abstract is not None and abstract.exists()):
        raise HandlerError(
            "ARTICLE_TEMPLATE_TARGET_EXISTS", "Generated template target already exists"
        )
    for source in root.rglob("*"):
        if not source.is_file():
            continue
        name = source.name.casefold()
        if (
            name in FORBIDDEN_TEMPLATE_NAMES
            or source.suffix.casefold() in FORBIDDEN_TEMPLATE_SUFFIXES
        ):
            raise HandlerError(
                "ARTICLE_TEMPLATE_SCRIPT_FORBIDDEN", "Template contains executable content"
            )
        if source.stat().st_mode & 0o111:
            raise HandlerError(
                "ARTICLE_TEMPLATE_SCRIPT_FORBIDDEN", "Template contains an executable file"
            )


def _validate_manifest_11(root: Path, registered: Mapping[str, Any]) -> None:
    """Validate the optional manifest 1.1 extensions; 1.0 manifests skip this.

    Field semantics follow docs/article/template-spec.md: the fields declare
    what the normalized template supports, never what the article selected.
    """
    schema_version = str(registered.get("schema_version", "1.0"))
    if schema_version != "1.1":
        return
    abstract_target = registered.get("abstract_target")
    if abstract_target is not None:
        if not isinstance(abstract_target, str) or not abstract_target:
            raise HandlerError(
                "ARTICLE_TEMPLATE_MANIFEST_INVALID", "Template abstract target is invalid"
            )
        _safe_child(root, abstract_target)
    if str(registered.get("body_layout", "single")) not in {"single", "sections"}:
        raise HandlerError("ARTICLE_TEMPLATE_MANIFEST_INVALID", "Template body layout is invalid")
    if str(registered.get("field_profile", "default")) not in {"default", "cumcm"}:
        raise HandlerError("ARTICLE_TEMPLATE_MANIFEST_INVALID", "Template field profile is invalid")
    figure_dir = str(registered.get("figure_dir", "figures"))
    relative = PurePosixPath(figure_dir)
    if (
        not figure_dir
        or len(figure_dir) > 120
        or relative.is_absolute()
        or ".." in relative.parts
        or "\\" in figure_dir
    ):
        raise HandlerError("ARTICLE_TEMPLATE_MANIFEST_INVALID", "Template figure dir is invalid")
    if str(registered.get("bibliography_mode", "inline")) not in {"inline", "native"}:
        raise HandlerError(
            "ARTICLE_TEMPLATE_MANIFEST_INVALID", "Template bibliography mode is invalid"
        )


def _raw_paper_fields(build: Mapping[str, Any]) -> dict[str, Mapping[str, Any]]:
    """Return every declared paper info field including disabled entries."""
    paper_info = build.get("paper_info")
    if not isinstance(paper_info, Mapping):
        return {}
    raw_fields = paper_info.get("fields")
    if not isinstance(raw_fields, Mapping):
        return {}
    result: dict[str, Mapping[str, Any]] = {}
    for key, value in raw_fields.items():
        if isinstance(key, str) and isinstance(value, Mapping):
            result[key] = value
    return result


def _paper_fields(build: Mapping[str, Any]) -> dict[str, dict[str, Any]]:
    """Return the enabled-only paper info fields from the frozen document."""
    paper_info = build.get("paper_info")
    if not isinstance(paper_info, Mapping):
        return {}
    raw_fields = paper_info.get("fields")
    if not isinstance(raw_fields, Mapping):
        return {}
    fields: dict[str, dict[str, Any]] = {}
    for key, value in raw_fields.items():
        if not isinstance(key, str) or not isinstance(value, Mapping):
            continue
        if value.get("enabled") is not True:
            continue
        text = value.get("value")
        fields[key] = {"value": text if isinstance(text, str) else ""}
    return fields


LATEX_SPECIALS = str.maketrans(
    {
        "\\": r"\textbackslash{}",
        "&": r"\&",
        "%": r"\%",
        "$": r"\$",
        "#": r"\#",
        "_": r"\_",
        "{": r"\{",
        "}": r"\}",
        "~": r"\textasciitilde{}",
        "^": r"\textasciicircum{}",
    }
)


def _latex_escape(value: str) -> str:
    """Escape plain-text paper info values for LaTeX.

    Backslash must be emitted last for the specials that produce their own
    braces, so translate in two passes: escape everything except the
    backslash placeholder, then restore the command form.
    """
    replaced = value.replace("\\", "\x00")
    replaced = replaced.translate(LATEX_SPECIALS)
    return replaced.replace("\x00", r"\textbackslash{}")


CUMCM_FIELD_COMMANDS = {
    "team_number": "baominghao",
    "captain": "membera",
    "member2": "memberb",
    "member3": "memberc",
    "supervisor": "supervisor",
    "problem_number": "tihao",
    "school": "schoolname",
    "submit_date": "nianyue",
}


_TEMPLATE_COMMAND_DEFINITION = re.compile(
    r"\\(?:newcommand|renewcommand|providecommand)\*?\s*\{?\\([A-Za-z@]+)\}?"
    r"|\\def\\([A-Za-z@]+)"
    r"|\\let\\([A-Za-z@]+)",
)


def _defined_template_commands(template_root: Path) -> set[str]:
    """Return the command names defined by the template's TeX sources.

    Templates own their commands: a paper-info field whose target command the
    template never defines must be skipped rather than emitted, or the build
    would fail with an undefined control sequence. Extraction already bounds
    the source count and sizes, so scanning the text sources is safe.
    """
    names: set[str] = set()
    for source in template_root.rglob("*"):
        if (
            not source.is_file()
            or source.suffix.lower() not in {".cls", ".sty", ".tex"}
        ):
            continue
        try:
            text = source.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        for match in _TEMPLATE_COMMAND_DEFINITION.finditer(text):
            for group in match.groups():
                if group:
                    names.add(group)
    return names


def _write_metadata_blocks(
    template_root: Path,
    manifest: Mapping[str, Any],
    fields: Mapping[str, Mapping[str, Any]],
    abstract_enabled: bool,
) -> None:
    """Emit the generated metadata, title, and keyword TeX blocks.

    Only fields compatible with the template profile are emitted; unknown or
    incompatible selections are skipped silently because the product already
    warned the user at save time. Under the cumcm profile a field is emitted
    only when the template actually defines its target command (e.g.
    cumcmthesis.cls has no \\nianyue, so submit_date is dropped).
    """
    generated = template_root / ".mmdash"
    generated.mkdir(parents=True, exist_ok=True)
    lines = [
        "\\newif\\ifmmdashabstract",
        "\\mmdashabstracttrue" if abstract_enabled else "\\mmdashabstractfalse",
    ]
    profile = str(manifest.get("field_profile", "default"))
    defined = _defined_template_commands(template_root) if profile == "cumcm" else set()
    if "title" in fields:
        lines.append(f"\\title{{{_latex_escape(fields['title']['value'])}}}")
    if "author" in fields:
        lines.append(f"\\author{{{_latex_escape(fields['author']['value'])}}}")
    if "date" in fields:
        lines.append(f"\\date{{{_latex_escape(fields['date']['value'])}}}")
    if profile == "cumcm":
        for key, command in CUMCM_FIELD_COMMANDS.items():
            if key in fields and command in defined:
                lines.append(f"\\{command}{{{_latex_escape(fields[key]['value'])}}}")
    (generated / "metadata.tex").write_text("\n".join(lines) + "\n", encoding="utf-8", newline="\n")
    title_block = "\\maketitle\n" if {"title", "author", "date"} & fields.keys() else ""
    (generated / "title-block.tex").write_text(title_block, encoding="utf-8", newline="\n")
    keywords_line = ""
    if "keywords" in fields:
        keywords_value = _latex_escape(fields["keywords"]["value"])
        if profile == "cumcm" and "keywords" in defined:
            # The class renders its official bold heading inside the abstract.
            keywords_line = f"\\keywords{{{keywords_value}}}\n"
        else:
            keywords_line = f"\\noindent\\textbf{{关键词：}}{keywords_value}\n"
    (generated / "keywords-block.tex").write_text(keywords_line, encoding="utf-8", newline="\n")


def _split_markdown_sections(
    manuscript: str, headings: list[Mapping[str, Any]]
) -> list[tuple[str, str]]:
    """Split the Markdown body into per-H1/H2 chunks keyed by block ID.

    Fenced code blocks are tracked so a `#` line inside them can never start
    a new section. Content before the first frozen heading becomes the
    front-matter chunk with an empty block ID.
    """
    expected = [heading for heading in headings if heading.get("block_id")]
    chunks: list[tuple[str, str]] = []
    current_id = ""
    current_lines: list[str] = []
    fence = ""
    index = 0
    for line in manuscript.splitlines():
        stripped = line.lstrip()
        if fence:
            if stripped.startswith(fence):
                fence = ""
        elif stripped.startswith(("```", "~~~")):
            fence = stripped[:3]
        else:
            if index < len(expected) and stripped.startswith("#"):
                heading = expected[index]
                level = int(heading.get("level", 1) or 1)
                marker = "#" * level
                text = str(heading.get("text", "")).strip()
                candidate = stripped[len(marker) :].strip()
                if stripped.startswith(marker + " ") and candidate == text and text:
                    if current_lines or current_id:
                        chunks.append((current_id, "\n".join(current_lines)))
                    current_id = str(heading.get("block_id", ""))
                    current_lines = [line]
                    index += 1
                    continue
        current_lines.append(line)
    if current_lines or current_id:
        chunks.append((current_id, "\n".join(current_lines)))
    if not chunks:
        chunks.append(("", manuscript))
    return chunks


def _section_filename(block_id: str) -> str:
    safe = [
        character if character.isalnum() or character in "-_" else "-" for character in block_id
    ]
    return "".join(safe)[:120] or "section"


def _inject_bibliography_block(
    template_root: Path,
    manifest: Mapping[str, Any],
    entrypoint_text: str,
    bibliography_target: Path,
) -> None:
    """Emit the generated bibliography block.

    Inline mode keeps citeproc rendering inside the content fragment, so the
    block stays empty. Native mode trusts the template's own wiring; only a
    template without any bibliography command receives the GB/T 7714 numeric
    fallback pointing at the generated .bib file.
    """
    generated = template_root / ".mmdash"
    generated.mkdir(parents=True, exist_ok=True)
    body = ""
    if str(manifest.get("bibliography_mode", "inline")) == "native":
        wiring = _BIBLIOGRAPHY_WIRING.search(entrypoint_text)
        if wiring is None:
            stem = bibliography_target.with_suffix("").name
            body = f"\\bibliographystyle{{gbt7714-numerical}}\n\\bibliography{{{stem}}}\n"
    (generated / "bibliography-block.tex").write_text(body, encoding="utf-8", newline="\n")


_BIBLIOGRAPHY_WIRING = re.compile(
    r"^[^%\n]*\\(?:bibliography|bibliographystyle|addbibresource|printbibliography)\b",
    re.MULTILINE,
)


def _create_source_zip(
    output: Path,
    manuscript: Path,
    bibliography: Path,
    article_manifest: Path,
    template_root: Path,
    pdf: Path,
    entrypoint: Path,
    report: Path,
    build: Mapping[str, Any],
    toolchain: Mapping[str, str],
) -> None:
    entries: dict[str, bytes] = {
        "manuscript.md": manuscript.read_bytes(),
        "references.bib": bibliography.read_bytes(),
        ".mmdash/article.json": article_manifest.read_bytes(),
        "paper.pdf": pdf.read_bytes(),
        "build-report.json": report.read_bytes(),
        "README.md": (
            b"# mmdash reproducible Article source\n\n"
            b"Set the main document to `main.tex` in Overleaf. The frozen toolchain and build "
            b"metadata are in `build-manifest.json`; file digests are in `CHECKSUMS.sha256`.\n"
        ),
    }
    for source in sorted(template_root.rglob("*")):
        relative = source.relative_to(template_root)
        if relative.parts and relative.parts[0] in {".cache", ".config", ".local"}:
            continue
        if relative.parts and relative.parts[0].startswith(".texlive"):
            continue
        if source.is_file():
            entries[relative.as_posix()] = source.read_bytes()
    entrypoint_name = entrypoint.relative_to(template_root).as_posix()
    if entrypoint_name != "main.tex":
        # Keep the original entrypoint at its registered path. Copying its
        # bytes to the ZIP root silently changes the base directory used by
        # relative \input/\include paths. A tiny wrapper preserves the tree
        # and gives Overleaf the conventional main.tex entrypoint.
        entries["main.tex"] = f"\\input{{{entrypoint_name}}}\n".encode()
    manifest = {
        "schema_version": "1.0",
        "build_id": build["build_id"],
        "build_kind": build["build_kind"],
        "engine": toolchain.get("engine", ""),
        "bibliography_tool": build["bibliography_tool"],
        "toolchain": dict(toolchain),
        "source_date_epoch": 0,
    }
    entries["build-manifest.json"] = (
        json.dumps(manifest, ensure_ascii=False, sort_keys=True, indent=2) + "\n"
    ).encode()
    entries["CHECKSUMS.sha256"] = "".join(
        f"{hashlib.sha256(contents).hexdigest()}  {name}\n"
        for name, contents in sorted(entries.items())
    ).encode()
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for directory in ("figures/", "sections/", "tables/"):
            info = zipfile.ZipInfo(directory, FIXED_ZIP_TIME)
            info.external_attr = 0o40755 << 16
            archive.writestr(info, b"")
        for name, contents in sorted(entries.items()):
            if name.startswith("/") or ".." in PurePosixPath(name).parts:
                raise HandlerError("ARTICLE_SOURCE_ZIP_INVALID", "Source ZIP path is invalid")
            info = zipfile.ZipInfo(name, FIXED_ZIP_TIME)
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o100644 << 16
            archive.writestr(info, contents)


class _CommandFailure(RuntimeError):
    def __init__(self, code: str, message: str, log: str, *, retryable: bool = False) -> None:
        super().__init__(message)
        self.code = code
        self.log = log
        self.retryable = retryable


def _run_command(
    arguments: list[str],
    cwd: Path,
    *,
    timeout: int,
    limits: Mapping[str, int] | None = None,
) -> str:
    environment = {
        "PATH": os.environ.get("PATH", ""),
        "HOME": str(cwd),
        "LANG": "C.UTF-8",
        "LC_ALL": "C.UTF-8",
        "SOURCE_DATE_EPOCH": "0",
        "TZ": "UTC",
        "openout_any": "p",
        "openin_any": "p",
    }
    try:
        completed = subprocess.run(
            arguments,
            cwd=cwd,
            env=environment,
            shell=False,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=timeout,
            check=False,
            preexec_fn=partial(_limit_process, limits or {}) if os.name == "posix" else None,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        code = (
            "ARTICLE_BUILD_TIMEOUT"
            if isinstance(error, subprocess.TimeoutExpired)
            else "ARTICLE_TOOL_UNAVAILABLE"
        )
        captured = getattr(error, "stdout", "") or ""
        if isinstance(captured, bytes):
            captured = captured.decode("utf-8", errors="replace")
        raise _CommandFailure(
            code,
            "Article build tool failed to execute",
            "$ " + " ".join(arguments) + "\n" + captured[-4_000_000:],
            retryable=code == "ARTICLE_TOOL_UNAVAILABLE",
        ) from error
    output = completed.stdout[-4_000_000:]
    if completed.returncode != 0:
        raise _CommandFailure(
            "ARTICLE_BUILD_FAILED",
            "Article document compilation failed",
            "$ " + " ".join(arguments) + "\n" + output,
        )
    return "$ " + " ".join(arguments) + "\n" + output


def _limit_process(limits: Mapping[str, int]) -> None:
    if resource is None:
        return
    cpu = limits.get("timeout_seconds", 600)
    memory = limits.get("memory_bytes", 1024**3)
    output = limits.get("output_bytes", MAX_OUTPUT_BYTES)
    resource.setrlimit(resource.RLIMIT_CPU, (cpu, cpu))
    resource.setrlimit(resource.RLIMIT_AS, (memory, memory))
    resource.setrlimit(resource.RLIMIT_FSIZE, (output, output))
    resource.setrlimit(resource.RLIMIT_NOFILE, (256, 256))
    resource.setrlimit(resource.RLIMIT_NPROC, (128, 128))


def _limits(build: Mapping[str, Any]) -> dict[str, int]:
    raw = _mapping(build.get("limits"))
    if raw.get("network") != "none":
        raise HandlerError("ARTICLE_BUILD_INVALID_INPUT", "Article build network must be disabled")
    bounds = {
        "timeout_seconds": (1, 600),
        "memory_bytes": (128 * 1024**2, 2 * 1024**3),
        "disk_bytes": (128 * 1024**2, 4 * 1024**3),
        "output_bytes": (1, MAX_OUTPUT_BYTES),
    }
    result: dict[str, int] = {}
    for name, (minimum, maximum) in bounds.items():
        value = raw.get(name)
        if not isinstance(value, int) or isinstance(value, bool) or not minimum <= value <= maximum:
            raise HandlerError("ARTICLE_BUILD_INVALID_INPUT", "Article build limits are invalid")
        result[name] = value
    return result


def _check_disk(root: Path, limit: int) -> None:
    total = 0
    for source in root.rglob("*"):
        if source.is_file():
            total += source.stat().st_size
            if total > limit:
                raise HandlerError("ARTICLE_DISK_LIMIT", "Article build exceeded its disk limit")


def _toolchain(engine: str) -> dict[str, str]:
    return {
        "pandoc": _version(["pandoc", "--version"]),
        "latexmk": _version(["latexmk", "-v"]),
        "tex_engine": _version([engine, "--version"]),
        "engine": engine,
    }


def _verify_toolchain(build: Mapping[str, Any], engine: str) -> None:
    expected = _mapping(build.get("toolchain"))
    actual = _toolchain(engine)
    checks = {
        "pandoc": actual["pandoc"],
        "latexmk": actual["latexmk"],
        "texlive": actual["tex_engine"],
    }
    for name, version in checks.items():
        required = expected.get(name)
        if not isinstance(required, str) or not required or required not in version:
            raise HandlerError(
                "ARTICLE_TOOLCHAIN_MISMATCH",
                f"Pinned Article {name} toolchain is unavailable",
            )


def _version(command: list[str]) -> str:
    try:
        output = subprocess.run(
            command,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=10,
            check=False,
        ).stdout
    except (OSError, subprocess.TimeoutExpired):
        return "unavailable"
    return output.splitlines()[0][:200] if output else "unknown"


def _engine(build: Mapping[str, Any], manifest: Mapping[str, Any]) -> str:
    value = str(build["engine"])
    if value == "auto":
        value = str(manifest.get("engine", "pdflatex"))
    if value == "auto":
        value = "pdflatex"
    if value not in {"pdflatex", "xelatex", "lualatex"}:
        raise HandlerError("ARTICLE_ENGINE_INVALID", "Article engine is invalid")
    return value


def _safe_child(root: Path, name: str) -> Path:
    relative = PurePosixPath(name)
    if relative.is_absolute() or ".." in relative.parts or "\\" in name or not relative.parts:
        raise HandlerError("ARTICLE_TEMPLATE_UNSAFE", "Template manifest path is unsafe")
    target = root.joinpath(*relative.parts).resolve()
    if root.resolve() not in target.parents:
        raise HandlerError("ARTICLE_TEMPLATE_UNSAFE", "Template manifest escaped its root")
    return target


def _required(value: Mapping[str, Any], key: str) -> str:
    result = value.get(key)
    if not isinstance(result, str) or not result or len(result) > 255:
        raise HandlerError("ARTICLE_TEMPLATE_INVALID", "Template manifest is invalid")
    return result


def _mapping(value: Any) -> Mapping[str, Any]:
    if not isinstance(value, Mapping):
        raise HandlerError("ARTICLE_BUILD_INVALID_INPUT", "Article build input is invalid")
    return value


def _sanitize_log(value: str, root: Path) -> str:
    sanitized = value.replace(str(root), "$WORKDIR")
    for key in ("TOKEN", "SECRET", "PASSWORD", "API_KEY", "AUTHORIZATION"):
        secret = os.environ.get(key, "")
        if secret:
            sanitized = sanitized.replace(secret, "[REDACTED]")
    return sanitized[-4_000_000:]


def _upload_output(
    client: ArticleClient,
    job_id: str,
    role: str,
    source: Path,
    filename: str,
    mime_type: str,
) -> dict[str, Any]:
    size = source.stat().st_size
    if size < 1 or size > MAX_OUTPUT_BYTES:
        raise HandlerError("ARTICLE_OUTPUT_INVALID", "Article output exceeds its limit")
    digest = _sha256(source)
    response = client.upload_article_build_output(
        job_id,
        role,
        source,
        filename=filename,
        mime_type=mime_type,
        sha256=digest,
        size_bytes=size,
    )
    return {
        "role": role,
        "artifact_id": response.get("artifact_id"),
        "version_id": response.get("version_id"),
        "sha256": digest,
        "size_bytes": size,
    }


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        while chunk := source.read(64 * 1024):
            digest.update(chunk)
    return digest.hexdigest()
