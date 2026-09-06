package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
)

const (
	// A Bundle may contain 10,000 result files. The committed representation
	// also contains manifest.json and .mmdash/artifacts.json.
	maxResultFiles = 10002
	maxResultBytes = int64(10 << 30)
)

var contentSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ResultFileChange is one trusted, already-validated file staged by the
// Experiment finalizer. SourcePath never crosses the public Repo API.
type ResultFileChange struct {
	Path       string
	SHA256     string
	SizeBytes  int64
	SourcePath string
}

// ResultCommitRequest writes one complete immutable Experiment directory.
type ResultCommitRequest struct {
	ActorID         string
	ExpectedHeadSHA string
	ExperimentID    string
	Files           []ResultFileChange
	ProjectID       string
	ResultDirectory string
	SourceRoot      string
}

type ResultRevertRequest struct {
	ActorID         string
	ExperimentID    string
	Paths           []string
	ProjectID       string
	ResultDirectory string
}

// ResultWorkspaceService is the narrow trusted Repo capability used by
// Experiment. Repo remains the only component that locks, commits and pushes.
type ResultWorkspaceService struct {
	Coordinator  *Coordinator
	Reader       *Reader
	Repositories Store
	Service      *Service
}

type ResultTreeFile struct {
	ObjectID string
	Path     string
	Size     int64
}

func (workspace ResultWorkspaceService) SyncNow(
	ctx context.Context,
	projectID string,
) error {
	if workspace.Coordinator == nil {
		return ErrNotReady
	}
	_, err := workspace.Coordinator.SyncProjectWorkspace(
		ctx, projectID, WorkspaceResult,
	)
	return err
}

func (workspace ResultWorkspaceService) VerifyCommitReachable(
	ctx context.Context,
	projectID, commitSHA string,
) error {
	if workspace.Reader == nil || gitcli.ValidateFullSHA(commitSHA) != nil {
		return ErrInvalid
	}
	repository, err := workspace.Repositories.GetByProject(ctx, projectID)
	if err != nil {
		return err
	}
	result, err := findWorkspace(repository, WorkspaceResult)
	if err != nil || result.HeadCommitSHA == nil {
		return ErrNotReady
	}
	layout, err := workspace.Reader.readyLayout(repository)
	if err != nil {
		return err
	}
	_, err = workspace.Reader.Git.Run(ctx, gitcli.Command{
		Args: []string{
			"--git-dir=" + layout.Bare, "merge-base", "--is-ancestor",
			commitSHA, *result.HeadCommitSHA,
		},
		Directory: layout.Repository, Operation: "repo.result.commit.reachable",
	})
	if err == nil {
		return nil
	}
	var commandError *gitcli.CommandError
	if errors.As(err, &commandError) && commandError.ExitCode == 1 {
		return ErrObjectNotFound
	}
	return err
}

