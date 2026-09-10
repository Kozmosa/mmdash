package article

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
)

const (
	cumcm2026TemplateFilename       = "mmdash-cumcm2026-template.zip"
	cumcm2026TemplateIdempotencyKey = "article-cumcm2026-template:1.0.1"
	// cumcm2026RetiredTemplateName identifies the superseded built-in whose
	// rows stay persisted for old builds but disappear from the template list.
	cumcm2026RetiredTemplateName = "CUMCM 国赛论文模板（cumcmthesis）"
)

// cumcm2026TemplateCls ships the contest class byte-for-byte except for the
// conditional font fallbacks: the licensed Windows fonts (Times New Roman,
// Arial, SimSun, simkai.ttf) do not exist on the Linux build toolchain, so
// each declaration falls back to its metric-compatible TeX Live clone. The
// GB/T 7714 style ships with the template because the pinned Worker image
// does not include the gbt7714 package.
//
//go:embed templates/cumcm2026/cumcmthesis.cls
var cumcm2026TemplateCls []byte

//go:embed templates/cumcm2026/gbt7714-numerical.bst
var cumcm2026TemplateBst []byte

func cumcm2026TemplateManifest() TemplateManifest {
	return TemplateManifest{
		SchemaVersion:      "1.1",
		Name:               "CUMCM 2026 国赛论文模板（cumcm2026）",
		Version:            "1.0.1",
		Entrypoint:         "main.tex",
		Output:             "main.pdf",
		ContentTarget:      "texfile/body.tex",
		BibliographyTarget: "references.bib",
		Engine:             "xelatex",
		BibliographyTool:   "bibtex",
		AbstractTarget:     "mmdash/abstract-block.tex",
		BodyLayout:         "sections",
		FieldProfile:       "cumcm",
		FigureDir:          "figures",
		BibliographyMode:   "native",
	}
}

// cumcm2026TemplateMainTex mirrors the official contest template: main.tex
// only wires the abstract wrapper and the generated section index; every H1
// section compiles from its own texfile/N-标题.tex.
const cumcm2026TemplateMainTex = `% !TeX program = xelatex
% mmdash CUMCM 2026 built-in template. main.tex 只做装配：题目信息由
% mmdash/metadata.tex 注入，摘要装在 texfile/0-摘要.tex，正文各 H1 拆分进
% texfile/N-标题.tex 并由 texfile/body.tex 汇总 \input。
\documentclass[withoutpreface,bwprint]{cumcmthesis}

\usepackage{graphicx}
\usepackage{float}
\usepackage{longtable}
\usepackage{booktabs}
\usepackage{array}
\usepackage{url}
\usepackage{xurl}
\usepackage{subcaption}
\usepackage{amsmath,amssymb,bm}
\usepackage{mathtools}
\usepackage{etoolbox}
\graphicspath{{figures/}}

% Pandoc 把表格渲染为 longtable，而 cumcmthesis.cls 只给 tabular 补了
% 1.38 行距；这里让 longtable 与模板自身的 tabular 表格行距一致。
\AtBeginEnvironment{longtable}{\renewcommand{\arraystretch}{1.38}}

% Pandoc 列表兼容命令
\providecommand{\tightlist}{%
  \setlength{\itemsep}{0pt}\setlength{\parskip}{0pt}}

% 题目/题号/报名号/学校/队员/指导教师按启用的论文信息字段写入
% mmdash/metadata.tex；\@title 重置保证未填题目时 \maketitle 仍合法。
\makeatletter
\renewcommand*{\@title}{}
\makeatother
\input{mmdash/metadata}

\begin{document}

\maketitle

\input{texfile/0-摘要}
\input{texfile/body}

% Native bibliography wiring: bibtex + 自带的 GB/T 7714 数字样式渲染
% references.bib（由冻结的参考文献生成）。
\bibliographystyle{gbt7714-numerical}
\bibliography{references}

\end{document}
`

