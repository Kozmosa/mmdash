import json
import zipfile
from pathlib import Path

import pytest

from mmdash_worker.article.handler import (
    _inject_bibliography_block,
    _latex_escape,
    _paper_fields,
    _raw_paper_fields,
    _section_filename,
    _split_markdown_sections,
    _validate_manifest_11,
    _write_metadata_blocks,
)
from mmdash_worker.jobs.handlers import HandlerError

MANIFEST_11 = {
    
        "schema_version": "1.1",
        "name": "cumcm-template",
        "version": "1.1.0",
        "entrypoint": "main.tex",
        "output": "paper.pdf",
        "content_target": "sections/body.tex",
        "bibliography_target": ".mmdash/references-zotero.bib",
        "engine": "xelatex",
        "bibliography_tool": "bibtex"
    ,
    "abstract_target": ".mmdash/abstract-block.tex",
    "body_layout": "sections",
    "field_profile": "cumcm",
    "figure_dir": "figure",
    "bibliography_mode": "native",
}


def test_manifest_11_validates_optional_extensions(tmp_path: Path) -> None:
    # 1.0 manifests skip extension validation entirely.
    _validate_manifest_11(tmp_path, {"schema_version": "1.0"})
    _validate_manifest_11(tmp_path, MANIFEST_11)
    for key, value in (
        ("body_layout", "chapters"),
        ("field_profile", "custom"),
        ("figure_dir", "../escape"),
        ("bibliography_mode", "citeproc"),
    ):
        with pytest.raises(HandlerError) as caught:
            _validate_manifest_11(tmp_path, {**MANIFEST_11, key: value})
        assert caught.value.code == "ARTICLE_TEMPLATE_MANIFEST_INVALID"
    # Path traversal in the abstract target is an unsafe path, not a format
    # error.
    with pytest.raises(HandlerError) as caught:
        _validate_manifest_11(tmp_path, {**MANIFEST_11, "abstract_target": "../escape.tex"})
    assert caught.value.code == "ARTICLE_TEMPLATE_UNSAFE"


CUMCM_STUB_CLS = r"""
% Minimal stand-in mirroring cumcmthesis.cls command definitions. \nianyue
% is deliberately absent to mirror the real class.
\newcommand*\tihao[1]{}
\newcommand*\baominghao[1]{}
\newcommand*\schoolname[1]{}
\newcommand*\membera[1]{}
\newcommand*\memberb[1]{}
\newcommand*\memberc[1]{}
\newcommand*\supervisor[1]{}
\newcommand\keywords[1]{}
"""


def _write_cumcm_stub_cls(root: Path) -> None:
    (root / "cumcmthesis.cls").write_text(CUMCM_STUB_CLS, encoding="utf-8")


def test_paper_fields_return_enabled_entries_only() -> None:
    build = {
        "paper_info": {
            "schema_version": "1.0",
            "fields": {
                "team_number": {"enabled": True, "value": "T2603001"},
                "title": {"enabled": False, "value": "Hidden"},
            },
        }
    }
    assert _paper_fields(build) == {
        "team_number": {"value": "T2603001"},
    }
    assert set(_raw_paper_fields(build)) == {"team_number", "title"}
    assert _paper_fields({}) == {}


def test_metadata_blocks_map_cumcm_fields_and_skip_unselected(tmp_path: Path) -> None:
    manifest = {**MANIFEST_11, "field_profile": "cumcm"}
    _write_cumcm_stub_cls(tmp_path)
    fields = {
        "team_number": {"value": "T2603001"},
        "keywords": {"value": "优化; 排队论_100"},
    }
    _write_metadata_blocks(tmp_path, manifest, fields, abstract_enabled=True)

    metadata = (tmp_path / ".mmdash" / "metadata.tex").read_text(encoding="utf-8")
    title_block = (tmp_path / ".mmdash" / "title-block.tex").read_text(encoding="utf-8")
    keywords_block = (tmp_path / ".mmdash" / "keywords-block.tex").read_text(encoding="utf-8")

    # Selecting only 队伍编号 generates only \baominghao: no empty title,
    # author, date, or abstract commands ride along. The class defines
    # \keywords, so keywords render through the official command.
    assert "\\baominghao{T2603001}" in metadata
    assert "\\title" not in metadata
    assert "\\author" not in metadata
    assert "\\date" not in metadata
    assert title_block == ""
    assert "\\keywords{优化; 排队论\\_100}" in keywords_block
    assert "\\mmdashabstracttrue" in metadata

    _write_metadata_blocks(tmp_path, manifest, {}, abstract_enabled=False)
    metadata = (tmp_path / ".mmdash" / "metadata.tex").read_text(encoding="utf-8")
    assert "\\mmdashabstractfalse" in metadata

    default_manifest = {**MANIFEST_11, "field_profile": "default"}
    _write_metadata_blocks(tmp_path, default_manifest, {"team_number": {"value": "T1"}}, True)
    metadata = (tmp_path / ".mmdash" / "metadata.tex").read_text(encoding="utf-8")
    # The default profile does not understand CUMCM commands.
    assert "baominghao" not in metadata


