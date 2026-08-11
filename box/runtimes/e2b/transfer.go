package e2b

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mmdash/mmdash/box/capabilities/sandbox"
)

const (
	remoteWorkspace = "/workspace"
	remoteOutput    = "/output"
)

type workspaceEntry struct {
	Relative   string
	LocalPath  string
	Mode       fs.FileMode
	LinkTarget string
}

type workspaceSnapshot struct {
	Directories []workspaceEntry
	Files       []workspaceEntry
	Symlinks    []workspaceEntry
	Executables []string
}

func (client *ProviderClient) prepareWorkspace(ctx context.Context, session *executionSession, request sandbox.RunRequest) error {
	snapshot, err := client.scanWorkspace(request.Workspace)
	if err != nil {
		return err
	}
	if err := client.makeDir(ctx, session, remoteWorkspace, client.adminUser); err != nil {
		return err
	}
	if err := client.makeDir(ctx, session, remoteOutput, client.adminUser); err != nil {
		return err
	}
	for _, entry := range snapshot.Directories {
		if err := client.makeDir(ctx, session, remoteJoin(remoteWorkspace, entry.Relative), client.adminUser); err != nil {
			return err
		}
	}
	for _, entry := range snapshot.Files {
		if err := client.uploadFile(ctx, session, entry.LocalPath, remoteJoin(remoteWorkspace, entry.Relative)); err != nil {
			return err
		}
	}
	for _, entry := range snapshot.Symlinks {
		command := []string{"/bin/ln", "-s", "--", entry.LinkTarget, remoteJoin(remoteWorkspace, entry.Relative)}
		if err := client.runSetupProcess(ctx, session, command); err != nil {
			return err
		}
	}
	for start := 0; start < len(snapshot.Executables); start += 100 {
		end := start + 100
		if end > len(snapshot.Executables) {
			end = len(snapshot.Executables)
		}
		command := []string{"/bin/chmod", "u+x", "--"}
		for _, relative := range snapshot.Executables[start:end] {
			command = append(command, remoteJoin(remoteWorkspace, relative))
		}
		if err := client.runSetupProcess(ctx, session, command); err != nil {
			return err
		}
	}
	owner := client.user + ":" + client.user
	if err := client.runSetupProcess(ctx, session, []string{"/bin/chown", "-R", owner, remoteWorkspace, remoteOutput}); err != nil {
		return err
	}
	return nil
}

func (client *ProviderClient) runSetupProcess(ctx context.Context, session *executionSession, command []string) error {
	requestCtx, cancel := client.requestContext(ctx)
	defer cancel()
	exitCode, err := client.runProcess(requestCtx, session, client.adminUser, command, nil, io.Discard, io.Discard, nil)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("E2B setup command exited with code %d", exitCode)
	}
	return nil
}

func (client *ProviderClient) scanWorkspace(root string) (workspaceSnapshot, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return workspaceSnapshot{}, err
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return workspaceSnapshot{}, errors.New("E2B workspace is unavailable")
	}
	var snapshot workspaceSnapshot
	var count int
	var total int64
	err = filepath.WalkDir(rootReal, func(localPath string, entry fs.DirEntry, walkErr error) error {
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
		if relative == ".git" || strings.HasPrefix(relative, ".git/") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		count++
		if count > client.maxWorkspaceFiles {
			return errors.New("E2B workspace contains too many entries")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := workspaceEntry{Relative: relative, LocalPath: localPath, Mode: info.Mode()}
		switch {
		case entry.IsDir():
			snapshot.Directories = append(snapshot.Directories, item)
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(localPath)
			if err != nil {
				return err
			}
			if filepath.IsAbs(target) || strings.ContainsRune(target, 0) {
				return fmt.Errorf("workspace symlink is unsafe: %s", relative)
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(localPath), target))
			resolvedReal, err := filepath.EvalSymlinks(resolved)
			if err != nil || !withinRoot(rootReal, resolvedReal) {
				return fmt.Errorf("workspace symlink escapes checkout: %s", relative)
			}
			item.LinkTarget = filepath.ToSlash(target)
			snapshot.Symlinks = append(snapshot.Symlinks, item)
		case info.Mode().IsRegular():
			total += info.Size()
			if total > client.maxWorkspaceBytes {
				return errors.New("E2B workspace exceeds the upload size limit")
			}
			snapshot.Files = append(snapshot.Files, item)
			if info.Mode().Perm()&0o111 != 0 {
				snapshot.Executables = append(snapshot.Executables, relative)
			}
		default:
			return fmt.Errorf("workspace contains unsupported file type: %s", relative)
		}
		return nil
	})
	if err != nil {
		return workspaceSnapshot{}, err
	}
	return snapshot, nil
}

