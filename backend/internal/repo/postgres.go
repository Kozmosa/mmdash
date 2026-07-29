package repo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgconn"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// PostgresStore persists repository configuration, status, and leases.
type PostgresStore struct {
	Clock       clock.Clock
	DB          *sql.DB
	Generator   identity.Generator
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
		       settings_version, webhook_id, last_synced_at,
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
		       settings_version, webhook_id, last_synced_at,
		       last_error_code, last_error_message, sync_requested_at,
		       sync_started_at, sync_locked_by, sync_lease_expires_at,
		       sync_attempts, next_sync_at, cleanup_after, created_by,
		       created_at, updated_at
		FROM repo_repositories
		WHERE webhook_id = $1
	`, hookID)
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
	result, err := store.DB.ExecContext(ctx, `
		UPDATE repo_repositories
		SET sync_requested_at = $2,
		    next_sync_at = LEAST(COALESCE(next_sync_at, $2), $2),
		    updated_at = $2
		WHERE project_id = $1 AND status <> 'disconnected'
	`, projectID, now.UTC())
	if err := requireAffected(result, err); err != nil {
		return Repository{}, err
	}
	return store.GetByProject(ctx, projectID)
}

func (store PostgresStore) UpdateMappings(
	ctx context.Context,
	projectID string,
	mappings WorkspaceMappings,
	now time.Time,
) (Repository, error) {
	if err := validateMappings(mappings); err != nil {
		return Repository{}, err
	}
	now = now.UTC()
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var repositoryID string
		if err := tx.QueryRowContext(ctx, `
			SELECT repository_id FROM repo_repositories
			WHERE project_id = $1 AND status <> 'disconnected'
			FOR UPDATE
		`, projectID).Scan(&repositoryID); err != nil {
			return err
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
			SET status = 'pending', sync_requested_at = $2, next_sync_at = $2,
			    last_error_code = NULL, last_error_message = NULL, updated_at = $2
			WHERE repository_id = $1
		`, repositoryID, now)
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
	}
	claimedRows := []claimed{}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			WITH candidates AS (
				SELECT repository_id, sync_requested_at
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
			RETURNING repository.project_id, candidates.sync_requested_at
		`, now, owner, expiresAt, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item claimed
			if err := rows.Scan(&item.projectID, &item.requested); err != nil {
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
		claims = append(claims, SyncClaim{Repository: repository, Requested: item.requested})
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
