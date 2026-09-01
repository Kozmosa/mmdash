package article

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/repo"
)

const commitOperationSelect = `SELECT operation_id,commit_id,project_id,operation_kind,
idempotency_key,COALESCE(publication_id::text,''),COALESCE(publication_key,''),
COALESCE(template_id::text,''),COALESCE(engine,''),COALESCE(bibliography_tool,''),
COALESCE(tag,''),COALESCE(title,''),COALESCE(notes,''),
request_sha256,draft_revision,expected_head_sha,state_vector,yjs_update,tiptap_json,
manuscript,references_bib,manifest_bytes,frozen_references,message,
manuscript_sha256,references_sha256,manifest_sha256,status,stage,
COALESCE(commit_sha,''),COALESCE(previous_commit_sha,''),COALESCE(error_code,''),
attempts,max_attempts,next_attempt_at,COALESCE(locked_by,''),lease_expires_at,
created_by,created_at,updated_at,finished_at FROM article_commit_operations`

func (store PostgresStore) CreateCommitOperation(
	ctx context.Context,
	item CommitOperation,
) (CommitOperation, bool, error) {
	if item.OperationID == "" || item.CommitID == "" || item.ProjectID == "" ||
		item.IdempotencyKey == "" || item.RequestSHA256 == "" ||
		item.ExpectedHeadSHA == "" || item.CreatedBy == "" ||
		(item.OperationKind != "commit" && item.OperationKind != "publication") {
		return CommitOperation{}, false, ErrInvalid
	}
	if item.OperationKind == "publication" &&
		(item.PublicationID == "" || item.PublicationKey == "" ||
			item.TemplateID == "" || item.Engine == "" ||
			item.BibliographyTool == "" || item.Tag == "" || item.Title == "") {
		return CommitOperation{}, false, ErrInvalid
	}
	tiptap, err := json.Marshal(item.TiptapJSON)
	if err != nil {
		return CommitOperation{}, false, ErrInvalid
	}
	frozen, err := json.Marshal(item.FrozenReferences)
	if err != nil {
		return CommitOperation{}, false, ErrInvalid
	}
	created := false
	err = store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO article_commit_operations(
			operation_id,commit_id,project_id,operation_kind,idempotency_key,
			publication_id,publication_key,template_id,engine,bibliography_tool,
			tag,title,notes,request_sha256,draft_revision,expected_head_sha,
			state_vector,yjs_update,tiptap_json,manuscript,references_bib,
			manifest_bytes,frozen_references,message,manuscript_sha256,references_sha256,
			manifest_sha256,status,stage,attempts,max_attempts,next_attempt_at,created_by,
			created_at,updated_at
		) VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,NULLIF($7,''),
			NULLIF($8,'')::uuid,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),
			NULLIF($12,''),CASE WHEN $4='publication' THEN $13 ELSE NULL END,
			$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,
			'queued','queued',0,$28,$29,$30,$31,$31)
		ON CONFLICT(project_id,idempotency_key) DO NOTHING`,
			item.OperationID, item.CommitID, item.ProjectID, item.OperationKind,
			item.IdempotencyKey, item.PublicationID, item.PublicationKey,
			item.TemplateID, item.Engine, item.BibliographyTool, item.Tag,
			item.Title, item.Notes, item.RequestSHA256, item.DraftRevision,
			item.ExpectedHeadSHA, item.StateVector, item.YjsUpdate, tiptap,
			item.Manuscript, item.ReferencesBIB, item.ManifestBytes, frozen,
			item.Message, item.ManuscriptSHA256, item.ReferencesSHA256,
			item.ManifestSHA256, item.MaxAttempts, item.NextAttemptAt.UTC(), item.CreatedBy,
			item.CreatedAt.UTC())
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		created = affected > 0
		if !created {
			return nil
		}
		return store.auditOnly(
			ctx, tx, "article.commit.operation.queued", item.ProjectID,
			item.CreatedBy, "article_commit_operation", item.OperationID,
			map[string]interface{}{
				"draft_revision": item.DraftRevision, "status": "queued",
			},
		)
	})
	if err != nil {
		return CommitOperation{}, false, err
	}
	existing, err := store.GetCommitOperation(ctx, item.ProjectID, item.IdempotencyKey)
	if err != nil {
		return CommitOperation{}, false, err
	}
	if existing.RequestSHA256 != item.RequestSHA256 {
		return CommitOperation{}, false, ErrConflict
	}
	return existing, created, nil
}

func (store PostgresStore) GetCommitOperation(
	ctx context.Context,
	projectID string,
	id string,
) (CommitOperation, error) {
	item, err := scanCommitOperation(store.DB.QueryRowContext(
		ctx, commitOperationSelect+` WHERE project_id=$1
		AND (operation_id::text=$2 OR idempotency_key=$2)`, projectID, id,
	).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return CommitOperation{}, ErrNotFound
	}
	return item, err
}

func (store PostgresStore) ClaimCommitOperations(
	ctx context.Context,
	owner string,
	now time.Time,
	lease time.Duration,
	limit int,
) ([]CommitOperation, error) {
	if owner == "" || lease <= 0 || limit < 1 {
		return nil, ErrInvalid
	}
	rows, err := store.DB.QueryContext(ctx, `WITH candidates AS (
		SELECT operation_id FROM article_commit_operations
		WHERE status IN ('queued','retry_wait','running')
		  AND next_attempt_at <= $1
		  AND (lease_expires_at IS NULL OR lease_expires_at < $1)
		ORDER BY next_attempt_at,created_at
		FOR UPDATE SKIP LOCKED LIMIT $4
	) UPDATE article_commit_operations AS operation
	SET status='running',stage=CASE
		WHEN operation.operation_kind='publication' AND operation.commit_sha IS NOT NULL
		THEN 'publishing' ELSE 'committing' END,attempts=attempts+1,
		locked_by=$2,lease_expires_at=$3,updated_at=$1
	FROM candidates WHERE operation.operation_id=candidates.operation_id
	RETURNING operation.operation_id,operation.commit_id,operation.project_id,
	operation.operation_kind,operation.idempotency_key,
	COALESCE(operation.publication_id::text,''),
	COALESCE(operation.publication_key,''),COALESCE(operation.template_id::text,''),
	COALESCE(operation.engine,''),COALESCE(operation.bibliography_tool,''),
	COALESCE(operation.tag,''),COALESCE(operation.title,''),COALESCE(operation.notes,''),
	operation.request_sha256,operation.draft_revision,
	operation.expected_head_sha,operation.state_vector,operation.yjs_update,
	operation.tiptap_json,operation.manuscript,operation.references_bib,
	operation.manifest_bytes,operation.frozen_references,operation.message,
	operation.manuscript_sha256,operation.references_sha256,
	operation.manifest_sha256,operation.status,operation.stage,
	COALESCE(operation.commit_sha,''),COALESCE(operation.previous_commit_sha,''),
	COALESCE(operation.error_code,''),operation.attempts,operation.max_attempts,
	operation.next_attempt_at,COALESCE(operation.locked_by,''),
	operation.lease_expires_at,operation.created_by,operation.created_at,
	operation.updated_at,operation.finished_at`, now.UTC(), owner,
		now.UTC().Add(lease), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CommitOperation{}
	for rows.Next() {
		item, scanErr := scanCommitOperation(rows.Scan)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) RenewCommitOperationLease(
	ctx context.Context,
	operationID string,
	owner string,
	expiresAt time.Time,
) error {
	result, err := store.DB.ExecContext(ctx, `UPDATE article_commit_operations
	SET lease_expires_at=$3,updated_at=NOW()
	WHERE operation_id=$1 AND locked_by=$2 AND status='running'`,
		operationID, owner, expiresAt.UTC())
	return requireArticleAffected(result, err)
}

func (store PostgresStore) BindCommitOperation(
	ctx context.Context,
	operation CommitOperation,
	item Commit,
	now time.Time,
) (Commit, error) {
	frozen, _ := json.Marshal(item.FrozenReferences)
	tiptap, _ := json.Marshal(item.TiptapJSON)
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO article_commits(
			commit_id,project_id,draft_revision,state_vector,yjs_update,tiptap_json,
			git_commit_sha,previous_git_commit_sha,message,manuscript_sha256,
			references_sha256,manifest_sha256,frozen_references,created_by,created_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT(project_id,git_commit_sha) DO NOTHING`, item.CommitID,
			item.ProjectID, item.DraftRevision, item.StateVector, item.YjsUpdate,
			tiptap, item.CommitSHA, item.PreviousCommitSHA, item.Message,
			item.ManuscriptSHA256, item.ReferencesSHA256, item.ManifestSHA256,
			frozen, item.CreatedBy, item.CreatedAt.UTC())
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			if err := tx.QueryRowContext(ctx, `SELECT commit_id
				FROM article_commits WHERE project_id=$1 AND git_commit_sha=$2`,
				item.ProjectID, item.CommitSHA,
			).Scan(&item.CommitID); err != nil {
				return err
			}
		}
		if affected > 0 {
			if err := store.record(ctx, tx, "article.commit.created", item.ProjectID,
				item.CreatedBy, "article_commit", item.CommitID,
				map[string]interface{}{
					"commit_id": item.CommitID, "commit_sha": item.CommitSHA,
					"draft_revision":    item.DraftRevision,
					"manuscript_sha256": item.ManuscriptSHA256,
					"status":            "committed",
				}); err != nil {
				return err
			}
		}
		stage := "committing"
		if operation.OperationKind == "publication" {
			stage = "publishing"
		}
		result, err = tx.ExecContext(ctx, `UPDATE article_commit_operations
		SET stage=$4,commit_sha=$5,previous_commit_sha=$6,error_code=NULL,
			updated_at=$7,commit_id=$8
		WHERE operation_id=$1 AND project_id=$2 AND locked_by=$3
		  AND status='running'`, operation.OperationID, operation.ProjectID,
			operation.LockedBy, stage, item.CommitSHA, item.PreviousCommitSHA,
			now.UTC(), item.CommitID)
		return requireArticleAffected(result, err)
	})
	if err != nil {
		return Commit{}, err
	}
	return store.getCommitBySHA(ctx, item.ProjectID, item.CommitSHA)
}

