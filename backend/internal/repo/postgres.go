package repo

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgconn"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
)

// PostgresStore persists repository configuration, status, and leases.
type PostgresStore struct {
	Clock       clock.Clock
	DB          *sql.DB
	Generator   identity.Generator
	Outbox      outbox.Writer
	Transaction transaction.Manager
}

func (store PostgresStore) CreatePending(
	ctx context.Context,
	actorID string,
	snapshot ConnectionSnapshot,
) (Repository, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return Repository{}, err
	}
	repositoryID, err := store.Generator.New()
	if err != nil {
		return Repository{}, err
	}
	storageKey, err := store.Generator.New()
	if err != nil {
		return Repository{}, err
	}
	webhookID, err := store.Generator.New()
	if err != nil {
		return Repository{}, err
	}
	now := store.Clock.Now().UTC()
	repository := Repository{
		CanonicalRemoteURL: snapshot.CanonicalRemoteURL,
		CreatedAt:          now,
		CreatedBy:          actorID,
		DefaultBranch:      snapshot.DefaultBranch,
		DisplayName:        snapshot.DisplayName,
		ID:                 repositoryID,
		NextSyncAt:         &now,
		ProjectID:          "",
		Provider:           snapshot.Provider,
		SettingsVersion:    snapshot.SettingsVersion,
		Status:             StatusPending,
		StorageKey:         storageKey,
		SyncRequestedAt:    &now,
		UpdatedAt:          now,
		Webhook:            Webhook{HookID: webhookID},
		Workspaces:         mappingList(snapshot.Workspaces, now),
	}
	// The project ID is supplied through the tested setting scope.
	// It is copied onto the snapshot immediately before persistence.
	projectID := snapshot.ProjectID
	if projectID == "" {
		return Repository{}, ErrInvalid
	}
	repository.ProjectID = projectID
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO repo_repositories (
				repository_id, project_id, provider, canonical_remote_url,
				display_name, storage_key, default_branch, status,
				settings_version, webhook_id, sync_requested_at, next_sync_at,
				created_by, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, 'pending',
				$8, $9, $10, $10, $11, $10, $10
			)
		`,
			repository.ID, repository.ProjectID, repository.Provider,
			repository.CanonicalRemoteURL, repository.DisplayName,
			repository.StorageKey, repository.DefaultBranch,
			repository.SettingsVersion, repository.Webhook.HookID,
			now, actorID,
		); err != nil {
			return err
		}
		for _, workspace := range repository.Workspaces {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO repo_workspaces (
					repository_id, workspace_kind, remote_branch, local_branch,
					status, worktree_relpath, updated_at
				) VALUES ($1, $2, $3, $4, 'pending', $5, $6)
			`, repository.ID, workspace.Workspace, workspace.RemoteBranch,
				workspace.LocalBranch, workspace.WorktreeRelpath, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if uniqueViolation(err) {
			return Repository{}, ErrAlreadyConnected
		}
		return Repository{}, wrap("create pending repository", err)
	}
	return store.GetByProject(ctx, repository.ProjectID)
}

func (store PostgresStore) GetByProject(ctx context.Context, projectID string) (Repository, error) {
	return store.get(ctx, `
		SELECT repository_id, project_id, provider, canonical_remote_url,
		       display_name, storage_key, default_branch, status,
		       settings_version, webhook_id, connected_at, last_synced_at,
		       last_error_code, last_error_message, sync_requested_at,
		       sync_started_at, sync_locked_by, sync_lease_expires_at,
		       sync_attempts, next_sync_at, cleanup_after, created_by,
		       created_at, updated_at
		FROM repo_repositories
		WHERE project_id = $1
	`, projectID)
}

func (store PostgresStore) GetByHook(ctx context.Context, hookID string) (Repository, error) {
	return store.get(ctx, `
		SELECT repository_id, project_id, provider, canonical_remote_url,
		       display_name, storage_key, default_branch, status,
		       settings_version, webhook_id, connected_at, last_synced_at,
		       last_error_code, last_error_message, sync_requested_at,
		       sync_started_at, sync_locked_by, sync_lease_expires_at,
		       sync_attempts, next_sync_at, cleanup_after, created_by,
		       created_at, updated_at
		FROM repo_repositories
		WHERE webhook_id = $1
	`, hookID)
}

