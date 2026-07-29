package repo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
)

// CheckoutStore persists detached checkout leases without exposing paths.
type CheckoutStore interface {
	CreateCheckout(context.Context, Checkout) error
	ExpireCheckouts(context.Context, time.Time, int) ([]Checkout, error)
	GetCheckout(context.Context, string, string) (Checkout, error)
	ListActiveCheckouts(context.Context, string) ([]Checkout, error)
	MarkCheckoutError(context.Context, string) error
	ReleaseCheckout(context.Context, string, string, time.Time) (Checkout, error)
}

// CommitStore persists idempotency, write leases, heads, commits, and events.
type CommitStore interface {
	BeginCommit(
		context.Context, WorkspaceCommitRequest, string, time.Duration, time.Time,
	) (CommitClaim, error)
	CompleteCommit(
		context.Context, CommitClaim, Commit, CommitResult, time.Time,
	) error
	FailCommit(context.Context, CommitClaim, string, time.Time) error
	SavePreparedCommit(
		context.Context, CommitClaim, string, time.Time,
	) error
}

func (store PostgresStore) CreateCheckout(
	ctx context.Context,
	checkout Checkout,
) error {
	_, err := store.DB.ExecContext(ctx, `
		INSERT INTO repo_checkouts (
			checkout_id, repository_id, commit_sha, purpose, created_by,
			checkout_relpath, status, created_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, 'active', $7, $8)
	`, checkout.CheckoutID, checkout.RepositoryID, checkout.CommitSHA,
		checkout.Purpose, checkout.CreatedBy, checkout.CheckoutRelpath,
		checkout.CreatedAt.UTC(), checkout.ExpiresAt.UTC())
	return err
}

