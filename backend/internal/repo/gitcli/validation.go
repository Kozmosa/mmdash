package gitcli

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	fullSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	githubSegment  = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// GitHubRemote is the normalized, credential-free GitHub HTTPS identity.
type GitHubRemote struct {
	DisplayName  string
	FetchURL     string
	CanonicalURL string
}

// NormalizeGitHubURL accepts only a two-segment github.com HTTPS repository URL.
func NormalizeGitHubURL(raw string) (GitHubRemote, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil ||
		parsed.Scheme != "https" ||
		!strings.EqualFold(parsed.Hostname(), "github.com") ||
		parsed.Port() != "" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		parsed.RawPath != "" ||
		strings.Contains(parsed.EscapedPath(), "%") {
		return GitHubRemote{}, ErrRemoteInvalid
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) != 2 {
		return GitHubRemote{}, ErrRemoteInvalid
	}
	owner := segments[0]
	repository := strings.TrimSuffix(segments[1], ".git")
	if owner == "" || repository == "" ||
		owner == "." || owner == ".." ||
		repository == "." || repository == ".." ||
		!githubSegment.MatchString(owner) ||
		!githubSegment.MatchString(repository) {
		return GitHubRemote{}, ErrRemoteInvalid
	}
	display := owner + "/" + repository
	canonical := "https://github.com/" + display
	return GitHubRemote{
		CanonicalURL: canonical,
		DisplayName:  display,
		FetchURL:     canonical + ".git",
	}, nil
}

// ResolveLocalSource resolves symlinks and proves the source is below an allowlisted root.
func ResolveLocalSource(raw string, allowedRoots []string) (string, error) {
	if len(allowedRoots) == 0 {
		return "", ErrLocalDisabled
	}
	candidate, err := canonicalExistingDirectory(raw)
	if err != nil {
		return "", ErrPathInvalid
	}
	for _, configuredRoot := range allowedRoots {
		root, err := canonicalExistingDirectory(configuredRoot)
		if err != nil {
			return "", fmt.Errorf("resolve configured local root: %w", ErrPathInvalid)
		}
		if contained(root, candidate) {
			return candidate, nil
		}
	}
	return "", ErrStorageEscape
}

// ValidateFullSHA accepts immutable SHA-1 and future SHA-256 object identifiers.
func ValidateFullSHA(value string) error {
	if !fullSHAPattern.MatchString(value) {
		return ErrRevisionInvalid
	}
	return nil
}

// ValidateBranch rejects ref syntax that could be parsed as options or revisions.
func ValidateBranch(value string) error {
	if value == "" || len(value) > 255 ||
		strings.HasPrefix(value, "-") ||
		strings.HasPrefix(value, "/") ||
		strings.HasSuffix(value, "/") ||
		strings.HasSuffix(value, ".") ||
		strings.Contains(value, "..") ||
		strings.Contains(value, "//") ||
		strings.Contains(value, "@{") ||
		strings.ContainsAny(value, ` ~^:?*[\`) {
		return ErrBranchInvalid
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" ||
			strings.HasPrefix(segment, ".") ||
			strings.HasSuffix(segment, ".lock") {
			return ErrBranchInvalid
		}
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return ErrBranchInvalid
		}
	}
	return nil
}

// ValidateRepoPath validates a UTF-8 POSIX repository-relative path.
func ValidateRepoPath(value string, allowRoot bool) error {
	if value == "" {
		if allowRoot {
			return nil
		}
		return ErrPathInvalid
	}
	if len(value) > 4096 ||
		!utf8.ValidString(value) ||
		strings.ContainsRune(value, 0) ||
		strings.Contains(value, `\`) ||
		strings.HasPrefix(value, "/") ||
		filepath.VolumeName(value) != "" {
		return ErrPathInvalid
	}
	cleaned := path.Clean(value)
	if cleaned == "." ||
		cleaned == ".." ||
		strings.HasPrefix(cleaned, "../") ||
		cleaned != value {
		return ErrPathInvalid
	}
	return nil
}

func canonicalExistingDirectory(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", ErrPathInvalid
	}
	absolute, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", ErrPathInvalid
	}
	return filepath.Clean(resolved), nil
}

func contained(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative == "." ||
		(relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
			!filepath.IsAbs(relative))
}