// cumcm2026TemplateAbstractTex keeps the abstract page shape of the official
// template (texfile/0摘要.tex): the class abstract environment plus the
// generated abstract and keywords blocks.
const cumcm2026TemplateAbstractTex = `% 摘要与关键词：由 mmdash 摘要页生成注入
\ifmmdashabstract
\begin{abstract}
\input{mmdash/abstract-block}
\input{mmdash/keywords-block}
\end{abstract}
\fi
`

// cumcm2026TemplateArchive returns deterministic bytes so every Project can
// idempotently reference the same built-in template without asking a browser
// to manufacture or upload a ZIP.
func cumcm2026TemplateArchive() ([]byte, string, error) {
	manifest, err := json.MarshalIndent(cumcm2026TemplateManifest(), "", "  ")
	if err != nil {
		return nil, "", err
	}
	manifest = append(manifest, '\n')
	files := []struct {
		name    string
		content []byte
	}{
		{name: "main.tex", content: []byte(cumcm2026TemplateMainTex)},
		{name: "cumcmthesis.cls", content: cumcm2026TemplateCls},
		{name: "gbt7714-numerical.bst", content: cumcm2026TemplateBst},
		{name: "texfile/0-摘要.tex", content: []byte(cumcm2026TemplateAbstractTex)},
		{name: "mmdash-template.json", content: manifest},
	}

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		header.SetModTime(defaultTemplateTimestamp)
		header.SetMode(0o644)
		writer, createErr := archive.CreateHeader(header)
		if createErr != nil {
			return nil, "", createErr
		}
		if _, writeErr := writer.Write(file.content); writeErr != nil {
			return nil, "", writeErr
		}
	}
	if err = archive.Close(); err != nil {
		return nil, "", err
	}
	value := buffer.Bytes()
	digest := sha256.Sum256(value)
	return value, hex.EncodeToString(digest[:]), nil
}

// ensureCumcm2026Template registers the CUMCM 2026 built-in and retires the
// superseded cumcmthesis built-in from the Project's template list. Retired
// rows stay persisted so historical builds keep resolving their template.
func (service *Service) ensureCumcm2026Template(ctx context.Context, actorID, projectID string, templates []Template) ([]Template, error) {
	manifest := cumcm2026TemplateManifest()
	archive, digest, err := cumcm2026TemplateArchive()
	if err != nil {
		return templates, err
	}
	artifactID, versionID, err := service.Artifacts.ArchiveArticleTemplate(
		ctx, projectID, actorID, cumcm2026TemplateFilename,
		cumcm2026TemplateIdempotencyKey, digest, int64(len(archive)), bytes.NewReader(archive),
	)
	if err != nil {
		return templates, err
	}
	custom := make([]Template, 0, len(templates))
	var item Template
	for _, candidate := range templates {
		if candidate.Manifest.Name == manifest.Name && candidate.Manifest.Version == manifest.Version {
			if candidate.VersionID == versionID {
				item = candidate
			}
			// Keep one indistinguishable built-in copy in the product UI,
			// exactly like the default template bootstrap does.
			continue
		}
		if candidate.Manifest.Name == cumcm2026RetiredTemplateName {
			continue
		}
		custom = append(custom, candidate)
	}
	if item.TemplateID == "" {
		item, _, err = service.registerTemplate(ctx, actorID, projectID, artifactID, versionID, manifest)
		if err != nil {
			return templates, err
		}
	} else if item.Status == "validating" && item.TestBuildID == "" {
		revision := int64(0)
		build, _, buildErr := service.createBuild(ctx, actorID, projectID, BuildTemplateTest, "", &revision, item.TemplateID, manifest.Engine, manifest.BibliographyTool, "template-test:"+item.VersionID)
		if buildErr != nil {
			return templates, buildErr
		}
		item.TestBuildID = build.BuildID
	}
	return append([]Template{item}, custom...), nil
}
