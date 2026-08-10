package sandbox

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mmdash/mmdash/box/contracts"
)

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
	if filepath.Clean(destination) == filepath.Clean(filepath.Join(root, "manifest.json")) {
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
	entry, err := archive.Create("manifest.json")
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
	return contracts.ArtifactPointer{Filename: "artifact.zip", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(data))}, nil
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
