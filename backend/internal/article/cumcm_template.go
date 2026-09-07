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
	cumcmTemplateFilename       = "mmdash-cumcm-template.zip"
	cumcmTemplateIdempotencyKey = "article-cumcm-template:1.1.0"
)

// cumcmTemplateCls ships the original contest class byte-for-byte so the
// theorem environments, cover strings, and fonts stay identical to the
// published cumcmthesis template. The GB/T 7714 style ships with the template
// because the pinned Worker image does not include the gbt7714 package and
// the bibliography style belongs to the template, not the toolchain.
//
//go:embed templates/cumcm/cumcmthesis.cls
var cumcmTemplateCls []byte

//go:embed templates/cumcm/gbt7714-numerical.bst
var cumcmTemplateBst []byte

func cumcmTemplateManifest() TemplateManifest {
	return TemplateManifest{
		SchemaVersion:      "1.1",
		Name:               "CUMCM 国赛论文模板（cumcmthesis）",
		Version:            "1.1.0",
		Entrypoint:         "main.tex",
		Output:             "main.pdf",
		ContentTarget:      "generated-content.tex",
		BibliographyTarget: "references.bib",
		Engine:             "xelatex",
		BibliographyTool:   "bibtex",
		AbstractTarget:     ".mmdash/abstract-block.tex",
		BodyLayout:         "single",
		FieldProfile:       "cumcm",
		FigureDir:          "figures",
		BibliographyMode:   "native",
	}
}

const cumcmTemplateMainTex = `% !TeX program = xelatex
% mmdash CUMCM built-in template, parallel to the default template. The class
% file is the original cumcmthesis.cls; only this main file wires the mmdash
% generated slots into the contest layout.
\documentclass[withoutpreface,bwprint]{cumcmthesis}

% Extra packages on top of what cumcmthesis.cls already loads.
\usepackage{xurl}
\usepackage{mathtools}

% Pandoc fragment compatibility, same as the default template.
\providecommand{\tightlist}{%
  \setlength{\itemsep}{0pt}\setlength{\parskip}{0pt}}

\graphicspath{{figures/}}

% The worker probes the template for each CUMCM field command and emits
% \title/\tihao/... only for enabled fields the class actually defines, and
% resets \@title so \maketitle stays legal when no title is selected, e.g.
% during template validation builds.
\makeatletter
\renewcommand*{\@title}{}
\makeatother
\input{.mmdash/metadata}

\begin{document}

\maketitle

\ifmmdashabstract
\begin{abstract}
\input{.mmdash/abstract-block}
\input{.mmdash/keywords-block}
\end{abstract}
\fi

\input{generated-content}

% Native bibliography wiring: the Worker keeps Zotero citations as \cite
% commands and writes the frozen references into references.bib; bibtex and
% the shipped GB/T 7714 numerical style render the list here.
\bibliographystyle{gbt7714-numerical}
\bibliography{references}

\end{document}
`

// cumcmTemplateArchive returns deterministic bytes so every Project can
// idempotently reference the same built-in CUMCM template without asking a
// browser to manufacture or upload a ZIP.
func cumcmTemplateArchive() ([]byte, string, error) {
	manifest, err := json.MarshalIndent(cumcmTemplateManifest(), "", "  ")
	if err != nil {
		return nil, "", err
	}
	manifest = append(manifest, '\n')
	files := []struct {
		name    string
		content []byte
	}{
		{name: "main.tex", content: []byte(cumcmTemplateMainTex)},
		{name: "cumcmthesis.cls", content: cumcmTemplateCls},
		{name: "gbt7714-numerical.bst", content: cumcmTemplateBst},
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

// ensureCumcmTemplate mirrors ensureDefaultTemplate for the CUMCM built-in so
// the two built-ins roll out in parallel without touching the default
// template path. The duplication is deliberate and should collapse into a
// shared helper when the template module is generalized.
func (service *Service) ensureCumcmTemplate(ctx context.Context, actorID, projectID string, templates []Template) ([]Template, error) {
	manifest := cumcmTemplateManifest()
	archive, digest, err := cumcmTemplateArchive()
	if err != nil {
		return templates, err
	}
	artifactID, versionID, err := service.Artifacts.ArchiveArticleTemplate(
		ctx, projectID, actorID, cumcmTemplateFilename,
		cumcmTemplateIdempotencyKey, digest, int64(len(archive)), bytes.NewReader(archive),
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
