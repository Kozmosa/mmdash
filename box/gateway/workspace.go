package gateway

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mmdash/mmdash/box/contracts"
)

const (
	defaultMaximumSourceBytes = int64(10 << 30)
	maximumSourceFiles        = 100_000
)

// TransferWorkspace downloads the immutable Repo-owned source archive through
// the short-lived URL frozen into the RunSpec. It does not invoke Git and does
// not receive a long-lived repository credential.
type TransferWorkspace struct {
	Root               string
	Client             *http.Client
	MaximumSourceBytes int64
}

func (provider TransferWorkspace) Prepare(ctx context.Context, spec contracts.RunSpec) (string, func(), error) {
	if err := spec.Validate(); err != nil {
		return "", nil, err
	}
	if provider.Root == "" || !filepath.IsAbs(provider.Root) {
		return "", nil, errors.New("an absolute source workspace root is required")
	}
	limit := provider.MaximumSourceBytes
	if limit <= 0 || limit > defaultMaximumSourceBytes {
		limit = defaultMaximumSourceBytes
	}
	if err := os.MkdirAll(provider.Root, 0o700); err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256([]byte(spec.ExperimentID + "\x00" + spec.ExecutionEpoch))
	target := filepath.Join(provider.Root, hex.EncodeToString(digest[:16]))
	if marker, err := os.ReadFile(filepath.Join(target, ".mmdash-commit")); err == nil {
		if strings.TrimSpace(string(marker)) != spec.SourceCommit {
			return "", nil, errors.New("cached source workspace has a different commit")
		}
		return target, func() { _ = os.RemoveAll(target) }, nil
	}
	if !time.Now().UTC().Before(spec.SourceTransfer.ExpiresAt) {
		return "", nil, errors.New("source transfer URL has expired")
	}
	temporary, err := os.MkdirTemp(provider.Root, ".source-")
	if err != nil {
		return "", nil, err
	}
	cleanupTemporary := func() { _ = os.RemoveAll(temporary) }
	archivePath := filepath.Join(temporary, "source.zip")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.SourceTransfer.URL, nil)
	if err != nil {
		cleanupTemporary()
		return "", nil, err
	}
	request.Header.Set("Accept", "application/zip")
	client := provider.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	response, err := client.Do(request)
	if err != nil {
		cleanupTemporary()
		return "", nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		cleanupTemporary()
		return "", nil, fmt.Errorf("source transfer returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		cleanupTemporary()
		return "", nil, errors.New("source archive exceeds the Box limit")
	}
	archive, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		cleanupTemporary()
		return "", nil, err
	}
	written, copyErr := io.Copy(archive, io.LimitReader(response.Body, limit+1))
	closeErr := archive.Close()
	if copyErr != nil || closeErr != nil || written > limit {
		cleanupTemporary()
		return "", nil, errors.Join(copyErr, closeErr, errors.New("source archive exceeds the Box limit"))
	}
	content := filepath.Join(temporary, "content")
	if err := extractSourceZip(archivePath, content, limit); err != nil {
		cleanupTemporary()
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(content, ".mmdash-commit"), []byte(spec.SourceCommit+"\n"), 0o600); err != nil {
		cleanupTemporary()
		return "", nil, err
	}
	if err := os.Rename(content, target); err != nil {
		cleanupTemporary()
		return "", nil, err
	}
	cleanupTemporary()
	return target, func() { _ = os.RemoveAll(target) }, nil
}

func extractSourceZip(archivePath, destination string, limit int64) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return errors.New("source transfer is not a valid zip archive")
	}
	defer reader.Close()
	if len(reader.File) > maximumSourceFiles {
		return errors.New("source archive contains too many files")
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	var total int64
	for _, item := range reader.File {
		name := strings.ReplaceAll(item.Name, `\`, "/")
		if strings.HasSuffix(name, "/") {
			name = strings.TrimSuffix(name, "/")
			if name == "" {
				continue
			}
		}
		if err := contracts.ValidateRelativePath(name); err != nil || item.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe source archive path %q", item.Name)
		}
		total += int64(item.UncompressedSize64)
		if total > limit {
			return errors.New("expanded source archive exceeds the Box limit")
		}
		path := filepath.Join(destination, filepath.FromSlash(name))
		if item.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return err
			}
			continue
		}
		if !item.Mode().IsRegular() {
			return fmt.Errorf("unsupported source archive entry %q", item.Name)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		input, err := item.Open()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = io.Copy(output, io.LimitReader(input, int64(item.UncompressedSize64)+1))
		}
		closeOutput := error(nil)
		if output != nil {
			closeOutput = output.Close()
		}
		closeInput := input.Close()
		if err = errors.Join(err, closeOutput, closeInput); err != nil {
			return err
		}
	}
	return nil
}
