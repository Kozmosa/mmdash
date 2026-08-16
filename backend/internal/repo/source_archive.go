package repo

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
)

const (
	defaultSourceArchiveBytes = int64(10 << 30)
	defaultSourceArchiveFiles = 100_000
)

// SourceArchiveService is the only Box-facing source export boundary. It asks
// Repo to materialize one immutable commit and streams a zip without exposing
// a worktree path or any provider credential.
type SourceArchiveService struct {
	Generator    interface{ New() (string, error) }
	MaxBytes     int64
	MaxFiles     int
	Repositories interface {
		GetByProject(context.Context, string) (Repository, error)
	}
	Runtime Runtime
	Storage *gitcli.Storage
}

func (service SourceArchiveService) WriteSourceArchive(ctx context.Context, projectID, commitSHA string, output io.Writer) error {
	if service.Generator == nil || service.Repositories == nil || service.Runtime.Git == nil ||
		service.Storage == nil || output == nil || gitcli.ValidateFullSHA(commitSHA) != nil {
		return ErrInvalid
	}
	repository, err := service.Repositories.GetByProject(ctx, projectID)
	if err != nil {
		return err
	}
	checkoutID, err := service.Generator.New()
	if err != nil {
		return err
	}
	relative, err := service.Runtime.CreateCheckout(ctx, repository, checkoutID, commitSHA)
	if err != nil {
		return err
	}
	defer service.Runtime.ReleaseCheckout(context.Background(), repository, relative)
	root, err := service.Storage.ManagedPath(repository.StorageKey, relative)
	if err != nil {
		return err
	}
	maxBytes := service.MaxBytes
	if maxBytes <= 0 || maxBytes > defaultSourceArchiveBytes {
		maxBytes = defaultSourceArchiveBytes
	}
	maxFiles := service.MaxFiles
	if maxFiles <= 0 || maxFiles > defaultSourceArchiveFiles {
		maxFiles = defaultSourceArchiveFiles
	}
	archive := zip.NewWriter(output)
	if err := writeSourceTree(archive, root, maxBytes, maxFiles); err != nil {
		_ = archive.Close()
		return err
	}
	return archive.Close()
}

func writeSourceTree(archive *zip.Writer, root string, maxBytes int64, maxFiles int) error {
	var total int64
	files := 0
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == "." {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == ".git" || strings.HasPrefix(relative, ".git/") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in Repo source archive: %s", relative)
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing unsupported Repo source entry: %s", relative)
		}
		files++
		total += info.Size()
		if files > maxFiles || total > maxBytes {
			return errors.New("Repo source archive exceeds the configured limit")
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = relative
		header.Method = zip.Deflate
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, input)
		closeErr := input.Close()
		return errors.Join(copyErr, closeErr)
	})
}
