package article

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

func TestDefaultTemplateArchiveIsDeterministicAndSelfDescribing(t *testing.T) {
	first, firstSHA, err := defaultTemplateArchive()
	if err != nil {
		t.Fatal(err)
	}
	second, secondSHA, err := defaultTemplateArchive()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || firstSHA != secondSHA {
		t.Fatal("built-in template archive is not deterministic")
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
	if !bytes.Contains(entries["main.tex"], []byte(`\input{.mmdash/generated-content}`)) ||
		!bytes.Contains(entries["main.tex"], []byte(`\usepackage{booktabs,longtable,array,calc}`)) {
		t.Fatalf("default main.tex lacks the generated content slot or paper packages: %s", entries["main.tex"])
	}
	var manifest TemplateManifest
	if err = json.Unmarshal(entries["mmdash-template.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest != defaultTemplateManifest() || manifest.Engine != "xelatex" {
		t.Fatalf("unexpected built-in template manifest: %#v", manifest)
	}
}