func (workspace ResultWorkspaceService) ListResultFiles(
	ctx context.Context,
	projectID, commitSHA, resultDirectory string,
) ([]ResultTreeFile, error) {
	if workspace.Reader == nil || workspace.Repositories == nil ||
		gitcli.ValidateFullSHA(commitSHA) != nil ||
		gitcli.ValidateRepoPath(strings.TrimSuffix(resultDirectory, "/"), false) != nil ||
		!strings.HasSuffix(resultDirectory, "/") {
		return nil, ErrInvalid
	}
	repository, err := workspace.Repositories.GetByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	queue := []string{strings.TrimSuffix(resultDirectory, "/")}
	files := make([]ResultTreeFile, 0)
	for len(queue) > 0 {
		directory := queue[0]
		queue = queue[1:]
		cursor := ""
		for {
			page, err := workspace.Reader.ListTree(
				ctx, repository, WorkspaceResult, commitSHA, directory, cursor, maxTreeLimit,
			)
			if err != nil {
				return nil, err
			}
			for _, entry := range page.Items {
				switch entry.Kind {
				case "directory":
					queue = append(queue, entry.Path)
				case "file":
					if entry.Size == nil || *entry.Size < 0 || len(files) >= maxResultFiles {
						return nil, ErrInvalid
					}
					files = append(files, ResultTreeFile{
						ObjectID: entry.ObjectID, Path: entry.Path, Size: *entry.Size,
					})
				default:
					return nil, ErrInvalid
				}
			}
			if !page.HasMore || page.NextCursor == nil {
				break
			}
			cursor = *page.NextCursor
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func (workspace ResultWorkspaceService) ReadResultFile(
	ctx context.Context,
	projectID, commitSHA, repositoryPath string,
) (FileContent, error) {
	if workspace.Reader == nil {
		return FileContent{}, ErrNotReady
	}
	repository, err := workspace.Repositories.GetByProject(ctx, projectID)
	if err != nil {
		return FileContent{}, err
	}
	return workspace.Reader.ReadFile(
		ctx, repository, WorkspaceResult, commitSHA, repositoryPath,
	)
}

func (workspace ResultWorkspaceService) HashResultFile(
	ctx context.Context,
	projectID string,
	file ResultTreeFile,
) (string, error) {
	if workspace.Reader == nil {
		return "", ErrNotReady
	}
	repository, err := workspace.Repositories.GetByProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	return workspace.Reader.HashBlob(ctx, repository, file.ObjectID, file.Size)
}

func (workspace ResultWorkspaceService) ResolveHead(
	ctx context.Context,
	projectID string,
) (Revision, error) {
	repository, err := workspace.Repositories.GetByProject(ctx, projectID)
	if err != nil {
		return Revision{}, err
	}
	result, err := findWorkspace(repository, WorkspaceResult)
	if err != nil {
		return Revision{}, err
	}
	if result.HeadCommitSHA == nil {
		return Revision{}, ErrNotReady
	}
	return Revision{
		Branch: result.RemoteBranch, CommitSHA: *result.HeadCommitSHA,
		RepositoryID: repository.ID, Workspace: WorkspaceResult,
	}, nil
}

func (workspace ResultWorkspaceService) Commit(
	ctx context.Context,
	input ResultCommitRequest,
) (CommitResult, error) {
	if workspace.Service == nil || workspace.Repositories == nil {
		return CommitResult{}, ErrNotReady
	}
	changes, requestHash, err := validateResultCommit(input)
	if err != nil {
		return CommitResult{}, err
	}
	request := WorkspaceCommitRequest{
		ActorEmail: "experiment@mmdash.local", ActorID: input.ActorID,
		ActorName: "mmdash Experiment", Changes: changes,
		ExpectedHeadSHA: input.ExpectedHeadSHA,
		IdempotencyKey:  "experiment-result:" + input.ExperimentID,
		Message:         "experiment(" + input.ExperimentID + "): archive result",
		ProjectID:       input.ProjectID, RequestSHA256: requestHash,
		StageAll: true, Workspace: WorkspaceResult,
	}
	return workspace.Service.commitTrusted(ctx, request)
}

// Revert appends a compensating deletion commit for a result directory. It
// never rewrites remote history and is used only when Experiment invalidates a
// result after its normal push won the race.
func (workspace ResultWorkspaceService) Revert(
	ctx context.Context,
	input ResultRevertRequest,
) (CommitResult, error) {
	if workspace.Service == nil || input.ProjectID == "" || input.ActorID == "" ||
		input.ExperimentID == "" || len(input.Paths) < 1 || len(input.Paths) > maxResultFiles {
		return CommitResult{}, ErrInvalid
	}
	revision, err := workspace.ResolveHead(ctx, input.ProjectID)
	if err != nil {
		return CommitResult{}, err
	}
	seen := map[string]bool{}
	changes := make([]FileChange, 0, len(input.Paths))
	for _, repositoryPath := range input.Paths {
		if !strings.HasPrefix(repositoryPath, input.ResultDirectory) ||
			gitcli.ValidateRepoPath(repositoryPath, false) != nil || seen[repositoryPath] {
			return CommitResult{}, ErrInvalid
		}
		seen[repositoryPath] = true
		changes = append(changes, FileChange{Operation: "delete", Path: repositoryPath})
	}
	encoded, _ := json.Marshal(struct {
		ExperimentID string   `json:"experiment_id"`
		Paths        []string `json:"paths"`
	}{input.ExperimentID, input.Paths})
	digest := sha256.Sum256(encoded)
	return workspace.Service.commitTrusted(ctx, WorkspaceCommitRequest{
		ActorEmail: "experiment@mmdash.local", ActorID: input.ActorID,
		ActorName: "mmdash Experiment", Changes: changes,
		ExpectedHeadSHA: revision.CommitSHA,
		IdempotencyKey:  "experiment-result-revert:" + input.ExperimentID,
		Message:         "revert experiment(" + input.ExperimentID + ") result",
		ProjectID:       input.ProjectID, RequestSHA256: hex.EncodeToString(digest[:]),
		StageAll: true, Workspace: WorkspaceResult,
	})
}

func validateResultCommit(input ResultCommitRequest) ([]FileChange, string, error) {
	if input.ProjectID == "" || input.ExperimentID == "" || input.ActorID == "" ||
		gitcli.ValidateFullSHA(input.ExpectedHeadSHA) != nil ||
		gitcli.ValidateRepoPath(strings.TrimSuffix(input.ResultDirectory, "/"), false) != nil ||
		!strings.HasSuffix(input.ResultDirectory, "/") ||
		len(input.Files) < 1 || len(input.Files) > maxResultFiles {
		return nil, "", ErrInvalid
	}
	root, err := filepath.Abs(input.SourceRoot)
	if err != nil || root == "" {
		return nil, "", ErrInvalid
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, "", ErrInvalid
	}
	seen := map[string]bool{}
	changes := make([]FileChange, 0, len(input.Files))
	stable := make([]struct {
		Path      string `json:"path"`
		SHA256    string `json:"sha256"`
		SizeBytes int64  `json:"size_bytes"`
	}, 0, len(input.Files))
	var total int64
	for _, file := range input.Files {
		if !strings.HasPrefix(file.Path, input.ResultDirectory) ||
			gitcli.ValidateRepoPath(file.Path, false) != nil || seen[file.Path] ||
			!contentSHA256Pattern.MatchString(file.SHA256) || file.SizeBytes < 0 {
			return nil, "", ErrInvalid
		}
		seen[file.Path] = true
		source, pathErr := filepath.Abs(file.SourcePath)
		if pathErr != nil {
			return nil, "", ErrInvalid
		}
		relative, pathErr := filepath.Rel(root, source)
		if pathErr != nil || relative == "." || relative == ".." ||
			strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return nil, "", ErrInvalid
		}
		info, statErr := os.Lstat(source)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
			info.Size() != file.SizeBytes {
			return nil, "", ErrInvalid
		}
		if file.SizeBytes > maxResultBytes-total {
			return nil, "", ErrInvalid
		}
		total += file.SizeBytes
		changes = append(changes, FileChange{
			ContentSHA256: file.SHA256, Operation: "put", Path: file.Path,
			SizeBytes: file.SizeBytes, SourcePath: source,
		})
		stable = append(stable, struct {
			Path      string `json:"path"`
			SHA256    string `json:"sha256"`
			SizeBytes int64  `json:"size_bytes"`
		}{Path: file.Path, SHA256: file.SHA256, SizeBytes: file.SizeBytes})
	}
	hashInput := struct {
		ActorID         string      `json:"actor_id"`
		ExpectedHeadSHA string      `json:"expected_head_sha"`
		ExperimentID    string      `json:"experiment_id"`
		Files           interface{} `json:"files"`
		ProjectID       string      `json:"project_id"`
		ResultDirectory string      `json:"result_directory"`
	}{input.ActorID, input.ExpectedHeadSHA, input.ExperimentID, stable, input.ProjectID, input.ResultDirectory}
	encoded, err := json.Marshal(hashInput)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(encoded)
	return changes, hex.EncodeToString(sum[:]), nil
}
