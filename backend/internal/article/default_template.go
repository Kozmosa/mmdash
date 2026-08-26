package article

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

const (
	defaultTemplateFilename       = "mmdash-default-template.zip"
	defaultTemplateIdempotencyKey = "article-default-template:1.0.2"
)

var defaultTemplateTimestamp = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

func defaultTemplateManifest() TemplateManifest {
	return TemplateManifest{
		SchemaVersion:      "1.0",
		Name:               "mmdash 默认论文模板",
		Version:            "1.0.2",
		Entrypoint:         "main.tex",
		Output:             "main.pdf",
		ContentTarget:      "generated-content.tex",
		BibliographyTarget: "references.bib",
		Engine:             "xelatex",
		BibliographyTool:   "none",
	}
}

// defaultTemplateArchive returns deterministic bytes so every Project can
// idempotently reference the same built-in template content without asking a
// browser to manufacture or upload a ZIP.
func defaultTemplateArchive() ([]byte, string, error) {
	manifest, err := json.MarshalIndent(defaultTemplateManifest(), "", "  ")
	if err != nil {
		return nil, "", err
	}
	manifest = append(manifest, '\n')
	files := []struct {
		name    string
		content []byte
	}{
		{
			name: "main.tex",
			content: []byte(`\documentclass[UTF8,12pt]{ctexart}
\usepackage[a4paper,margin=2.5cm]{geometry}
\usepackage{amsmath,amssymb}
\usepackage{booktabs,longtable,array,calc}
\usepackage{graphicx}
\usepackage{subcaption}
\usepackage{hyperref}
\usepackage{xcolor}
\providecommand{\tightlist}{\setlength{\itemsep}{0pt}\setlength{\parskip}{0pt}}
\begin{document}
\input{generated-content}
\end{document}
`),
		},
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
