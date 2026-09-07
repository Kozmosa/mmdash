package article

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
)

func TestCumcmTemplateArchiveIsDeterministicAndSelfDescribing(t *testing.T) {
	first, firstSHA, err := cumcmTemplateArchive()
	if err != nil {
		t.Fatal(err)
	}
	second, secondSHA, err := cumcmTemplateArchive()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || firstSHA != secondSHA {
		t.Fatal("built-in CUMCM template archive is not deterministic")
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
	for _, name := range []string{"main.tex", "cumcmthesis.cls", "gbt7714-numerical.bst", "mmdash-template.json"} {
		if len(entries[name]) == 0 {
			t.Fatalf("built-in CUMCM template is missing %s", name)
		}
	}
	if !bytes.Contains(entries["gbt7714-numerical.bst"], []byte("GB/T 7714 BibTeX Style")) {
		t.Fatal("gbt7714-numerical.bst was not embedded intact")
	}
	for _, expected := range []string{
		`\documentclass[withoutpreface,bwprint]{cumcmthesis}`,
		`\input{.mmdash/metadata}`,
		`\begin{abstract}`,
		`\input{.mmdash/abstract-block}`,
		`\input{.mmdash/keywords-block}`,
		`\input{generated-content}`,
		`\bibliographystyle{gbt7714-numerical}`,
		`\bibliography{references}`,
		`\renewcommand*{\@title}{}`,
	} {
		if !bytes.Contains(entries["main.tex"], []byte(expected)) {
			t.Fatalf("CUMCM main.tex lacks %q: %s", expected, entries["main.tex"])
		}
	}
	if bytes.Contains(entries["main.tex"], []byte(`\nianyue`)) {
		t.Fatal("the nianyue shim must not come back; the worker probes field commands")
	}
	if bytes.Contains(entries["main.tex"], []byte(`\input{.mmdash/bibliography-block}`)) {
		t.Fatal("native wiring replaced the bibliography block input")
	}
	if bytes.Contains(entries["main.tex"], []byte(`\input{texfile/`)) {
		t.Fatal("CUMCM main.tex must not input the original scaffold sections")
	}
	if !bytes.Contains(entries["cumcmthesis.cls"], []byte(`\newcommand\keywords[1]{%`)) {
		t.Fatal("cumcmthesis.cls was not embedded byte-for-byte")
	}
	var manifest TemplateManifest
	if err = json.Unmarshal(entries["mmdash-template.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest != cumcmTemplateManifest() {
		t.Fatalf("unexpected built-in CUMCM manifest: %#v", manifest)
	}
	expected := cumcmTemplateManifest()
	if expected.SchemaVersion != "1.1" || expected.Engine != "xelatex" || expected.BibliographyTool != "bibtex" ||
		expected.FieldProfile != "cumcm" || expected.BibliographyMode != "native" || expected.BodyLayout != "single" ||
		expected.AbstractTarget != ".mmdash/abstract-block.tex" || expected.ContentTarget == expected.Entrypoint {
		t.Fatalf("built-in CUMCM manifest does not match the template spec: %#v", expected)
	}
}

func TestEnsureCumcmTemplateIsProjectIdempotentAndCoexistsWithDefault(t *testing.T) {
	store := &articleTestStore{}
	service := testService(store, &articleTestWorkspace{})
	service.Artifacts = &articleTestArtifacts{}

	templates, err := service.ensureDefaultTemplate(context.Background(), "user-1", "project-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	templates, err = service.ensureCumcmTemplate(context.Background(), "user-1", "project-1", templates)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 2 || len(store.templates) != 2 || len(store.builds) != 2 {
		t.Fatalf("CUMCM template was not registered beside the default with one validation build: %#v %#v", store.templates, store.builds)
	}
	item := templates[0]
	if item.Manifest != cumcmTemplateManifest() || item.TestBuildID == "" {
		t.Fatalf("CUMCM template registration is incomplete: %#v", item)
	}
	for _, build := range store.builds {
		if build.BuildKind != BuildTemplateTest {
			t.Fatalf("built-in bootstrap queued a non-test build: %#v", build)
		}
	}

	templates, err = service.ensureCumcmTemplate(context.Background(), "user-1", "project-1", templates)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 2 || len(store.templates) != 2 || len(store.builds) != 2 {
		t.Fatalf("CUMCM template bootstrap was not idempotent: %#v %#v", store.templates, store.builds)
	}
}