func (store PostgresStore) GetCheckout(
	ctx context.Context,
	projectID string,
	checkoutID string,
) (Checkout, error) {
	checkout, err := scanCheckout(store.DB.QueryRowContext(ctx, `
		SELECT checkout.checkout_id, checkout.repository_id,
		       checkout.commit_sha, checkout.purpose, checkout.created_by,
		       checkout.checkout_relpath, checkout.status, checkout.created_at,
		       checkout.expires_at, checkout.released_at
		FROM repo_checkouts AS checkout
		JOIN repo_repositories AS repository USING (repository_id)
		WHERE repository.project_id = $1 AND checkout.checkout_id = $2
	`, projectID, checkoutID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Checkout{}, ErrCheckoutNotFound
	}
	return checkout, err
}

func (store PostgresStore) ListActiveCheckouts(
	ctx context.Context,
	repositoryID string,
) ([]Checkout, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT checkout_id, repository_id, commit_sha, purpose, created_by,
		       checkout_relpath, status, created_at, expires_at, released_at
		FROM repo_checkouts
		WHERE repository_id = $1 AND status = 'active'
		ORDER BY checkout_id
	`, repositoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	checkouts := []Checkout{}
	for rows.Next() {
		checkout, err := scanCheckout(rows.Scan)
		if err != nil {
			return nil, err
		}
		checkouts = append(checkouts, checkout)
	}
	return checkouts, rows.Err()
}

func (store PostgresStore) ReleaseCheckout(
	ctx context.Context,
	projectID string,
	checkoutID string,
	now time.Time,
) (Checkout, error) {
	checkout, err := scanCheckout(store.DB.QueryRowContext(ctx, `
		UPDATE repo_checkouts AS checkout
		SET status = CASE
		      WHEN checkout.status = 'active' THEN 'released'
		      ELSE checkout.status
		    END,
		    released_at = CASE
		      WHEN checkout.status = 'active' THEN $3
		      ELSE checkout.released_at
		    END
		FROM repo_repositories AS repository
		WHERE checkout.repository_id = repository.repository_id
		  AND repository.project_id = $1
		  AND checkout.checkout_id = $2
		RETURNING checkout.checkout_id, checkout.repository_id,
		          checkout.commit_sha, checkout.purpose, checkout.created_by,
		          checkout.checkout_relpath, checkout.status,
		          checkout.created_at, checkout.expires_at,
		          checkout.released_at
	`, projectID, checkoutID, now.UTC()).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Checkout{}, ErrCheckoutNotFound
	}
	return checkout, err
}

func (store PostgresStore) ExpireCheckouts(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]Checkout, error) {
	if limit < 1 {
		return nil, ErrInvalid
	}
	rows, err := store.DB.QueryContext(ctx, `
		WITH candidates AS (
			SELECT checkout_id
			FROM repo_checkouts
			WHERE (status = 'active' AND expires_at <= $1)
			   OR status = 'error'
			ORDER BY expires_at, checkout_id
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		UPDATE repo_checkouts AS checkout
		SET status = 'expired', released_at = $1
		FROM candidates
		WHERE checkout.checkout_id = candidates.checkout_id
		RETURNING checkout.checkout_id, checkout.repository_id,
		          checkout.commit_sha, checkout.purpose, checkout.created_by,
		          checkout.checkout_relpath, checkout.status,
		          checkout.created_at, checkout.expires_at,
		          checkout.released_at
	`, now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	checkouts := []Checkout{}
	for rows.Next() {
		checkout, err := scanCheckout(rows.Scan)
		if err != nil {
			return nil, err
		}
		checkouts = append(checkouts, checkout)
	}
	return checkouts, rows.Err()
}

func (store PostgresStore) MarkCheckoutError(
	ctx context.Context,
	checkoutID string,
) error {
	result, err := store.DB.ExecContext(ctx, `
		UPDATE repo_checkouts SET status = 'error'
		WHERE checkout_id = $1 AND status IN ('active', 'expired')
	`, checkoutID)
	return requireAffected(result, err)
}

func (store PostgresStore) BeginCommit(
	ctx context.Context,
	request WorkspaceCommitRequest,
	owner string,
	lease time.Duration,
	now time.Time,
) (CommitClaim, error) {
	if owner == "" || lease <= 0 ||
		request.ProjectID == "" || request.ActorID == "" ||
		request.IdempotencyKey == "" || request.RequestSHA256 == "" ||
		gitcli.ValidateFullSHA(request.ExpectedHeadSHA) != nil {
		return CommitClaim{}, ErrInvalid
	}
	now = now.UTC()
	claim := CommitClaim{
		ActorID: request.ActorID, IdempotencyKey: request.IdempotencyKey,
		Owner: owner, RequestSHA256: request.RequestSHA256,
		Repository: Repository{ProjectID: request.ProjectID},
		Workspace:  Workspace{Workspace: request.Workspace},
	}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var leaseExpires sql.NullTime
		var lockedBy sql.NullString
		var repositoryID string
		var status RepositoryStatus
		if err := tx.QueryRowContext(ctx, `
			SELECT repository_id, status, sync_locked_by, sync_lease_expires_at
			FROM repo_repositories
			WHERE project_id = $1
			FOR UPDATE
		`, request.ProjectID).Scan(
			&repositoryID, &status, &lockedBy, &leaseExpires,
		); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotConfigured
			}
			return err
		}
		claim.Repository.ID = repositoryID
		var actorID string
		var commitSHA sql.NullString
		var existingError sql.NullString
		var existingHash string
		var existingLease sql.NullTime
		var existingOwner sql.NullString
		var existingStatus string
		var expectedHead string
		requestErr := tx.QueryRowContext(ctx, `
			SELECT request_sha256, expected_head_sha, commit_sha, status,
			       error_code, actor_id, locked_by, lease_expires_at
			FROM repo_commit_requests
			WHERE repository_id = $1 AND workspace_kind = $2
			  AND idempotency_key = $3
			FOR UPDATE
		`, repositoryID, request.Workspace, request.IdempotencyKey).Scan(
			&existingHash, &expectedHead, &commitSHA, &existingStatus,
			&existingError, &actorID, &existingOwner, &existingLease,
		)
		exists := requestErr == nil
		if requestErr != nil && !errors.Is(requestErr, sql.ErrNoRows) {
			return requestErr
		}
		if exists {
			if existingHash != request.RequestSHA256 ||
				expectedHead != request.ExpectedHeadSHA ||
				actorID != request.ActorID {
				return ErrConflict
			}
			claim.PreparedCommitSHA = commitSHA.String
			if existingStatus == "succeeded" {
				claim.AlreadySucceeded = true
				return nil
			}
		}
		if status != StatusReady {
			return ErrNotReady
		}
		var workspaceHead sql.NullString
		var workspaceStatus WorkspaceStatus
		if err := tx.QueryRowContext(ctx, `
			SELECT head_commit_sha, status
			FROM repo_workspaces
			WHERE repository_id = $1 AND workspace_kind = $2
			FOR UPDATE
		`, repositoryID, request.Workspace).Scan(
			&workspaceHead, &workspaceStatus,
		); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotReady
			}
			return err
		}
		if workspaceStatus != WorkspaceReady {
			return ErrNotReady
		}
		headMatchesExpected := workspaceHead.Valid &&
			workspaceHead.String == request.ExpectedHeadSHA
		headMatchesPrepared := commitSHA.Valid && workspaceHead.Valid &&
			workspaceHead.String == commitSHA.String
		if !headMatchesExpected && !headMatchesPrepared {
			return ErrHeadChanged
		}
		if lockedBy.Valid &&
			(!leaseExpires.Valid || leaseExpires.Time.After(now)) {
			return ErrLocked
		}
		if exists && existingStatus == "pending" &&
			existingOwner.Valid && existingOwner.String != owner &&
			existingLease.Valid && existingLease.Time.After(now) {
			return ErrLocked
		}
		expiresAt := now.Add(lease)
		if !exists {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO repo_commit_requests (
					repository_id, workspace_kind, idempotency_key,
					expected_head_sha, status, request_sha256, actor_id,
					locked_by, lease_expires_at, created_at, updated_at
				) VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7, $8, $9, $9)
			`, repositoryID, request.Workspace, request.IdempotencyKey,
				request.ExpectedHeadSHA, request.RequestSHA256,
				request.ActorID, owner, expiresAt, now); err != nil {
				return err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				UPDATE repo_commit_requests
				SET status = 'pending', error_code = NULL,
				    locked_by = $4, lease_expires_at = $5, updated_at = $6
				WHERE repository_id = $1 AND workspace_kind = $2
				  AND idempotency_key = $3
			`, repositoryID, request.Workspace, request.IdempotencyKey,
				owner, expiresAt, now); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE repo_repositories
			SET sync_locked_by = $2, sync_lease_expires_at = $3,
			    updated_at = $4
			WHERE repository_id = $1
		`, repositoryID, owner, expiresAt, now)
		return err
	})
	if err != nil {
		return CommitClaim{}, err
	}
	repository, err := store.GetByProject(ctx, request.ProjectID)
	if err != nil {
		_ = store.FailCommit(
			ctx, claim, "REPO_COMMIT_FAILED", now,
		)
		return CommitClaim{}, err
	}
	workspace, err := findWorkspace(repository, request.Workspace)
	if err != nil {
		_ = store.FailCommit(
			ctx, claim, "REPO_COMMIT_FAILED", now,
		)
		return CommitClaim{}, err
	}
	claim.Repository = repository
	claim.Workspace = workspace
	return claim, nil
}