def test_metadata_blocks_skip_fields_without_template_commands(tmp_path: Path) -> None:
    manifest = {**MANIFEST_11, "field_profile": "cumcm"}
    _write_cumcm_stub_cls(tmp_path)
    fields = {
        "problem_number": {"value": "B"},
        "submit_date": {"value": "2026年9月7日"},
    }
    _write_metadata_blocks(tmp_path, manifest, fields, abstract_enabled=True)

    metadata = (tmp_path / ".mmdash" / "metadata.tex").read_text(encoding="utf-8")
    # cumcmthesis.cls defines \tihao but has no \nianyue, so the submit date
    # is dropped instead of failing the build with an undefined command.
    assert "\\tihao{B}" in metadata
    assert "nianyue" not in metadata
    assert "2026年9月7日" not in metadata


def test_metadata_blocks_keywords_fall_back_without_class_keywords(tmp_path: Path) -> None:
    manifest = {**MANIFEST_11, "field_profile": "cumcm"}
    # A profile class without \keywords keeps the generic bold line.
    (tmp_path / "custom.cls").write_text(
        "\\newcommand*\\baominghao[1]{}\n", encoding="utf-8"
    )
    _write_metadata_blocks(
        tmp_path, manifest, {"keywords": {"value": "优化"}}, abstract_enabled=True
    )
    keywords_block = (tmp_path / ".mmdash" / "keywords-block.tex").read_text(encoding="utf-8")
    assert keywords_block == "\\noindent\\textbf{关键词：}优化\n"


def test_latex_escape_covers_specials_including_backslash() -> None:
    assert _latex_escape("50% of a_b & c#d") == "50\\% of a\\_b \\& c\\#d"
    assert _latex_escape("a\\b") == "a\\textbackslash{}b"


def test_bibliography_block_modes(tmp_path: Path) -> None:
    entrypoint = tmp_path / "main.tex"
    target = tmp_path / ".mmdash" / "references-zotero.bib"
    entrypoint.write_text("\\begin{document}\\input{x}\\end{document}", encoding="utf-8")

    _inject_bibliography_block(
        tmp_path, {"bibliography_mode": "inline"}, entrypoint.read_text(), target
    )
    block = (tmp_path / ".mmdash" / "bibliography-block.tex").read_text(encoding="utf-8")
    assert block == ""

    # Native mode trusts existing wiring; only unwired templates get the
    # GB/T 7714 numeric fallback.
    _inject_bibliography_block(
        tmp_path, {"bibliography_mode": "native"}, entrypoint.read_text(), target
    )
    block = (tmp_path / ".mmdash" / "bibliography-block.tex").read_text(encoding="utf-8")
    assert "gbt7714-numerical" in block
    assert "\\bibliography{references-zotero}" in block

    wired = "\\begin{document}\\bibliographystyle{plain}\\bibliography{refs}\\end{document}"
    _inject_bibliography_block(tmp_path, {"bibliography_mode": "native"}, wired, target)
    block = (tmp_path / ".mmdash" / "bibliography-block.tex").read_text(encoding="utf-8")
    assert block == ""


def test_section_filenames_are_stable_and_safe() -> None:
    assert _section_filename("block_2026") == "block_2026"
    # Unicode letters survive; path separators and dots collapse to dashes.
    assert _section_filename("中文/../block") == "中文----block"
    assert _section_filename("") == "section"
    long = _section_filename("x" * 500)
    assert len(long) == 120


def test_split_markdown_sections_keeps_fence_hash_lines_and_h3() -> None:
    manuscript = (
        "# 引言\n"
        "正文A\n"
        "## 模型\n"
        "```latex\n"
        "# 这不是新章节\n"
        "```\n"
        "### 子节保留在本文件\n"
        "## 求解\n"
        "结论\n"
    )
    headings = [
        {"block_id": "h1", "level": 1, "text": "引言", "ordinal": 0},
        {"block_id": "h2-model", "level": 2, "text": "模型", "ordinal": 1},
        {"block_id": "h2-solve", "level": 2, "text": "求解", "ordinal": 2},
    ]
    chunks = _split_markdown_sections(manuscript, headings)

    assert [block_id for block_id, _ in chunks] == ["h1", "h2-model", "h2-solve"]
    assert "正文A" in chunks[0][1]
    assert "# 这不是新章节" in chunks[1][1]
    assert "### 子节保留在本文件" in chunks[1][1]
    assert "结论" in chunks[2][1]
    # H3 headings never start their own file.
    assert "### " in chunks[1][1]


def test_split_markdown_sections_front_matter_lands_in_content_target() -> None:
    manuscript = "前言\n\n# 第一章\n正文"
    headings = [{"block_id": "h1", "level": 1, "text": "第一章", "ordinal": 0}]
    chunks = _split_markdown_sections(manuscript, headings)
    assert chunks[0][0] == ""
    assert chunks[0][1].strip() == "前言"
    assert chunks[1][0] == "h1"
    assert "正文" in chunks[1][1]


def write_manifest_zip(path: Path, manifest: dict) -> Path:
    with zipfile.ZipFile(path, "w") as archive:
        archive.writestr("mmdash-template.json", json.dumps(manifest, sort_keys=True))
    return path