func (store PostgresStore) GetByID(ctx context.Context, repositoryID string) (Repository, error) {
	return store.get(ctx, `
		SELECT repository_id, project_id, provider, canonical_remote_url,
		       display_name, storage_key, default_branch, status,
		       settings_version, webhook_id, connected_at, last_synced_at,
		       last_error_code, last_error_message, sync_requested_at,
		       sync_started_at, sync_locked_by, sync_lease_expires_at,
		       sync_attempts, next_sync_at, cleanup_after, created_by,
		       created_at, updated_at
		FROM repo_repositories
		WHERE repository_id = $1
	`, repositoryID)
}

func (store PostgresStore) ListRepositories(ctx context.Context) ([]Repository, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT project_id
		FROM repo_repositories
		WHERE status <> 'disconnected'
		ORDER BY repository_id
	`)
	if err != nil {
		return nil, err
	}
	projectIDs := []string{}
	for rows.Next() {
		var projectID string
		if err := rows.Scan(&projectID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		projectIDs = append(projectIDs, projectID)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	repositories := make([]Repository, 0, len(projectIDs))
	for _, projectID := range projectIDs {
		repository, err := store.GetByProject(ctx, projectID)
		if err != nil {
			return nil, err
		}
		repositories = append(repositories, repository)
	}
	return repositories, nil
}

func (store PostgresStore) get(ctx context.Context, query, value string) (Repository, error) {
	repository, err := scanRepository(store.DB.QueryRowContext(ctx, query, value).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, ErrNotConfigured
	}
	if err != nil {
		return Repository{}, err
	}
	workspaces, err := store.listWorkspaces(ctx, repository.ID)
	if err != nil {
		return Repository{}, err
	}
	repository.Workspaces = workspaces
	if repository.Provider == ProviderGitHub {
		remote := repository.CanonicalRemoteURL
		repository.RemoteURL = &remote
	}
	return repository, nil
}

func (store PostgresStore) listWorkspaces(
	ctx context.Context,
	repositoryID string,
) ([]Workspace, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT workspace_kind, remote_branch, local_branch, head_commit_sha,
		       tree_sha, status, worktree_relpath, updated_at
		FROM repo_workspaces
		WHERE repository_id = $1
		ORDER BY CASE workspace_kind
			WHEN 'code' THEN 0 WHEN 'article' THEN 1 ELSE 2 END
	`, repositoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	workspaces := []Workspace{}
	for rows.Next() {
		var workspace Workspace
		if err := rows.Scan(
			&workspace.Workspace,
			&workspace.RemoteBranch,
			&workspace.LocalBranch,
			&workspace.HeadCommitSHA,
			&workspace.TreeSHA,
			&workspace.Status,
			&workspace.WorktreeRelpath,
			&workspace.UpdatedAt,
		); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, workspace)
	}
	return workspaces, rows.Err()
}

func (store PostgresStore) RequestSync(
	ctx context.Context,
	projectID string,
	now time.Time,
) (Repository, error) {
	return store.RequestSyncSource(ctx, projectID, now, "manual")
}

func (store PostgresStore) RequestSyncSource(
	ctx context.Context,
	projectID string,
	now time.Time,
	source string,
) (Repository, error) {
	if !validSyncSource(source) {
		return Repository{}, ErrInvalid
	}
	result, err := store.DB.ExecContext(ctx, `
		UPDATE repo_repositories
		SET sync_requested_at = $2,
		    sync_source = $3,
		    next_sync_at = LEAST(COALESCE(next_sync_at, $2), $2),
		    updated_at = $2
		WHERE project_id = $1 AND status <> 'disconnected'
	`, projectID, now.UTC(), source)
	if err := requireAffected(result, err); err != nil {
		return Repository{}, err
	}
	return store.GetByProject(ctx, projectID)
}