func (store PostgresStore) SavePreparedCommit(
	ctx context.Context,
	claim CommitClaim,
	commitSHA string,
	now time.Time,
) error {
	if gitcli.ValidateFullSHA(commitSHA) != nil {
		return ErrInvalid
	}
	result, err := store.DB.ExecContext(ctx, `
		UPDATE repo_commit_requests
		SET commit_sha = $5, updated_at = $6
		WHERE repository_id = $1 AND workspace_kind = $2
		  AND idempotency_key = $3 AND locked_by = $4
		  AND status = 'pending'
		  AND (commit_sha IS NULL OR commit_sha = $5)
	`, claim.Repository.ID, claim.Workspace.Workspace, claim.IdempotencyKey,
		claim.Owner, commitSHA, now.UTC())
	if err := requireAffected(result, err); err != nil {
		if errors.Is(err, ErrNotConfigured) {
			return ErrLocked
		}
		return err
	}
	return nil
}

func (store PostgresStore) CompleteCommit(
	ctx context.Context,
	claim CommitClaim,
	commit Commit,
	result CommitResult,
	now time.Time,
) error {
	if claim.Repository.ID == "" ||
		claim.PreparedCommitSHA != commit.CommitSHA ||
		result.CommitSHA != commit.CommitSHA ||
		gitcli.ValidateFullSHA(commit.CommitSHA) != nil ||
		gitcli.ValidateFullSHA(commit.TreeSHA) != nil {
		return ErrInvalid
	}
	now = now.UTC()
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var lockedBy sql.NullString
		if err := tx.QueryRowContext(ctx, `
			SELECT sync_locked_by FROM repo_repositories
			WHERE repository_id = $1
			FOR UPDATE
		`, claim.Repository.ID).Scan(&lockedBy); err != nil {
			return err
		}
		if !lockedBy.Valid || lockedBy.String != claim.Owner {
			return ErrLocked
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO repo_commits (
				repository_id, commit_sha, tree_sha, parent_shas,
				author_name, author_email, author_time,
				committer_name, committer_email, committer_time,
				message, source, first_seen_at
			) VALUES (
				$1, $2, $3, $4::text[], $5, $6, $7,
				$8, $9, $10, $11, 'mmdash', $12
			)
			ON CONFLICT (repository_id, commit_sha) DO NOTHING
		`, claim.Repository.ID, commit.CommitSHA, commit.TreeSHA,
			postgresSHAArray(commit.ParentSHAs),
			commit.Author.Name, commit.Author.Email, commit.Author.Time.UTC(),
			commit.Committer.Name, commit.Committer.Email,
			commit.Committer.Time.UTC(), commit.Message, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE repo_workspaces
			SET head_commit_sha = $3, tree_sha = $4, status = 'ready',
			    updated_at = $5
			WHERE repository_id = $1 AND workspace_kind = $2
		`, claim.Repository.ID, claim.Workspace.Workspace,
			commit.CommitSHA, commit.TreeSHA, now); err != nil {
			return err
		}
		eventID, err := store.Generator.New()
		if err != nil {
			return err
		}
		inserted, err := tx.ExecContext(ctx, `
			INSERT INTO repo_commit_events (
				repository_id, workspace_kind, commit_sha, event_type,
				event_id, created_at
			) VALUES ($1, $2, $3, 'repo.commit.created', $4, $5)
			ON CONFLICT (
				repository_id, workspace_kind, commit_sha, event_type
			) DO NOTHING
		`, claim.Repository.ID, claim.Workspace.Workspace,
			commit.CommitSHA, eventID, now)
		if err != nil {
			return err
		}
		affected, err := inserted.RowsAffected()
		if err != nil {
			return err
		}
		if affected > 0 {
			if _, err := store.Outbox.Write(ctx, tx, outbox.Event{
				Actor:   map[string]string{"user_id": claim.ActorID},
				EventID: eventID, EventType: "repo.commit.created",
				Payload: map[string]interface{}{
					"repository_id":       claim.Repository.ID,
					"workspace":           string(claim.Workspace.Workspace),
					"branch":              claim.Workspace.RemoteBranch,
					"commit_sha":          commit.CommitSHA,
					"previous_commit_sha": result.PreviousCommitSHA,
					"history_rewritten":   false,
					"source":              "mmdash",
				},
				Producer: "repo", ProjectID: claim.Repository.ProjectID,
			}); err != nil {
				return err
			}
		}
		requestUpdate, err := tx.ExecContext(ctx, `
			UPDATE repo_commit_requests
			SET status = 'succeeded', commit_sha = $5, error_code = NULL,
			    locked_by = NULL, lease_expires_at = NULL, updated_at = $6
			WHERE repository_id = $1 AND workspace_kind = $2
			  AND idempotency_key = $3 AND locked_by = $4
		`, claim.Repository.ID, claim.Workspace.Workspace,
			claim.IdempotencyKey, claim.Owner, commit.CommitSHA, now)
		if err := requireAffected(requestUpdate, err); err != nil {
			return ErrLocked
		}
		repositoryUpdate, err := tx.ExecContext(ctx, `
			UPDATE repo_repositories
			SET status = 'ready', last_synced_at = $3,
			    sync_locked_by = NULL, sync_lease_expires_at = NULL,
			    last_error_code = NULL, last_error_message = NULL,
			    updated_at = $3
			WHERE repository_id = $1 AND sync_locked_by = $2
		`, claim.Repository.ID, claim.Owner, now)
		if err := requireAffected(repositoryUpdate, err); err != nil {
			return ErrLocked
		}
		return nil
	})
}