func (client *ProviderClient) collectOutput(ctx context.Context, session *executionSession, destination string, diskLimit int64) error {
	if diskLimit <= 0 {
		return errors.New("E2B output disk limit is invalid")
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	queue := []string{remoteOutput}
	seenDirectories := map[string]struct{}{remoteOutput: {}}
	seenFiles := map[string]struct{}{}
	entriesSeen := 0
	var total int64
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		entries, err := client.listDir(ctx, session, current)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(left, right int) bool { return entries[left].Path < entries[right].Path })
		for _, entry := range entries {
			entriesSeen++
			if entriesSeen > client.maxOutputFiles {
				return errors.New("E2B output contains too many entries")
			}
			relative, err := outputRelative(entry.Path)
			if err != nil {
				return err
			}
			localPath := filepath.Join(destination, filepath.FromSlash(relative))
			switch entry.Type {
			case "FILE_TYPE_DIRECTORY":
				if _, duplicate := seenDirectories[entry.Path]; duplicate {
					return fmt.Errorf("duplicate E2B output directory: %s", relative)
				}
				seenDirectories[entry.Path] = struct{}{}
				if err := os.MkdirAll(localPath, 0o700); err != nil {
					return err
				}
				queue = append(queue, entry.Path)
			case "FILE_TYPE_FILE":
				if _, duplicate := seenFiles[entry.Path]; duplicate {
					return fmt.Errorf("duplicate E2B output file: %s", relative)
				}
				seenFiles[entry.Path] = struct{}{}
				declared := int64(entry.Size)
				if declared < 0 || total+declared > diskLimit {
					return errors.New("E2B output exceeds the frozen disk limit")
				}
				if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
					return err
				}
				file, err := os.OpenFile(localPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
				if err != nil {
					return err
				}
				written, downloadErr := client.downloadFile(ctx, session, entry.Path, file, diskLimit-total)
				closeErr := file.Close()
				if downloadErr != nil {
					_ = os.Remove(localPath)
					return downloadErr
				}
				if closeErr != nil {
					return closeErr
				}
				if declared != written {
					return fmt.Errorf("E2B output size changed during download: %s", relative)
				}
				total += written
			case "FILE_TYPE_SYMLINK":
				return fmt.Errorf("refusing symlink in E2B output: %s", relative)
			default:
				return fmt.Errorf("refusing unsupported E2B output type: %s", relative)
			}
		}
	}
	return nil
}

func openRegularFile(filePath string) (*os.File, error) {
	before, err := os.Lstat(filePath)
	if err != nil || !before.Mode().IsRegular() {
		return nil, errors.New("E2B workspace file changed or became unsafe")
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		_ = file.Close()
		return nil, errors.New("E2B workspace file changed or became unsafe")
	}
	return file, nil
}

func outputRelative(remotePath string) (string, error) {
	clean := path.Clean(strings.ReplaceAll(remotePath, `\`, "/"))
	if clean == remoteOutput || !strings.HasPrefix(clean, remoteOutput+"/") {
		return "", errors.New("E2B output path escapes the output directory")
	}
	relative := strings.TrimPrefix(clean, remoteOutput+"/")
	if relative == "" || relative == "." || relative == ".." || strings.HasPrefix(relative, "../") {
		return "", errors.New("E2B output path is unsafe")
	}
	return relative, nil
}

func remoteJoin(root, relative string) string {
	return path.Join(root, strings.ReplaceAll(relative, `\`, "/"))
}

func withinRoot(root, candidate string) bool {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)
	return candidate == root || strings.HasPrefix(candidate, root+string(filepath.Separator))
}
