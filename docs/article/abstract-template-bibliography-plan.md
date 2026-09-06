# Article Abstract, Paper Info, and Template Normalization Plan

Status: accepted implementation plan (user-supplied design, 2026-09).
Owners: `backend/internal/article`, `apps/web/src/features/article`,
`apps/web-bff/src/article`, `workers/mmdash-worker/src/mmdash_worker/article`.

## Summary

The Article TeX source is split into three authoring inputs:

```text
abstract.md         -> .mmdash/abstract block target
manuscript.md       -> sections/*.tex (or content_target in single layout)
Zotero References   -> .mmdash/references-zotero.bib
                            |
                         main.tex
```

Abstract stays Markdown but is no longer mixed into the manuscript body. The
template decides where the abstract lands in the PDF.

## 1. Abstract module

- Article gains separate "摘要 / 正文" edit tabs. Both reuse the existing
  Tiptap/Yjs editing and collaboration save path.
- Abstract is stored separately: `abstract_tiptap_json`, `abstract_yjs_update`,
  `abstract_state_vector`, `abstract_revision`, `abstract_markdown`.
- Formulas, bold, and Zotero citations work inside the abstract.
- Keywords are structured fields, never written into `abstract.md`.
- "论文信息" can enable/disable the abstract. Disabling keeps the content but
  the TeX generation emits an empty abstract block. The Abstract tab shows
  已启用/未启用 so users cannot forget a written abstract.

Generation:

```text
abstract.md -> Pandoc -> abstract block target -> template slot
```

Default template structure:

```tex
\input{.mmdash/metadata.tex}

\begin{document}
\input{.mmdash/title-block.tex}
\input{.mmdash/abstract-block.tex}
\input{sections/body.tex}
\input{.mmdash/bibliography-block.tex}
\end{document}
```

When the abstract is disabled the abstract block file is empty: no empty
heading and no placeholder.

Custom template handling at normalization time:

- Existing `abstract` environment: keep environment and formatting, replace the
  example text with the generated fragment only.
- No abstract structure: insert the standard abstract block between title and
  body.
- Ambiguous abstract range: let the user pick the range in the template preview.
- A top-level 摘要/Abstract section inside the body offers a one-time migration
  preview; content moves to the Abstract tab only after user confirmation. The
  body is never silently edited.

## 2. Paper info and CUMCM fields

The 论文信息 dialog lets the user multi-select the fields this article needs:

- Generic: `title`, `author`, `date`, `abstract`, `keywords`.
- CUMCM (国赛): `problem_number` 题号, `team_number` 队伍编号, `school` 学校,
  `captain` 队长, `member2` 队员二, `member3` 队员三, `supervisor` 指导教师,
  `submit_date` 提交日期.

Selection and values are saved per article. Unselected fields generate no TeX;
fields incompatible with the current template are skipped after a prompt.

CUMCM command mapping (`cumcmthesis.cls`):

```text
team_number -> \baominghao{...}
captain     -> \membera{...}
member2     -> \memberb{...}
member3     -> \memberc{...}
supervisor  -> \supervisor{...}
problem_number / school / submit_date -> matching cumcmthesis commands
```

Selecting only 队伍编号 generates only `\baominghao{...}`: no empty title, no
empty author, no empty abstract.

## 3. Template upload and normalization

- Multi-select `.tex` and `.zip`; each file imports as its own template.
- A single `.tex` that depends on local `.cls/.sty` is rejected with a prompt
  to re-upload a ZIP containing the dependencies.
- Artifact UUID, Version UUID, template version, engine, bibliography tool,
  and the manifest are system-generated.
- The parser auto-detects entrypoint, body, abstract, title, bibliography
  structure, and toolchain. Ambiguous or multi-entry documents fall back to
  user selection in a preview.
- Parsing ignores example commands inside comments, `\verb`, `verbatim`,
  `listings`, and `tcode` environments.
- The original template is kept as an immutable Artifact; normalization
  produces a NEW template version and never overwrites user files.
- Missing files or fonts block the template and are listed exactly; fonts are
  never auto-replaced.
- Each template imports and tests independently, concurrency 2, batch jobs may
  partially succeed.

Manifest 1.1 adds (all optional, 1.0 stays valid):

- `abstract_target`: where the system writes the generated abstract block.
- `body_layout`: `single` (content_target) or `sections` (sections/body.tex).
- `field_profile`: `default`, `cumcm`, or an explicit field list.
- `figure_dir`: directory receiving converted figures (default `figures`).
- `bibliography_mode`: `inline` (citeproc into content) or `native`
  (template's own bibliography commands).

## 4. Body and references

Body splitting for `body_layout: sections`:

- H1 and H2 each become an independent TeX file; H3-H6 stay inside the
  enclosing file.
- File names use the stable heading Block ID.
- `sections/body.tex` inputs the files in document order.
- Images are placed uniformly under `figure/`.

Zotero is the only reference source:

- Full BibTeX export (authors, year, journal, volume/issue, pages, publisher,
  DOI, URL, ...).
- Abstract and body share one citation key space and one frozen reference set.
- Zotero references are written to the template's `bibliography_target` and
  never overwrite a template's own `.bib`.
- Native BibTeX/biblatex styles in the template are preserved.
- A template without a bibliography style falls back to GB/T 7714 numeric.
- New templates no longer unconditionally run citeproc.
- A handwritten `thebibliography` that cannot be converted safely blocks
  adaptation and points at the location; it is never silently deleted.

Preview, Commit, and formal Build freeze together:

- body revision, abstract revision, paper info version, template version,
  Zotero reference version.

The Repo commit contains `manuscript.md`, `abstract.md`, `references.bib`, and
`.mmdash/article.json` (with the frozen paper info and abstract revision).

## 5. Testing and acceptance

- Default template builds with the abstract both enabled and disabled.
- Abstract Markdown, formulas, Chinese, and Zotero citations verified.
- The abstract never appears inside any body section file.
- A 摘要 section inside the body is never moved silently.
- Full regression with the given `cumcmthesis.cls` and `example.tex`.
- Selecting only 队伍编号 generates only `\baominghao`.
- Single `.tex`, complete ZIP, multi-entry, missing dependency, missing font,
  and malicious ZIP cases.
- H1/H2 splitting and `figure/` image paths.
- BibTeX, Biber, GB/T 7714, and template-owned `.bib` preservation.
- Manifest 1.0 templates still build.
- Contract checks, module tests, `pnpm check`, and the Article full-chain
  smoke test.

## Default constraints

- Abstract is an independent Markdown document, not a normal body block.
- Keywords are structured fields, never part of the abstract body.
- Legacy in-body abstracts migrate only after explicit user confirmation.
- No link/DOI paste import.
- Existing block and chapter review state features are not modified.
