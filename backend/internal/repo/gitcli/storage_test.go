package gitcli

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const testStorageKey = "57c7c47f-a671-43df-938d-c813ffef8a99"

func TestStorageStagesPromotesAndBuildsManagedPaths(t *testing.T) {
	storage, err := NewStorage(filepath.Join(t.TempDir(), "repos"))
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	if err := storage.Check(); err != nil {
		t.Fatalf("check storage: %v", err)
	}
	staging, err := storage.Prepare(testStorageKey)
	if err != nil {
		t.Fatalf("prepare storage: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(staging, "bare.git"), 0o700); err != nil {
		t.Fatalf("create staged bare repository: %v", err)
	}
	if err := storage.Promote(staging, testStorageKey); err != nil {
		t.Fatalf("promote storage: %v", err)
	}
	layout, err := storage.Layout(testStorageKey)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if filepath.Base(layout.Bare) != "bare.git" ||
		filepath.Base(layout.Worktrees["article"]) != "article" {
		t.Fatalf("unexpected layout: %+v", layout)
	}
	path, err := storage.ManagedPath(testStorageKey, "checkouts/example")
	if err != nil || !contained(layout.Repository, path) {
		t.Fatalf("managed path: %q, %v", path, err)
	}
}

func TestStorageRejectsKeysTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	storage, err := NewStorage(filepath.Join(root, "repos"))
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	if _, err := storage.Layout("../outside"); !errors.Is(err, ErrPathInvalid) {
		t.Fatalf("invalid key should be rejected: %v", err)
	}
	if _, err := storage.ManagedPath(testStorageKey, "../outside"); !errors.Is(err, ErrPathInvalid) {
		t.Fatalf("traversal should be rejected: %v", err)
	}

	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatalf("create outside directory: %v", err)
	}
	link := filepath.Join(storage.Root(), testStorageKey)
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink permission unavailable: %v", err)
		}
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := storage.Layout(testStorageKey); !errors.Is(err, ErrStorageEscape) {
		t.Fatalf("repository symlink escape should be rejected: %v", err)
	}
}
