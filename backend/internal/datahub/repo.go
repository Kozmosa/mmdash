package datahub

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	repodomain "github.com/mmdash/mmdash/backend/internal/repo"
)

// ProjectRepository creates or refreshes the stable Repo registry object.
func (store PostgresStore) ProjectRepository(
	ctx context.Context,
	event contract.EventEnvelope,
	repository repodomain.Repository,
) error {
	if event.ProjectID == nil ||
		*event.ProjectID != repository.ProjectID {
		return ErrInvalid
	}
	objectID, err := store.Generator.New()
	if err != nil {
		return err
	}
	activityID, err := store.Generator.New()
	if err != nil {
		return err
	}
	workspaces := make([]map[string]interface{}, 0, len(repository.Workspaces))
	for _, workspace := range repository.Workspaces {
		workspaces = append(workspaces, map[string]interface{}{
			"branch":     workspace.RemoteBranch,
			"commit_sha": workspace.HeadCommitSHA,
			"status":     string(workspace.Status),
			"workspace":  string(workspace.Workspace),
		})
	}
	metadata := jsonBytes(map[string]interface{}{
		"default_branch": repository.DefaultBranch,
		"provider":       string(repository.Provider),
		"repository_id":  repository.ID,
		"workspaces":     workspaces,
	})
	actor := projectionActor(event.Actor)
	title := strings.TrimSpace(repository.DisplayName)
	if title == "" {
		title = repository.ID
	}
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		done, err := projectionDone(ctx, tx, event.EventID)
		if err != nil || done {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO data_objects (
				object_id, project_id, object_type, source_module, source_id,
				title, summary, status, metadata,
				occurred_at, created_at, updated_at
			) VALUES (
				$1, $2, 'repository', 'repo', $3,
				$4, $5, $6, $7, $8, $8, $8
			)
			ON CONFLICT (source_module, object_type, source_id) DO UPDATE
			SET title = EXCLUDED.title,
			    summary = EXCLUDED.summary,
			    status = EXCLUDED.status,
			    metadata = EXCLUDED.metadata,
			    version = data_objects.version + 1,
			    occurred_at = EXCLUDED.occurred_at,
			    updated_at = EXCLUDED.updated_at
		`, objectID, repository.ProjectID, repository.ID,
			title, repository.DefaultBranch,
			string(repository.Status), metadata, event.OccurredAt); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO data_activity (
				activity_id, project_id, object_id, event_id, activity_type,
				title, summary, actor, metadata, occurred_at, created_at
			)
			SELECT $1, $2, object_id, $3, $4, $5, $6, $7, $8, $9, $10
			FROM data_objects
			WHERE source_module = 'repo' AND object_type = 'repository'
			  AND source_id = $11
			ON CONFLICT (event_id) WHERE event_id IS NOT NULL DO NOTHING
		`, activityID, repository.ProjectID, event.EventID, event.EventType,
			title, repository.DefaultBranch, actor, metadata,
			event.OccurredAt, store.Clock.Now().UTC(), repository.ID)
		return err
	})
}

// ProjectRepoCommit projects immutable commit metadata and one activity.
func (store PostgresStore) ProjectRepoCommit(
	ctx context.Context,
	event contract.EventEnvelope,
	repository repodomain.Repository,
	workspace repodomain.Workspace,
	commit repodomain.Commit,
	recordActivity bool,
) error {
	if event.ProjectID == nil ||
		*event.ProjectID != repository.ProjectID ||
		commit.RepositoryID != repository.ID {
		return ErrInvalid
	}
	objectID, err := store.Generator.New()
	if err != nil {
		return err
	}
	activityID := ""
	if recordActivity {
		activityID, err = store.Generator.New()
		if err != nil {
			return err
		}
	}
	title := commitTitle(commit.Message, commit.CommitSHA)
	sourceID := repository.ID + ":" + commit.CommitSHA
	metadata := jsonBytes(map[string]interface{}{
		"author":        commit.Author,
		"branch":        workspace.RemoteBranch,
		"changed_paths": commit.Changes,
		"commit_sha":    commit.CommitSHA,
		"committer":     commit.Committer,
		"parent_shas":   commit.ParentSHAs,
		"repository_id": repository.ID,
		"source":        commit.Source,
		"tree_sha":      commit.TreeSHA,
		"workspace":     string(workspace.Workspace),
	})
	actor := projectionActor(event.Actor)
	summary := fmt.Sprintf(
		"%s on %s", commit.Author.Name, workspace.RemoteBranch,
	)
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if recordActivity {
			done, err := projectionDone(ctx, tx, event.EventID)
			if err != nil || done {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO data_objects (
				object_id, project_id, object_type, source_module, source_id,
				title, summary, status, metadata,
				occurred_at, created_at, updated_at
			) VALUES (
				$1, $2, 'repo_commit', 'repo', $3,
				$4, $5, 'active', $6, $7, $7, $7
			)
			ON CONFLICT (source_module, object_type, source_id) DO UPDATE
			SET title = EXCLUDED.title,
			    summary = EXCLUDED.summary,
			    status = EXCLUDED.status,
			    metadata = EXCLUDED.metadata,
			    version = data_objects.version + 1,
			    occurred_at = EXCLUDED.occurred_at,
			    updated_at = EXCLUDED.updated_at
			WHERE data_objects.title IS DISTINCT FROM EXCLUDED.title
			   OR data_objects.summary IS DISTINCT FROM EXCLUDED.summary
			   OR data_objects.status IS DISTINCT FROM EXCLUDED.status
			   OR data_objects.metadata IS DISTINCT FROM EXCLUDED.metadata
		`, objectID, repository.ProjectID, sourceID,
			title, summary, metadata, event.OccurredAt); err != nil {
			return err
		}
		if !recordActivity {
			return nil
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO data_activity (
				activity_id, project_id, object_id, event_id, activity_type,
				title, summary, actor, metadata, occurred_at, created_at
			)
			SELECT $1, $2, object_id, $3, $4, $5, $6, $7, $8, $9, $10
			FROM data_objects
			WHERE source_module = 'repo' AND object_type = 'repo_commit'
			  AND source_id = $11
			ON CONFLICT (event_id) WHERE event_id IS NOT NULL DO NOTHING
		`, activityID, repository.ProjectID, event.EventID, event.EventType,
			title, summary, actor, metadata, event.OccurredAt,
			store.Clock.Now().UTC(), sourceID)
		return err
	})
}