func (store PostgresStore) UpdateMappings(
	ctx context.Context,
	projectID string,
	mappings WorkspaceMappings,
	settingsVersion int64,
	now time.Time,
) (Repository, error) {
	if err := validateMappings(mappings); err != nil {
		return Repository{}, err
	}
	if settingsVersion < 1 {
		return Repository{}, ErrInvalid
	}
	now = now.UTC()
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var lockedBy sql.NullString
		var repositoryID string
		if err := tx.QueryRowContext(ctx, `
			SELECT repository_id, sync_locked_by FROM repo_repositories
			WHERE project_id = $1 AND status <> 'disconnected'
			FOR UPDATE
		`, projectID).Scan(&repositoryID, &lockedBy); err != nil {
			return err
		}
		if lockedBy.Valid {
			return ErrLocked
		}
		for _, workspace := range mappingList(mappings, now) {
			if _, err := tx.ExecContext(ctx, `
				UPDATE repo_workspaces
				SET remote_branch = $3, head_commit_sha = NULL, tree_sha = NULL,
				    status = 'pending', updated_at = $4
				WHERE repository_id = $1 AND workspace_kind = $2
			`, repositoryID, workspace.Workspace, workspace.RemoteBranch, now); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE repo_repositories
			SET status = 'pending', settings_version = $3,
			    sync_requested_at = $2, next_sync_at = $2,
			    sync_source = 'manual',
			    last_error_code = NULL, last_error_message = NULL, updated_at = $2
			WHERE repository_id = $1
		`, repositoryID, now, settingsVersion)
		return err
	})
	if err != nil {
		if uniqueViolation(err) {
			return Repository{}, ErrBranchMapping
		}
		if errors.Is(err, sql.ErrNoRows) {
			return Repository{}, ErrNotConfigured
		}
		return Repository{}, wrap("update repository mappings", err)
	}
	return store.GetByProject(ctx, projectID)
}

func (store PostgresStore) Disconnect(
	ctx context.Context,
	projectID string,
	cleanupAfter time.Time,
	now time.Time,
) error {
	result, err := store.DB.ExecContext(ctx, `
		UPDATE repo_repositories
		SET status = 'disconnected', cleanup_after = $2,
		    sync_requested_at = NULL, sync_started_at = NULL,
		    sync_locked_by = NULL, sync_lease_expires_at = NULL,
		    next_sync_at = NULL, updated_at = $3
		WHERE project_id = $1 AND status <> 'disconnected'
	`, projectID, cleanupAfter.UTC(), now.UTC())
	return requireAffected(result, err)
}

// ClaimCleanup leases disconnected repositories whose grace period elapsed.
func (store PostgresStore) ClaimCleanup(
	ctx context.Context,
	now time.Time,
	lease time.Duration,
	limit int,
) ([]Repository, error) {
	rows, err := store.DB.QueryContext(ctx, `
		WITH candidates AS (
			SELECT repository_id
			FROM repo_repositories
			WHERE status = 'disconnected'
			  AND cleanup_after IS NOT NULL
			  AND cleanup_after <= $1
			ORDER BY cleanup_after, repository_id
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		UPDATE repo_repositories AS repository
		SET cleanup_after = $3, updated_at = $1
		FROM candidates
		WHERE repository.repository_id = candidates.repository_id
		RETURNING repository.repository_id
	`, now.UTC(), limit, now.UTC().Add(lease))
	if err != nil {
		return nil, wrap("claim repository cleanup", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var repositoryID string
		if err := rows.Scan(&repositoryID); err != nil {
			return nil, wrap("scan repository cleanup claim", err)
		}
		ids = append(ids, repositoryID)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate repository cleanup claims", err)
	}
	repositories := make([]Repository, 0, len(ids))
	for _, repositoryID := range ids {
		repository, err := store.GetByID(ctx, repositoryID)
		if err != nil {
			return nil, err
		}
		repositories = append(repositories, repository)
	}
	return repositories, nil
}

// CompleteCleanup deletes metadata only after managed storage is absent.
func (store PostgresStore) CompleteCleanup(
	ctx context.Context,
	repositoryID string,
) error {
	result, err := store.DB.ExecContext(ctx, `
		DELETE FROM repo_repositories
		WHERE repository_id = $1 AND status = 'disconnected'
	`, repositoryID)
	return requireAffected(result, err)
}

// RetryCleanup reschedules a failed managed-storage cleanup.
func (store PostgresStore) RetryCleanup(
	ctx context.Context,
	repositoryID string,
	retryAt time.Time,
	now time.Time,
) error {
	result, err := store.DB.ExecContext(ctx, `
		UPDATE repo_repositories
		SET cleanup_after = $2, updated_at = $3
		WHERE repository_id = $1 AND status = 'disconnected'
	`, repositoryID, retryAt.UTC(), now.UTC())
	return requireAffected(result, err)
}

// RepoGaugeSnapshot returns only aggregate queue and checkout counts.
func (store PostgresStore) RepoGaugeSnapshot(
	ctx context.Context,
	now time.Time,
) (RepoGaugeSnapshot, error) {
	var snapshot RepoGaugeSnapshot
	err := store.DB.QueryRowContext(ctx, `
		SELECT
			(
				SELECT count(*)
				FROM repo_repositories
				WHERE status <> 'disconnected'
				  AND sync_requested_at IS NOT NULL
				  AND next_sync_at <= $1
			),
			(
				SELECT count(*)
				FROM repo_checkouts
				WHERE status = 'active'
			)
	`, now.UTC()).Scan(
		&snapshot.SyncQueueDepth,
		&snapshot.CheckoutsActive,
	)
	if err != nil {
		return RepoGaugeSnapshot{}, wrap("read repository metrics", err)
	}
	return snapshot, nil
}

func (store PostgresStore) ClaimSync(
	ctx context.Context,
	owner string,
	now time.Time,
	lease time.Duration,
	limit int,
) ([]SyncClaim, error) {
	if owner == "" || lease <= 0 || limit < 1 {
		return nil, ErrInvalid
	}
	now = now.UTC()
	expiresAt := now.Add(lease)
	type claimed struct {
		projectID string
		requested time.Time
		source    string
	}
	claimedRows := []claimed{}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			WITH candidates AS (
				SELECT repository_id, sync_requested_at, sync_source
				FROM repo_repositories
				WHERE sync_requested_at IS NOT NULL
				  AND status <> 'disconnected'
				  AND (next_sync_at IS NULL OR next_sync_at <= $1)
				  AND (
				    sync_lease_expires_at IS NULL
				    OR sync_lease_expires_at < $1
				  )
				ORDER BY sync_requested_at, repository_id
				FOR UPDATE SKIP LOCKED
				LIMIT $4
			)
			UPDATE repo_repositories AS repository
			SET status = CASE
			      WHEN repository.status IN ('pending', 'cloning', 'configuring')
			        THEN 'cloning'
			      ELSE 'syncing'
			    END,
			    sync_started_at = $1,
			    sync_locked_by = $2,
			    sync_lease_expires_at = $3,
			    updated_at = $1
			FROM candidates
			WHERE repository.repository_id = candidates.repository_id
			RETURNING repository.project_id, candidates.sync_requested_at,
			          candidates.sync_source
		`, now, owner, expiresAt, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item claimed
			if err := rows.Scan(&item.projectID, &item.requested, &item.source); err != nil {
				return err
			}
			claimedRows = append(claimedRows, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, wrap("claim repository sync", err)
	}
	claims := make([]SyncClaim, 0, len(claimedRows))
	for _, item := range claimedRows {
		repository, err := store.GetByProject(ctx, item.projectID)
		if err != nil {
			return nil, err
		}
		claims = append(claims, SyncClaim{
			Repository: repository, Requested: item.requested, Source: item.source,
		})
	}
	return claims, nil
}

func (store PostgresStore) RenewSyncLease(
	ctx context.Context,
	repositoryID string,
	owner string,
	expiresAt time.Time,
) error {
	result, err := store.DB.ExecContext(ctx, `
		UPDATE repo_repositories
		SET sync_lease_expires_at = $3, updated_at = CURRENT_TIMESTAMP
		WHERE repository_id = $1 AND sync_locked_by = $2
		  AND status IN ('cloning', 'syncing')
	`, repositoryID, owner, expiresAt.UTC())
	if err := requireAffected(result, err); err != nil {
		if errors.Is(err, ErrNotConfigured) {
			return ErrLocked
		}
		return err
	}
	return nil
}

// CompleteSync atomically persists fetched Git state and its durable events.
func (store PostgresStore) CompleteSync(
	ctx context.Context,
	owner string,
	claim SyncClaim,
	syncResult SyncResult,
	now time.Time,
) error {
	if owner == "" || claim.Repository.ID == "" ||
		!validSyncSource(claim.Source) ||
		len(syncResult.Workspaces) != 3 {
		return ErrInvalid
	}
	now = now.UTC()
	for _, commit := range syncResult.Commits {
		if commit.RepositoryID != claim.Repository.ID ||
			gitcli.ValidateFullSHA(commit.CommitSHA) != nil ||
			gitcli.ValidateFullSHA(commit.TreeSHA) != nil {
			return ErrInvalid
		}
		for _, parent := range commit.ParentSHAs {
			if gitcli.ValidateFullSHA(parent) != nil {
				return ErrInvalid
			}
		}
	}
	workspaceResults := map[WorkspaceKind]SyncedWorkspace{}
	for _, workspace := range syncResult.Workspaces {
		if workspace.Status != WorkspaceReady ||
			gitcli.ValidateFullSHA(workspace.HeadCommitSHA) != nil ||
			gitcli.ValidateFullSHA(workspace.TreeSHA) != nil {
			return ErrInvalid
		}
		if _, exists := workspaceResults[workspace.Workspace]; exists {
			return ErrInvalid
		}
		workspaceResults[workspace.Workspace] = workspace
	}
	for _, kind := range []WorkspaceKind{
		WorkspaceCode, WorkspaceArticle, WorkspaceResult,
	} {
		if _, exists := workspaceResults[kind]; !exists {
			return ErrInvalid
		}
	}

	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var connectedAt sql.NullTime
		var lockedBy sql.NullString
		var projectID string
		var status RepositoryStatus
		if err := tx.QueryRowContext(ctx, `
			SELECT connected_at, sync_locked_by, project_id, status
			FROM repo_repositories
			WHERE repository_id = $1
			FOR UPDATE
		`, claim.Repository.ID).Scan(
			&connectedAt, &lockedBy, &projectID, &status,
		); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotConfigured
			}
			return err
		}
		if !lockedBy.Valid || lockedBy.String != owner ||
			status == StatusDisconnected {
			return ErrLocked
		}

		type previousWorkspace struct {
			branch string
			head   sql.NullString
		}
		previous := map[WorkspaceKind]previousWorkspace{}
		rows, err := tx.QueryContext(ctx, `
			SELECT workspace_kind, remote_branch, head_commit_sha
			FROM repo_workspaces
			WHERE repository_id = $1
			ORDER BY workspace_kind
			FOR UPDATE
		`, claim.Repository.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var branch string
			var head sql.NullString
			var kind WorkspaceKind
			if err := rows.Scan(&kind, &branch, &head); err != nil {
				_ = rows.Close()
				return err
			}
			previous[kind] = previousWorkspace{branch: branch, head: head}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(previous) != 3 {
			return ErrBranchMapping
		}

		for _, commit := range syncResult.Commits {
			source := commit.Source
			if source == "" {
				source = "sync"
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO repo_commits (
					repository_id, commit_sha, tree_sha, parent_shas,
					author_name, author_email, author_time,
					committer_name, committer_email, committer_time,
					message, source, first_seen_at
				) VALUES (
					$1, $2, $3, $4::text[],
					$5, $6, $7, $8, $9, $10, $11, $12, $13
				)
				ON CONFLICT (repository_id, commit_sha) DO NOTHING
			`,
				claim.Repository.ID, commit.CommitSHA, commit.TreeSHA,
				postgresSHAArray(commit.ParentSHAs),
				commit.Author.Name, commit.Author.Email, commit.Author.Time.UTC(),
				commit.Committer.Name, commit.Committer.Email,
				commit.Committer.Time.UTC(), commit.Message, source, now,
			); err != nil {
				return err
			}
		}

		for _, kind := range []WorkspaceKind{
			WorkspaceCode, WorkspaceArticle, WorkspaceResult,
		} {
			current := workspaceResults[kind]
			if _, err := tx.ExecContext(ctx, `
				UPDATE repo_workspaces
				SET head_commit_sha = $3, tree_sha = $4, status = 'ready',
				    updated_at = $5
				WHERE repository_id = $1 AND workspace_kind = $2
			`, claim.Repository.ID, kind, current.HeadCommitSHA,
				current.TreeSHA, now); err != nil {
				return err
			}
		}

		if !connectedAt.Valid {
			workspaces := make([]interface{}, 0, 3)
			for _, kind := range []WorkspaceKind{
				WorkspaceCode, WorkspaceArticle, WorkspaceResult,
			} {
				current := workspaceResults[kind]
				workspaces = append(workspaces, map[string]interface{}{
					"workspace":  string(kind),
					"branch":     previous[kind].branch,
					"commit_sha": current.HeadCommitSHA,
				})
			}
			if _, err := store.Outbox.Write(ctx, tx, outbox.Event{
				EventType: "repo.connected",
				Payload: map[string]interface{}{
					"repository_id": claim.Repository.ID,
					"provider":      string(claim.Repository.Provider),
					"workspaces":    workspaces,
				},
				Producer: "repo", ProjectID: projectID,
			}); err != nil {
				return err
			}
		} else {
			for _, kind := range []WorkspaceKind{
				WorkspaceCode, WorkspaceArticle, WorkspaceResult,
			} {
				current := workspaceResults[kind]
				before := previous[kind]
				if before.head.Valid && before.head.String == current.HeadCommitSHA {
					continue
				}
				eventID, err := store.Generator.New()
				if err != nil {
					return err
				}
				result, err := tx.ExecContext(ctx, `
					INSERT INTO repo_commit_events (
						repository_id, workspace_kind, commit_sha, event_type,
						event_id, created_at
					) VALUES ($1, $2, $3, 'repo.commit.detected', $4, $5)
					ON CONFLICT (
						repository_id, workspace_kind, commit_sha, event_type
					) DO NOTHING
				`, claim.Repository.ID, kind, current.HeadCommitSHA, eventID, now)
				if err != nil {
					return err
				}
				affected, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if affected == 0 {
					continue
				}
				var previousSHA interface{}
				if before.head.Valid {
					previousSHA = before.head.String
				}
				if _, err := store.Outbox.Write(ctx, tx, outbox.Event{
					EventID: eventID, EventType: "repo.commit.detected",
					Payload: map[string]interface{}{
						"repository_id":       claim.Repository.ID,
						"workspace":           string(kind),
						"branch":              before.branch,
						"commit_sha":          current.HeadCommitSHA,
						"previous_commit_sha": previousSHA,
						"history_rewritten":   current.HistoryRewritten,
						"source":              claim.Source,
					},
					Producer: "repo", ProjectID: projectID,
				}); err != nil {
					return err
				}
			}
		}

		result, err := tx.ExecContext(ctx, `
			UPDATE repo_repositories
			SET status = 'ready',
			    connected_at = COALESCE(connected_at, $4),
			    last_synced_at = $4,
			    last_error_code = NULL,
			    last_error_message = NULL,
			    sync_requested_at = CASE
			      WHEN sync_requested_at <= $3 THEN NULL
			      ELSE sync_requested_at
			    END,
			    sync_started_at = NULL,
			    sync_locked_by = NULL,
			    sync_lease_expires_at = NULL,
			    sync_attempts = 0,
			    next_sync_at = CASE
			      WHEN sync_requested_at <= $3 THEN NULL
			      ELSE $4
			    END,
			    updated_at = $4
			WHERE repository_id = $1 AND sync_locked_by = $2
		`, claim.Repository.ID, owner, claim.Requested.UTC(), now)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrLocked
		}
		return nil
	})
}

// FailSync stores a bounded failure and releases the current lease for retry.
func (store PostgresStore) FailSync(
	ctx context.Context,
	repositoryID string,
	owner string,
	code string,
	message string,
	retryAt time.Time,
	now time.Time,
) error {
	if repositoryID == "" || owner == "" || code == "" {
		return ErrInvalid
	}
	message = strings.TrimSpace(message)
	if len(message) > 500 {
		message = message[:500]
	}
	now = now.UTC()
	result, err := store.DB.ExecContext(ctx, `
		UPDATE repo_repositories
		SET status = 'error',
		    last_error_code = $3,
		    last_error_message = $4,
		    sync_started_at = NULL,
		    sync_locked_by = NULL,
		    sync_lease_expires_at = NULL,
		    sync_attempts = sync_attempts + 1,
		    next_sync_at = $5,
		    updated_at = $6
		WHERE repository_id = $1 AND sync_locked_by = $2
		  AND status <> 'disconnected'
	`, repositoryID, owner, code, message, retryAt.UTC(), now)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrLocked
	}
	return nil
}

type scanFunction func(...interface{}) error

func scanRepository(scan scanFunction) (Repository, error) {
	var repository Repository
	err := scan(
		&repository.ID,
		&repository.ProjectID,
		&repository.Provider,
		&repository.CanonicalRemoteURL,
		&repository.DisplayName,
		&repository.StorageKey,
		&repository.DefaultBranch,
		&repository.Status,
		&repository.SettingsVersion,
		&repository.Webhook.HookID,
		&repository.ConnectedAt,
		&repository.LastSyncedAt,
		&repository.LastErrorCode,
		&repository.LastErrorMessage,
		&repository.SyncRequestedAt,
		&repository.SyncStartedAt,
		&repository.SyncLockedBy,
		&repository.SyncLeaseExpiresAt,
		&repository.SyncAttempts,
		&repository.NextSyncAt,
		&repository.CleanupAfter,
		&repository.CreatedBy,
		&repository.CreatedAt,
		&repository.UpdatedAt,
	)
	return repository, err
}

func validateSnapshot(snapshot ConnectionSnapshot) error {
	if snapshot.ProjectID == "" ||
		(snapshot.Provider != ProviderGitHub && snapshot.Provider != ProviderLocal) ||
		snapshot.CanonicalRemoteURL == "" ||
		snapshot.DisplayName == "" ||
		snapshot.DefaultBranch == "" ||
		snapshot.SettingsVersion < 1 {
		return ErrInvalid
	}
	return validateMappings(snapshot.Workspaces)
}

func validateMappings(mappings WorkspaceMappings) error {
	if mappings.CodeBranch == "" ||
		mappings.ArticleBranch == "" ||
		mappings.ResultBranch == "" {
		return ErrBranchMapping
	}
	if mappings.CodeBranch == mappings.ArticleBranch ||
		mappings.CodeBranch == mappings.ResultBranch ||
		mappings.ArticleBranch == mappings.ResultBranch {
		return ErrBranchMapping
	}
	return nil
}

func validSyncSource(source string) bool {
	return source == "manual" || source == "webhook" || source == "poll"
}

func postgresSHAArray(values []string) string {
	return "{" + strings.Join(values, ",") + "}"
}

func uniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

func requireAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotConfigured
	}
	return nil
}

var _ Store = PostgresStore{}
