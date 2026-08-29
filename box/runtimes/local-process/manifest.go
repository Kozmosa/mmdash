package localprocess

// Manifest discovery is deliberately allowlist-driven: only documented files
// at the workspace root are ever considered. Conda-style manifests have no
// frozen solver contract yet and are rejected with a stable error instead of
// being silently ignored.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maximumManifestBytes = int64(4 << 20)

// Environment families supported by the first bare-metal delivery.
const (
	familyPipLock        = "pip-lock"         // requirements.lock (hash-pinned)
	familyPipRequirement = "pip-requirements" // requirements.txt (best effort unless pinned)
	familyUv             = "uv"               // pyproject.toml + uv.lock
	familyPoetry         = "poetry"           // pyproject.toml + poetry.lock
	familyPipenv         = "pipenv"           // Pipfile + Pipfile.lock
)

type manifestFile struct {
	Path    string
	Content []byte
	Hash    string
}

type manifestInfo struct {
	Family      string
	PrimaryPath string
	Files       []manifestFile
	BestEffort  bool
}

type manifestError struct {
	code string
	err  error
}

func (err manifestError) Error() string           { return err.err.Error() }
func (err manifestError) Unwrap() error           { return err.err }
func (err manifestError) EnvironmentCode() string { return err.code }

// Stable environment error codes shared with the Local Docker environment
// pipeline and extended for the bare-metal discovery rules.
const (
	EnvCodeInvalid     = "ENVIRONMENT_INVALID"
	EnvCodeUnsupported = "ENVIRONMENT_MANIFEST_UNSUPPORTED"
	EnvCodeAmbiguous   = "ENVIRONMENT_MANIFEST_AMBIGUOUS"
	EnvCodeBuildFailed = "ENVIRONMENT_BUILD_FAILED"
	EnvCodeUnavailable = "ENVIRONMENT_UNAVAILABLE"
)

func codedEnvironmentError(code string, err error) error {
	return manifestError{code: code, err: err}
}

// detectManifest inspects the workspace and either resolves exactly one
// environment family or fails with a stable error. An explicit selection from
// the frozen RunSpec is honoured; precedence is never guessed.
func detectManifest(workspace string, selection map[string]string) (*manifestInfo, error) {
	if workspace == "" {
		return nil, codedEnvironmentError(EnvCodeInvalid, errors.New("environment workspace is required"))
	}
	// Conda-style manifests are explicitly unsupported in the first delivery.
	for _, name := range []string{"environment.yml", "environment.yaml"} {
		if existsRegular(filepath.Join(workspace, name)) {
			return nil, codedEnvironmentError(EnvCodeUnsupported,
				fmt.Errorf("dependency manifest %q has no frozen solver contract yet; use pip, uv, poetry or pipenv manifests", name))
		}
	}
	families := make([]manifestInfo, 0, 2)
	if file := readManifestFile(workspace, "requirements.lock"); file != nil {
		families = append(families, manifestInfo{Family: familyPipLock, PrimaryPath: "requirements.lock", Files: []manifestFile{*file}})
	}
	if file := readManifestFile(workspace, "requirements.txt"); file != nil {
		families = append(families, manifestInfo{
			Family: familyPipRequirement, PrimaryPath: "requirements.txt",
			Files: []manifestFile{*file}, BestEffort: !fullyHashPinned(file.Content),
		})
	}
	if file := readManifestFile(workspace, "pyproject.toml"); file != nil {
		if lock := readManifestFile(workspace, "uv.lock"); lock != nil {
			families = append(families, manifestInfo{
				Family: familyUv, PrimaryPath: "uv.lock",
				Files: []manifestFile{*file, *lock},
			})
		}
		if lock := readManifestFile(workspace, "poetry.lock"); lock != nil {
			families = append(families, manifestInfo{
				Family: familyPoetry, PrimaryPath: "poetry.lock",
				Files: []manifestFile{*file, *lock},
			})
		}
	}
	if pipfile := readManifestFile(workspace, "Pipfile"); pipfile != nil {
		if lock := readManifestFile(workspace, "Pipfile.lock"); lock != nil {
			families = append(families, manifestInfo{
				Family: familyPipenv, PrimaryPath: "Pipfile",
				Files: []manifestFile{*pipfile, *lock},
			})
		}
	}
	switch len(families) {
	case 0:
		return nil, nil
	case 1:
		return &families[0], nil
	}
	selected := strings.TrimSpace(selection["python_manifest"])
	if selected != "" {
		for _, family := range families {
			if family.PrimaryPath == selected || filepath.ToSlash(family.PrimaryPath) == filepath.ToSlash(selected) {
				return &family, nil
			}
		}
		return nil, codedEnvironmentError(EnvCodeInvalid,
			fmt.Errorf("frozen RunSpec selected %q but no supported environment family matches it", selected))
	}
	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.PrimaryPath)
	}
	return nil, codedEnvironmentError(EnvCodeAmbiguous,
		fmt.Errorf("multiple environment families are present (%s); the frozen RunSpec must select one explicitly", strings.Join(names, ", ")))
}

// readManifestFile returns the workspace-root file when it exists as a regular
// file within the Box size budget. Symlinks are rejected so a transferred
// source tree cannot redirect dependency resolution.
func readManifestFile(workspace, name string) *manifestFile {
	path := filepath.Join(workspace, name)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	if info.Size() > maximumManifestBytes {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	digest := sha256.Sum256(content)
	return &manifestFile{Path: name, Content: content, Hash: hex.EncodeToString(digest[:])}
}

func existsRegular(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

// fullyHashPinned reports whether every requirement line carries at least one
// --hash option, which pip requires for --require-hashes verification.
func fullyHashPinned(content []byte) bool {
	requirements := false
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "-") {
			// Options such as --index-url are allowed; only requirement lines
			// must be pinned.
			continue
		}
		requirements = true
		if !strings.Contains(line, "--hash=") {
			return false
		}
	}
	return requirements
}

// manifestEvidence produces the provider-neutral result evidence.
func manifestEvidence(files []manifestFile) ([]string, map[string]string) {
	paths := make([]string, 0, len(files))
	hashes := make(map[string]string, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
		hashes[file.Path] = file.Hash
	}
	return paths, hashes
}