func (store PostgresStore) CompleteCommitOperation(
	ctx context.Context,
	operation CommitOperation,
	now time.Time,
) error {
	result, err := store.DB.ExecContext(ctx, `UPDATE article_commit_operations
	SET status='succeeded',stage='completed',error_code=NULL,locked_by=NULL,
		lease_expires_at=NULL,finished_at=$4,updated_at=$4
	WHERE operation_id=$1 AND project_id=$2 AND locked_by=$3
	  AND status='running'`, operation.OperationID, operation.ProjectID,
		operation.LockedBy, now.UTC())
	return requireArticleAffected(result, err)
}

func (store PostgresStore) FailCommitOperation(
	ctx context.Context,
	operation CommitOperation,
	code string,
	terminal bool,
	retryAt time.Time,
	now time.Time,
) error {
	result, err := store.DB.ExecContext(ctx, `UPDATE article_commit_operations
	SET status=CASE WHEN $5 OR attempts>=max_attempts THEN 'failed'
		ELSE 'retry_wait' END,
		stage=CASE WHEN $5 OR attempts>=max_attempts THEN 'failed' ELSE 'queued' END,
		error_code=$4,next_attempt_at=$6,locked_by=NULL,lease_expires_at=NULL,
		finished_at=CASE WHEN $5 OR attempts>=max_attempts THEN $7 ELSE NULL END,
		updated_at=$7
	WHERE operation_id=$1 AND project_id=$2 AND locked_by=$3
	  AND status='running'`, operation.OperationID, operation.ProjectID,
		operation.LockedBy, code, terminal, retryAt.UTC(), now.UTC())
	return requireArticleAffected(result, err)
}

