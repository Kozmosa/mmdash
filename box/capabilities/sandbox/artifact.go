package sandbox

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mmdash/mmdash/box/contracts"
)

const (
	manifestFilename = "manifest.json"
	maxArtifactBytes = int64(10 << 30)
	maxArtifactFiles = 10_000
	maxSummaryBytes  = int64(20_000)
)

// GenerateManifest creates the authoritative result manifest from the actual
// Sandbox output. Experiment programs only write result files; they cannot
// choose the terminal status, experiment identity, hashes, or packaged paths.
type ManifestInput struct {
	ExperimentID    string
	SourceCommit    string
	ResultDirectory string
	Status          string
	StartedAt       time.Time
	FinishedAt      time.Time
	Runtime         string
	RuntimeVersion  string
	LogsTruncated   bool
	ExitCode        *int
	Environment     *contracts.ManifestEnvironment
}

func GenerateManifest(root string, input ManifestInput, diskLimit int64) (contracts.Manifest, error) {
	if root == "" || input.ExperimentID == "" || diskLimit < 1 {
		return contracts.Manifest{}, errors.New("invalid manifest generation input")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return contracts.Manifest{}, err
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return contracts.Manifest{}, errors.New("Sandbox output is unavailable")
	}
	limit := diskLimit
	if limit > maxArtifactBytes {
		limit = maxArtifactBytes
	}
	manifest := contracts.Manifest{
		SchemaVersion:   "2",
		ExperimentID:    input.ExperimentID,
		SourceCommit:    input.SourceCommit,
		ResultDirectory: input.ResultDirectory,
		Status:          input.Status,
		StartedAt:       input.StartedAt,
		FinishedAt:      input.FinishedAt,
		Runtime:         input.Runtime,
		RuntimeVersion:  input.RuntimeVersion,
		LogsTruncated:   input.LogsTruncated,
		ExitCode:        input.ExitCode,
		Environment:     input.Environment,
		Files:           []contracts.ManifestFile{},
	}
	var total int64
	err = filepath.WalkDir(rootReal, func(localPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(rootReal, localPath)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in Sandbox output: %s", relative)
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing unsupported Sandbox output type: %s", relative)
		}
		// The wrapper owns manifest.json. A program-supplied file with the same
		// name is deliberately ignored and replaced in artifact.zip.
		if relative == manifestFilename {
			return nil
		}
		if len(manifest.Files) >= maxArtifactFiles {
			return errors.New("Sandbox output contains too many files")
		}
		total += info.Size()
		if total > limit {
			return errors.New("Sandbox output exceeds the frozen disk limit")
		}
		digest, err := fileSHA256(localPath)
		if err != nil {
			return err
		}
		manifest.Files = append(manifest.Files, contracts.ManifestFile{
			Path: relative, SHA256: digest, Size: info.Size(), Kind: artifactKind(relative),
			MIMEType: mime.TypeByExtension(filepath.Ext(relative)),
		})
		return nil
	})
	if err != nil {
		return contracts.Manifest{}, err
	}
	sort.Slice(manifest.Files, func(left, right int) bool { return manifest.Files[left].Path < manifest.Files[right].Path })
	manifest.Summary = readSummary(rootReal)
	if err := manifest.Validate(); err != nil {
		return contracts.Manifest{}, err
	}
	return manifest, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func artifactKind(relative string) string {
	if relative == "summary.md" {
		return "summary"
	}
	prefix, _, _ := strings.Cut(relative, "/")
	switch prefix {
	case "logs":
		return "log"
	case "figures":
		return "figure"
	case "tables":
		return "table"
	case "data":
		return "data"
	case "models":
		return "model"
	default:
		return "file"
	}
}

func readSummary(root string) string {
	path := filepath.Join(root, "summary.md")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxSummaryBytes {
		return ""
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.ToValidUTF8(string(content), "")
}

func CollectManifest(root string, manifest contracts.Manifest) (contracts.Manifest, error) {
	if err := manifest.Validate(); err != nil {
		return contracts.Manifest{}, err
	}
	var total int64
	for index := range manifest.Files {
		file := &manifest.Files[index]
		path, err := securePath(root, file.Path)
		if err != nil {
			return contracts.Manifest{}, err
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() != file.Size {
			return contracts.Manifest{}, fmt.Errorf("manifest file is missing or unsafe: %s", file.Path)
		}
		total += file.Size
		if total > 10<<30 {
			return contracts.Manifest{}, errors.New("manifest output exceeds the maximum size")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return contracts.Manifest{}, err
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != file.SHA256 {
			return contracts.Manifest{}, fmt.Errorf("manifest hash mismatch: %s", file.Path)
		}
	}
	return manifest, nil
}

func BuildArtifactZip(root, destination string, manifest contracts.Manifest) (contracts.ArtifactPointer, error) {
	manifest, err := CollectManifest(root, manifest)
	if err != nil {
		return contracts.ArtifactPointer{}, err
	}
	if filepath.Clean(destination) == filepath.Clean(filepath.Join(root, manifestFilename)) {
		return contracts.ArtifactPointer{}, errors.New("artifact destination must be outside output directory")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return contracts.ArtifactPointer{}, err
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return contracts.ArtifactPointer{}, err
	}
	archive := zip.NewWriter(file)
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		_ = file.Close()
		return contracts.ArtifactPointer{}, err
	}
	entry, err := archive.Create(manifestFilename)
	if err == nil {
		_, err = entry.Write(manifestBytes)
	}
	paths := make([]string, 0, len(manifest.Files))
	for _, item := range manifest.Files {
		paths = append(paths, item.Path)
	}
	sort.Strings(paths)
	for _, relative := range paths {
		if err != nil {
			break
		}
		filePath, pathErr := securePath(root, relative)
		if pathErr != nil {
			err = pathErr
			break
		}
		info, statErr := os.Lstat(filePath)
		if statErr != nil || !info.Mode().IsRegular() {
			err = fmt.Errorf("refusing non-regular artifact path: %s", relative)
			break
		}
		input, openErr := os.Open(filePath)
		if openErr != nil {
			err = openErr
			break
		}
		entry, err = archive.Create(relative)
		if err == nil {
			_, err = io.Copy(entry, input)
		}
		_ = input.Close()
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return contracts.ArtifactPointer{}, err
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		return contracts.ArtifactPointer{}, err
	}
	digest := sha256.Sum256(data)
	return contracts.ArtifactPointer{Filename: "execution-bundle.zip", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(data))}, nil
}

func safeManifestPath(relative string) bool {
	return relative != "" && !strings.ContainsRune(relative, 0) && contracts.ValidateRelativePath(relative) == nil
}

func securePath(root, relative string) (string, error) {
	if !safeManifestPath(relative) {
		return "", errors.New("unsafe artifact path")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(root, filepath.FromSlash(relative))
	parentReal, err := filepath.EvalSymlinks(filepath.Dir(candidate))
	if err != nil {
		return "", err
	}
	resolved := filepath.Join(parentReal, filepath.Base(candidate))
	if resolved != rootReal && !strings.HasPrefix(resolved, rootReal+string(filepath.Separator)) {
		return "", errors.New("artifact path escapes output directory")
	}
	return candidate, nil
}
