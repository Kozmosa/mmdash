package gitcli

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNormalizeGitHubURL(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{input: "https://github.com/Kozmosa/mmdash", valid: true},
		{input: "https://github.com/Kozmosa/mmdash.git", valid: true},
		{input: "http://github.com/Kozmosa/mmdash", valid: false},
		{input: "https://token@github.com/Kozmosa/mmdash", valid: false},
		{input: "https://github.com/Kozmosa/mmdash?token=secret", valid: false},
		{input: "https://github.com/Kozmosa/mmdash/extra", valid: false},
		{input: "https://github.com/Kozmosa/%2e%2e", valid: false},
		{input: "https://github.example/Kozmosa/mmdash", valid: false},
	}
	for _, test := range tests {
		remote, err := NormalizeGitHubURL(test.input)
		if test.valid && err != nil {
			t.Fatalf("%s should be valid: %v", test.input, err)
		}
		if !test.valid && !errors.Is(err, ErrRemoteInvalid) {
			t.Fatalf("%s should be rejected: %v", test.input, err)
		}
		if test.valid && remote.FetchURL != "https://github.com/Kozmosa/mmdash.git" {
			t.Fatalf("unexpected fetch URL: %s", remote.FetchURL)
		}
	}
}

func TestValidateBranchSHAAndRepoPath(t *testing.T) {
	for _, branch := range []string{"main", "feature/repo-v1", "论文"} {
		if err := ValidateBranch(branch); err != nil {
			t.Fatalf("valid branch %q: %v", branch, err)
		}
	}
	for _, branch := range []string{"-main", "../main", "a..b", "a.lock", "a\\b", "a b"} {
		if !errors.Is(ValidateBranch(branch), ErrBranchInvalid) {
			t.Fatalf("invalid branch accepted: %q", branch)
		}
	}
	if err := ValidateFullSHA("0123456789abcdef0123456789abcdef01234567"); err != nil {
		t.Fatalf("valid SHA rejected: %v", err)
	}
	if !errors.Is(ValidateFullSHA("main"), ErrRevisionInvalid) {
		t.Fatal("moving branch must not pass immutable SHA validation")
	}
	for _, valid := range []string{"paper/main.md", "目录/结果 #1.txt", "line\nname.txt"} {
		if err := ValidateRepoPath(valid, false); err != nil {
			t.Fatalf("valid path %q: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "/etc/passwd", "../secret", "a/../b", `a\b`, "a//b"} {
		if !errors.Is(ValidateRepoPath(invalid, false), ErrPathInvalid) {
			t.Fatalf("invalid path accepted: %q", invalid)
		}
	}
	if err := ValidateRepoPath("", true); err != nil {
		t.Fatalf("root path should be accepted: %v", err)
	}
}

func TestResolveLocalSourceEnforcesAllowlistAndSymlinks(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	outside := filepath.Join(root, "outside")
	mustMkdir(t, allowed)
	mustMkdir(t, outside)
	source := filepath.Join(allowed, "repo")
	mustMkdir(t, source)
	resolved, err := ResolveLocalSource(source, []string{allowed})
	if err != nil || resolved == "" {
		t.Fatalf("resolve allowed source: %q, %v", resolved, err)
	}
	if _, err := ResolveLocalSource(outside, []string{allowed}); !errors.Is(err, ErrStorageEscape) {
		t.Fatalf("outside source should be rejected: %v", err)
	}
	if _, err := ResolveLocalSource(source, nil); !errors.Is(err, ErrLocalDisabled) {
		t.Fatalf("empty allowlist should disable Local Git: %v", err)
	}

	link := filepath.Join(allowed, "escape")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink permission unavailable: %v", err)
		}
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := ResolveLocalSource(link, []string{allowed}); !errors.Is(err, ErrStorageEscape) {
		t.Fatalf("symlink escape should be rejected: %v", err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("create test directory: %v", err)
	}
}