func scanCommitOperation(scan func(...interface{}) error) (CommitOperation, error) {
	var item CommitOperation
	var tiptap, frozen []byte
	err := scan(&item.OperationID, &item.CommitID, &item.ProjectID,
		&item.OperationKind, &item.IdempotencyKey, &item.PublicationID,
		&item.PublicationKey, &item.TemplateID, &item.Engine,
		&item.BibliographyTool, &item.Tag, &item.Title, &item.Notes,
		&item.RequestSHA256, &item.DraftRevision,
		&item.ExpectedHeadSHA, &item.StateVector, &item.YjsUpdate, &tiptap,
		&item.Manuscript, &item.ReferencesBIB, &item.ManifestBytes, &frozen,
		&item.Message, &item.ManuscriptSHA256, &item.ReferencesSHA256,
		&item.ManifestSHA256, &item.Status, &item.Stage, &item.CommitSHA,
		&item.PreviousCommitSHA, &item.ErrorCode, &item.Attempts,
		&item.MaxAttempts, &item.NextAttemptAt, &item.LockedBy,
		&item.LeaseExpiresAt, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
		&item.FinishedAt)
	if err != nil {
		return CommitOperation{}, err
	}
	if json.Unmarshal(tiptap, &item.TiptapJSON) != nil ||
		json.Unmarshal(frozen, &item.FrozenReferences) != nil {
		return CommitOperation{}, ErrInvalid
	}
	return item, nil
}

func requireArticleAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrConflict
	}
	return nil
}

// CommitOperationCoordinator runs Article Git writes inside Core. It does not
// expose Repo credentials or worktree paths to the Python Worker.
type CommitOperationCoordinator struct {
	Clock   interface{ Now() time.Time }
	Lease   time.Duration
	Limit   int
	OnError func(error)
	Owner   string
	Poll    time.Duration
	Service *Service
	Store   Store
}

func (coordinator CommitOperationCoordinator) Run(ctx context.Context) {
	poll := coordinator.Poll
	if poll <= 0 {
		poll = time.Second
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := coordinator.RunOnce(ctx); err != nil && coordinator.OnError != nil {
				coordinator.OnError(err)
			}
			timer.Reset(poll)
		}
	}
}

func (coordinator CommitOperationCoordinator) RunOnce(ctx context.Context) error {
	if coordinator.Clock == nil || coordinator.Owner == "" ||
		coordinator.Service == nil || coordinator.Store == nil {
		return ErrInvalid
	}
	lease := coordinator.Lease
	if lease <= 0 {
		lease = 90 * time.Second
	}
	limit := coordinator.Limit
	if limit < 1 {
		limit = 4
	}
	items, err := coordinator.Store.ClaimCommitOperations(
		ctx, coordinator.Owner, coordinator.Clock.Now().UTC(), lease, limit,
	)
	if err != nil {
		return err
	}
	var firstError error
	var mutex sync.Mutex
	var group sync.WaitGroup
	for _, item := range items {
		item := item
		group.Add(1)
		go func() {
			defer group.Done()
			if processErr := coordinator.process(ctx, item, lease); processErr != nil {
				mutex.Lock()
				if firstError == nil {
					firstError = processErr
				}
				mutex.Unlock()
			}
		}()
	}
	group.Wait()
	return firstError
}