func (store PostgresStore) FailCommit(
	ctx context.Context,
	claim CommitClaim,
	code string,
	now time.Time,
) error {
	if code == "" {
		code = "REPO_COMMIT_FAILED"
	}
	now = now.UTC()
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE repo_commit_requests
			SET status = 'failed', error_code = $5,
			    locked_by = NULL, lease_expires_at = NULL, updated_at = $6
			WHERE repository_id = $1 AND workspace_kind = $2
			  AND idempotency_key = $3 AND locked_by = $4
		`, claim.Repository.ID, claim.Workspace.Workspace,
			claim.IdempotencyKey, claim.Owner, code, now); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE repo_repositories
			SET status = 'ready', sync_locked_by = NULL,
			    sync_lease_expires_at = NULL, updated_at = $3
			WHERE repository_id = $1 AND sync_locked_by = $2
		`, claim.Repository.ID, claim.Owner, now)
		return err
	})
}

func scanCheckout(scan scanFunction) (Checkout, error) {
	var checkout Checkout
	err := scan(
		&checkout.CheckoutID,
		&checkout.RepositoryID,
		&checkout.CommitSHA,
		&checkout.Purpose,
		&checkout.CreatedBy,
		&checkout.CheckoutRelpath,
		&checkout.Status,
		&checkout.CreatedAt,
		&checkout.ExpiresAt,
		&checkout.ReleasedAt,
	)
	return checkout, err
}

var (
	_ CheckoutStore = PostgresStore{}
	_ CommitStore   = PostgresStore{}
)
