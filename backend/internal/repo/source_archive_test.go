package repo

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

type sourceRepositoryStore struct{ repository Repository }

func (store sourceRepositoryStore) GetByProject(context.Context, string) (Repository, error) {
	return store.repository, nil
}

type sourceIDGenerator struct{ value string }

func (generator sourceIDGenerator) New() (string, error) { return generator.value, nil }

func TestSourceArchiveExportsOnlyTheFrozenCommitTree(t *testing.T) {
	reader, repository, head := readerFixture(t)
	service := SourceArchiveService{
		Generator:    sourceIDGenerator{value: "00000000-0000-4000-8000-000000000105"},
		Repositories: sourceRepositoryStore{repository: repository},
		Runtime:      Runtime{Clock: reader.Clock, Git: reader.Git, Storage: reader.Storage},
		Storage:      reader.Storage,
	}
	var output bytes.Buffer
	if err := service.WriteSourceArchive(context.Background(), repository.ProjectID, head, &output); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, item := range archive.File {
		if item.Name == ".git" || strings.HasPrefix(item.Name, ".git/") {
			t.Fatalf("Git administrative data leaked into source archive: %s", item.Name)
		}
		input, err := item.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(input)
		_ = input.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[item.Name] = string(contents)
	}
	if files["README.md"] != "updated\n" || files["new.txt"] != "rename me\n" || files["old.txt"] != "" {
		t.Fatalf("archive was not pinned to the requested commit: %#v", files)
	}
}
