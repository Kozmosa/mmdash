package gitcli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var storageKeyPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`,
)

// Layout contains only Core-generated managed paths.
type Layout struct {
	Bare       string
	Checkouts  string
	Repository string
	Worktrees  map[string]string
}

// Storage manages repository directories below one canonical root.
type Storage struct {
	root string
}

// NewStorage creates and canonicalizes the managed repository root.
func NewStorage(root string) (*Storage, error) {
	if strings.TrimSpace(root) == "" {
		return nil, ErrPathInvalid
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create repository root: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("canonicalize repository root: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return nil, ErrPathInvalid
	}
	return &Storage{root: filepath.Clean(canonical)}, nil
}

// Root returns the configured path for readiness checks, never for HTTP.
func (storage *Storage) Root() string {
	return storage.root
}

// Layout returns the fixed repository directory layout.
func (storage *Storage) Layout(storageKey string) (Layout, error) {
	repository, err := storage.repositoryPath(storageKey)
	if err != nil {
		return Layout{}, err
	}
	return Layout{
		Bare:       filepath.Join(repository, "bare.git"),
		Checkouts:  filepath.Join(repository, "checkouts"),
		Repository: repository,
		Worktrees: map[string]string{
			"code":    filepath.Join(repository, "worktrees", "code"),
			"article": filepath.Join(repository, "worktrees", "article"),
			"result":  filepath.Join(repository, "worktrees", "result"),
		},
	}, nil
}

// Prepare creates an isolated same-filesystem staging directory for clone/configuration.
func (storage *Storage) Prepare(storageKey string) (string, error) {
	if !storageKeyPattern.MatchString(storageKey) {
		return "", ErrPathInvalid
	}
	if _, err := storage.repositoryPath(storageKey); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(storage.root, ".stage-"+storageKey+"-")
	if err != nil {
		return "", fmt.Errorf("create repository staging directory: %w", err)
	}
	if !contained(storage.root, staging) {
		_ = os.Remove(staging)
		return "", ErrStorageEscape
	}
	return staging, nil
}

// Promote atomically installs a completed staging repository.
func (storage *Storage) Promote(staging, storageKey string) error {
	canonicalStaging, err := filepath.EvalSymlinks(staging)
	if err != nil || !contained(storage.root, canonicalStaging) {
		return ErrStorageEscape
	}
	if !strings.HasPrefix(filepath.Base(canonicalStaging), ".stage-"+storageKey+"-") {
		return ErrPathInvalid
	}
	destination, err := storage.repositoryPath(storageKey)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("managed repository destination already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(canonicalStaging, destination); err != nil {
		return fmt.Errorf("promote managed repository: %w", err)
	}
	return nil
}

// Discard removes only a verified staging directory created for one storage key.
func (storage *Storage) Discard(staging, storageKey string) error {
	canonicalStaging, err := filepath.EvalSymlinks(staging)
	if err != nil || !contained(storage.root, canonicalStaging) {
		return ErrStorageEscape
	}
	if !strings.HasPrefix(filepath.Base(canonicalStaging), ".stage-"+storageKey+"-") {
		return ErrPathInvalid
	}
	return os.RemoveAll(canonicalStaging)
}

// RemoveRepository removes exactly one Core-generated managed repository.
// Missing directories are already clean; symlinks and malformed keys are
// rejected before any recursive deletion is attempted.
func (storage *Storage) RemoveRepository(storageKey string) error {
	repository, err := storage.repositoryPath(storageKey)
	if err != nil {
		return err
	}
	info, err := os.Lstat(repository)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrStorageEscape
	}
	resolved, err := filepath.EvalSymlinks(repository)
	if err != nil || resolved != repository || !contained(storage.root, resolved) {
		return ErrStorageEscape
	}
	return os.RemoveAll(repository)
}

// ManagedPath joins a reviewed relative path and refuses symlink escapes.
func (storage *Storage) ManagedPath(storageKey, relative string) (string, error) {
	repository, err := storage.repositoryPath(storageKey)
	if err != nil {
		return "", err
	}
	if relative == "" ||
		filepath.IsAbs(relative) ||
		filepath.VolumeName(relative) != "" {
		return "", ErrPathInvalid
	}
	cleaned := filepath.Clean(relative)
	if cleaned == "." || cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", ErrPathInvalid
	}
	candidate := filepath.Join(repository, cleaned)
	if !contained(repository, candidate) {
		return "", ErrStorageEscape
	}
	if _, err := os.Lstat(candidate); err == nil {
		resolved, resolveErr := filepath.EvalSymlinks(candidate)
		if resolveErr != nil || !contained(repository, resolved) {
			return "", ErrStorageEscape
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return candidate, nil
}

// Check proves the root supports writes and atomic rename.
func (storage *Storage) Check() error {
	directory, err := os.MkdirTemp(storage.root, ".readiness-")
	if err != nil {
		return fmt.Errorf("create repository readiness directory: %w", err)
	}
	defer os.RemoveAll(directory)
	source := filepath.Join(directory, "source")
	destination := filepath.Join(directory, "destination")
	if err := os.WriteFile(source, []byte("ready"), 0o600); err != nil {
		return fmt.Errorf("write repository readiness file: %w", err)
	}
	if err := os.Rename(source, destination); err != nil {
		return fmt.Errorf("atomically rename repository readiness file: %w", err)
	}
	return nil
}

// Size reports bytes stored below the managed root without following symlinks.
func (storage *Storage) Size() (int64, error) {
	var total int64
	err := filepath.WalkDir(storage.root, func(
		path string,
		entry fs.DirEntry,
		walkErr error,
	) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func (storage *Storage) repositoryPath(storageKey string) (string, error) {
	if !storageKeyPattern.MatchString(storageKey) {
		return "", ErrPathInvalid
	}
	candidate := filepath.Join(storage.root, strings.ToLower(storageKey))
	if !contained(storage.root, candidate) {
		return "", ErrStorageEscape
	}
	if info, err := os.Lstat(candidate); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", ErrStorageEscape
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil || !contained(storage.root, resolved) {
			return "", ErrStorageEscape
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return candidate, nil
}
