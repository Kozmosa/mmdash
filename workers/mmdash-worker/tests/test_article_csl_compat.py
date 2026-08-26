from pathlib import Path

from mmdash_worker.article.handler import (
    PANDOC_CSL_COMPATIBILITY,
    _inject_pandoc_citeproc_compatibility,
)


def test_pandoc_217_csl_environment_is_paragraph_based() -> None:
    assert "\\newenvironment{CSLReferences}[2]" in PANDOC_CSL_COMPATIBILITY
    assert "\\setlength{\\parindent}{0pt}" in PANDOC_CSL_COMPATIBILITY
    assert "\\def\\par{\\hangindent=\\cslhangindent\\oldpar}" in PANDOC_CSL_COMPATIBILITY
    assert "\\setlength{\\parskip}{#2\\cslentryspacingunit}" in PANDOC_CSL_COMPATIBILITY

    # Regression for the real build failure:
    # Pandoc 2.17 citeproc output is paragraph based, not list-item based.
    assert "\\begin{list}" not in PANDOC_CSL_COMPATIBILITY
    assert "\\end{list}" not in PANDOC_CSL_COMPATIBILITY


def test_generated_fragment_receives_pandoc_217_csl_prelude(tmp_path: Path) -> None:
    generated = tmp_path / "generated-content.tex"
    generated.write_text(
        "\\phantomsection\\label{refs}\n"
        "\\begin{CSLReferences}{1}{0}\n"
        "Doe, Jane. 2026. Example Reference.\\n\\n"
        "Smith, John. 2025. Another Reference.\\n"
        "\\end{CSLReferences}\n",
        encoding="utf-8",
    )

    _inject_pandoc_citeproc_compatibility(generated)

    result = generated.read_text(encoding="utf-8")
    assert result.startswith(PANDOC_CSL_COMPATIBILITY)
    assert "\\begin{CSLReferences}{1}{0}" in result
    assert "\\begin{list}" not in result.split("\\begin{CSLReferences}", 1)[0]
