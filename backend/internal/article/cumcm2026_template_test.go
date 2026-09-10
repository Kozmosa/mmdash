package article

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
)

func TestCumcm2026TemplateArchiveIsDeterministicAndSelfDescribing(t *testing.T) {
	first, firstSHA, err := cumcm2026TemplateArchive()
	if err != nil {
		t.Fatal(err)
	}
	second, secondSHA, err := cumcm2026TemplateArchive()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || firstSHA != secondSHA {
		t.Fatal("built-in CUMCM 2026 template archive is not deterministic")
	}
	reader, err := zip.NewReader(bytes.NewReader(first), int64(len(first)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{}
	for _, file := range reader.File {
		stream, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		entries[file.Name], err = io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{
		"main.tex", "cumcmthesis.cls", "gbt7714-numerical.bst",
		"texfile/0-摘要.tex", "mmdash-template.json",
	} {
		if len(entries[name]) == 0 {
			t.Fatalf("built-in CUMCM 2026 template is missing %s", name)
		}
	}
	if !bytes.Contains(entries["gbt7714-numerical.bst"], []byte("GB/T 7714 BibTeX Style")) {
		t.Fatal("gbt7714-numerical.bst was not embedded intact")
	}
	for _, expected := range []string{
		`\documentclass[withoutpreface,bwprint]{cumcmthesis}`,
		`\input{mmdash/metadata}`,
		`\input{texfile/0-摘要}`,
		`\input{texfile/body}`,
		`\bibliographystyle{gbt7714-numerical}`,
		`\bibliography{references}`,
		`\renewcommand*{\@title}{}`,
		`\usepackage{subcaption}`,
	} {
		if !bytes.Contains(entries["main.tex"], []byte(expected)) {
			t.Fatalf("CUMCM 2026 main.tex lacks %q: %s", expected, entries["main.tex"])
		}
	}
	for _, expected := range []string{
		`\begin{abstract}`,
		`\input{mmdash/abstract-block}`,
		`\input{mmdash/keywords-block}`,
		`\end{abstract}`,
	} {
		if !bytes.Contains(entries["texfile/0-摘要.tex"], []byte(expected)) {
			t.Fatalf("CUMCM 2026 abstract wrapper lacks %q", expected)
		}
	}
	if bytes.Contains(entries["main.tex"], []byte(`\input{.mmdash/`)) {
		t.Fatal("dot directories are rejected by openin_any=p; generated blocks must live under mmdash/")
	}
	if !bytes.Contains(entries["cumcmthesis.cls"], []byte(`\newcommand\keywords[1]{%`)) {
		t.Fatal("cumcmthesis.cls was not embedded intact")
	}
	// The only deliberate class divergence: conditional font fallbacks so the
	// class also builds on the Linux toolchain without licensed Windows fonts.
	for _, expected := range []string{
		`\IfFontExistsTF{Times New Roman}`,
		`\IfFontExistsTF{Arial}`,
		`\IfFontExistsTF{simkai.ttf}`,
		`\IfFontExistsTF{SimSun}`,
	} {
		if !bytes.Contains(entries["cumcmthesis.cls"], []byte(expected)) {
			t.Fatalf("cumcmthesis.cls lacks the font fallback guard %q", expected)
		}
	}
	var manifest TemplateManifest
	if err = json.Unmarshal(entries["mmdash-template.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest != cumcm2026TemplateManifest() {
		t.Fatalf("unexpected built-in CUMCM 2026 manifest: %#v", manifest)
	}
	expected := cumcm2026TemplateManifest()
	if expected.SchemaVersion != "1.1" || expected.Engine != "xelatex" || expected.BibliographyTool != "bibtex" ||
		expected.FieldProfile != "cumcm" || expected.BibliographyMode != "native" || expected.BodyLayout != "sections" ||
		expected.AbstractTarget != "mmdash/abstract-block.tex" || expected.ContentTarget != "texfile/body.tex" ||
		expected.ContentTarget == expected.Entrypoint {
		t.Fatalf("built-in CUMCM 2026 manifest does not match the template spec: %#v", expected)
	}
}

func TestEnsureCumcm2026TemplateIsProjectIdempotentAndRetiresCumcmthesis(t *testing.T) {
	store := &articleTestStore{}
	service := testService(store, &articleTestWorkspace{})
	service.Artifacts = &articleTestArtifacts{}

	templates, err := service.ensureDefaultTemplate(context.Background(), "user-1", "project-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	templates, err = service.ensureCumcm2026Template(context.Background(), "user-1", "project-1", templates)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 2 || len(store.templates) != 2 || len(store.builds) != 2 {
		t.Fatalf("CUMCM 2026 template was not registered beside the default with one validation build: %#v %#v", store.templates, store.builds)
	}
	item := templates[0]
	if item.Manifest != cumcm2026TemplateManifest() || item.TestBuildID == "" {
		t.Fatalf("CUMCM 2026 template registration is incomplete: %#v", item)
	}
	for _, build := range store.builds {
		if build.BuildKind != BuildTemplateTest {
			t.Fatalf("built-in bootstrap queued a non-test build: %#v", build)
		}
	}

	templates, err = service.ensureCumcm2026Template(context.Background(), "user-1", "project-1", templates)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 2 || len(store.templates) != 2 || len(store.builds) != 2 {
		t.Fatalf("CUMCM 2026 template bootstrap was not idempotent: %#v %#v", store.templates, store.builds)
	}

	// A persisted row of the retired cumcmthesis built-in disappears from the
	// list but stays in the store for historical builds.
	retired := Template{TemplateID: "retired-1", ProjectID: "project-1", VersionID: "v-old",
		Manifest: TemplateManifest{SchemaVersion: "1.1", Name: cumcm2026RetiredTemplateName, Version: "1.4.0"},
		Status:   "ready"}
	store.templates = append(store.templates, retired)
	templates, err = service.ensureCumcm2026Template(context.Background(), "user-1", "project-1", append(templates, retired))
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range templates {
		if candidate.Manifest.Name == cumcm2026RetiredTemplateName {
			t.Fatal("retired cumcmthesis built-in must not surface in the template list")
		}
	}
	if len(templates) != 2 {
		t.Fatalf("unexpected template list after retirement: %#v", templates)
	}
}