func (coordinator CommitOperationCoordinator) process(
	ctx context.Context,
	operation CommitOperation,
	lease time.Duration,
) error {
	if operation.OperationKind != "commit" && operation.OperationKind != "publication" {
		return coordinator.Store.FailCommitOperation(
			context.WithoutCancel(ctx), operation, "ARTICLE_OPERATION_INVALID",
			true, coordinator.Clock.Now().UTC(), coordinator.Clock.Now().UTC(),
		)
	}
	operationContext, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	renewInterval := lease / 3
	if renewInterval <= 0 {
		renewInterval = time.Second
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(renewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-operationContext.Done():
				return
			case <-ticker.C:
				if err := coordinator.Store.RenewCommitOperationLease(
					operationContext, operation.OperationID, operation.LockedBy,
					coordinator.Clock.Now().UTC().Add(lease),
				); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	var commit Commit
	var err error
	if operation.CommitSHA == "" {
		var result repo.CommitResult
		result, err = coordinator.Service.Workspace.Commit(
			operationContext, repo.WorkspaceCommitRequest{
				ActorEmail: "article@mmdash.local", ActorID: operation.CreatedBy,
				ActorName: "mmdash Article",
				Changes: []repo.FileChange{
					{Path: "manuscript.md", Operation: "put", Content: []byte(operation.Manuscript)},
					{Path: "references.bib", Operation: "put", Content: []byte(operation.ReferencesBIB)},
					{Path: ".mmdash/article.json", Operation: "put", Content: operation.ManifestBytes},
				},
				ExpectedHeadSHA: operation.ExpectedHeadSHA,
				IdempotencyKey:  "article-operation:" + operation.OperationID,
				Message:         operation.Message, ProjectID: operation.ProjectID,
				RequestSHA256: operation.RequestSHA256,
			},
		)
		if err == nil {
			commit = Commit{
				CommitID: operation.CommitID, ProjectID: operation.ProjectID,
				DraftRevision: operation.DraftRevision, StateVector: operation.StateVector,
				TiptapJSON: operation.TiptapJSON, YjsUpdate: operation.YjsUpdate,
				CommitSHA: result.CommitSHA, PreviousCommitSHA: result.PreviousCommitSHA,
				Message: operation.Message, ManuscriptSHA256: operation.ManuscriptSHA256,
				ReferencesSHA256: operation.ReferencesSHA256,
				ManifestSHA256:   operation.ManifestSHA256,
				FrozenReferences: operation.FrozenReferences,
				CreatedBy:        operation.CreatedBy, CreatedAt: operation.CreatedAt,
			}
		}
	} else {
		commit, err = coordinator.Store.GetCommit(
			operationContext, operation.ProjectID, operation.CommitID,
		)
	}
	cancel()
	<-done
	now := coordinator.Clock.Now().UTC()
	finalContext, finalCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finalCancel()
	if err != nil {
		code, terminal := commitOperationFailure(err)
		retryAt := now.Add(commitOperationRetry(operation.Attempts))
		return coordinator.Store.FailCommitOperation(
			finalContext, operation, code, terminal, retryAt, now,
		)
	}
	commit, err = coordinator.Store.BindCommitOperation(
		finalContext, operation, commit, now,
	)
	if err != nil {
		return err
	}
	if operation.OperationKind == "publication" {
		if err = coordinator.Service.createPublicationFromOperation(
			finalContext, operation, commit,
		); err != nil {
			code, terminal := articleOperationFailure(err)
			return coordinator.Store.FailCommitOperation(
				finalContext, operation, code, terminal,
				now.Add(commitOperationRetry(operation.Attempts)), now,
			)
		}
	}
	return coordinator.Store.CompleteCommitOperation(finalContext, operation, now)
}

func articleOperationFailure(err error) (string, bool) {
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrConflict):
		return "ARTICLE_PUBLICATION_INVALID", true
	case errors.Is(err, ErrNotFound):
		return "ARTICLE_PUBLICATION_DEPENDENCY_NOT_FOUND", true
	case errors.Is(err, ErrNotReady):
		return "ARTICLE_PUBLICATION_NOT_READY", false
	default:
		return "ARTICLE_PUBLICATION_FAILED", false
	}
}

func commitOperationFailure(err error) (string, bool) {
	switch {
	case errors.Is(err, repo.ErrHeadChanged):
		return "REPO_HEAD_CHANGED", true
	case errors.Is(err, repo.ErrNoChanges):
		return "REPO_NO_CHANGES", true
	case errors.Is(err, repo.ErrInvalid):
		return "REPO_COMMIT_INVALID", true
	case errors.Is(err, repo.ErrLocked):
		return "REPO_WRITE_IN_PROGRESS", false
	default:
		return "REPO_COMMIT_FAILED", false
	}
}

func commitOperationRetry(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	delay := time.Second * time.Duration(1<<uint(attempt-1))
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}