// UpsertRepoFiles applies one bounded current-code-head metadata batch.
func (store PostgresStore) UpsertRepoFiles(
	ctx context.Context,
	repository repodomain.Repository,
	commitSHA string,
	files []repodomain.DataHubFile,
	occurredAt time.Time,
) error {
	if repository.ID == "" || repository.ProjectID == "" ||
		commitSHA == "" || len(files) == 0 {
		return ErrInvalid
	}
	type preparedFile struct {
		file     repodomain.DataHubFile
		metadata []byte
		objectID string
	}
	prepared := make([]preparedFile, 0, len(files))
	for _, file := range files {
		objectID, err := store.Generator.New()
		if err != nil {
			return err
		}
		prepared = append(prepared, preparedFile{
			file: file, objectID: objectID,
			metadata: jsonBytes(map[string]interface{}{
				"commit_sha":    commitSHA,
				"kind":          file.Kind,
				"mode":          file.Mode,
				"object_id":     file.ObjectID,
				"path":          file.Path,
				"repository_id": repository.ID,
				"size":          file.Size,
				"workspace":     "code",
			}),
		})
	}
	now := store.Clock.Now().UTC()
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		for _, item := range prepared {
			sourceID := repository.ID + ":code:" + item.file.Path
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO data_objects (
					object_id, project_id, object_type, source_module, source_id,
					title, summary, status, metadata,
					occurred_at, created_at, updated_at
				) VALUES (
					$1, $2, 'repo_file', 'repo', $3, $4, $5,
					'active', $6, $7, $8, $8
				)
				ON CONFLICT (source_module, object_type, source_id) DO UPDATE
				SET title = EXCLUDED.title,
				    summary = EXCLUDED.summary,
				    status = EXCLUDED.status,
				    metadata = EXCLUDED.metadata,
				    version = data_objects.version + 1,
				    occurred_at = EXCLUDED.occurred_at,
				    updated_at = EXCLUDED.updated_at
				WHERE data_objects.title IS DISTINCT FROM EXCLUDED.title
				   OR data_objects.summary IS DISTINCT FROM EXCLUDED.summary
				   OR data_objects.status IS DISTINCT FROM EXCLUDED.status
				   OR data_objects.metadata IS DISTINCT FROM EXCLUDED.metadata
			`, item.objectID, repository.ProjectID, sourceID,
				item.file.Path, item.file.Kind+" "+item.file.Mode,
				item.metadata, occurredAt.UTC(), now); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteStaleRepoFiles removes paths not present at the indexed code head.
func (store PostgresStore) DeleteStaleRepoFiles(
	ctx context.Context,
	repositoryID string,
	commitSHA string,
) error {
	if repositoryID == "" || commitSHA == "" {
		return ErrInvalid
	}
	_, err := store.DB.ExecContext(ctx, `
		DELETE FROM data_objects
		WHERE source_module = 'repo'
		  AND object_type = 'repo_file'
		  AND metadata->>'repository_id' = $1
		  AND metadata->>'commit_sha' IS DISTINCT FROM $2
	`, repositoryID, commitSHA)
	return err
}

func projectionDone(
	ctx context.Context,
	tx transaction.Tx,
	eventID string,
) (bool, error) {
	var done bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM data_activity WHERE event_id = $1
		)
	`, eventID).Scan(&done)
	return done, err
}

func projectionActor(actor map[string]string) []byte {
	if actor == nil {
		actor = map[string]string{}
	}
	return jsonBytes(actor)
}

func commitTitle(message string, commitSHA string) string {
	title := strings.TrimSpace(strings.SplitN(message, "\n", 2)[0])
	if title == "" {
		title = commitSHA
	}
	for len(title) > 200 {
		_, size := utf8.DecodeLastRuneInString(title)
		title = title[:len(title)-size]
	}
	return title
}

var _ repodomain.DataHubSink = PostgresStore{}
