package config

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadSupportsHTTPAndPreservesRuntimeSettings(t *testing.T) {
	root := t.TempDir()
	want := Default(root)
	want.ControlURL = "http://localhost:3001"
	want.Name = "test-box"
	want.LocalDocker.Image = "example/sandbox:test"
	if err := Save(root, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.ControlURL != want.ControlURL || got.Name != want.Name || got.LocalDocker.Image != want.LocalDocker.Image {
		t.Fatalf("loaded config %#v, want %#v", got, want)
	}
	if gotPath := Path(root); gotPath != filepath.Join(root, "config.json") {
		t.Fatalf("config path %q", gotPath)
	}
}

func TestValidateRejectsNonHTTPControlURL(t *testing.T) {
	cfg := Default("")
	cfg.ControlURL = "file:///tmp/mmdash"
	if err := Validate(cfg); err == nil {
		t.Fatal("file URL was accepted")
	}
}
