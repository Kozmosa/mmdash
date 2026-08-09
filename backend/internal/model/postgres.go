package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgconn"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// PostgresStore keeps Model metadata in Core and creates Model jobs and
// domain events atomically with the state that caused them.
type PostgresStore struct {
	DB          *sql.DB
	Generator   identity.Generator
	Jobs        jobs.TransactionalWriter
	Outbox      outbox.Writer
	Transaction transaction.Manager
}

const sourceColumns = `
	source_id::text,project_id::text,notion_root_page_id::text,
	notion_root_page_url,notion_root_title,auto_sync_enabled,
	auto_sync_interval_seconds,next_sync_at,COALESCE(last_sync_id::text,''),
	sync_status,last_synced_at,last_error_code,last_error_message,
	discovered_page_count,created_by::text,updated_by::text,created_at,updated_at`

const questionColumns = `
	question_id::text,project_id::text,source_id::text,code,title,
	notion_page_id::text,notion_page_url,position,
	COALESCE(latest_snapshot_id::text,''),snapshot_count,sync_status,
	COALESCE(last_sync_id::text,''),last_synced_at,last_error_code,
	last_error_message,created_by::text,updated_by::text,created_at,updated_at`

const syncColumns = `
	sync_id::text,project_id::text,source_id::text,
	COALESCE(question_id::text,''),scope,trigger,status,job_id::text,
	requested_by::text,requested_at,started_at,finished_at,
	COALESCE(created_snapshot_id::text,''),error_code,error_message,updated_at`

const snapshotColumns = `
	s.snapshot_id::text,s.project_id::text,s.question_id::text,
	COALESCE(s.previous_snapshot_id::text,''),s.notion_page_id::text,
	s.notion_page_url,s.title,s.outline,s.blocks,s.content_markdown,
	s.content_text,s.summary,s.content_hash,s.tags,s.version_note,
	s.captured_at,s.triggered_by::text,s.created_at,s.metadata_updated_at`

type rowScanner interface {
	Scan(...interface{}) error
}

func (store PostgresStore) GetSource(ctx context.Context, projectID string) (Source, error) {
	item, err := scanSource(store.DB.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM model_sources WHERE project_id=$1`, projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return Source{}, ErrNotConfigured
	}
	return item, err
}

func (store PostgresStore) UpsertSource(ctx context.Context, config SourceConfig, sourceID string, now time.Time) (Source, error) {
	var item Source
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var previousRoot string
		err := tx.QueryRowContext(ctx, `SELECT notion_root_page_id::text FROM model_sources WHERE project_id=$1 FOR UPDATE`, config.ProjectID).Scan(&previousRoot)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		existed := err == nil
		next := interface{}(nil)
		if config.AutoSyncEnabled {
			next = now.Add(config.Interval)
		}
		item, err = scanSource(tx.QueryRowContext(ctx, `
			INSERT INTO model_sources(
				source_id,project_id,notion_root_page_id,notion_root_page_url,
				auto_sync_enabled,auto_sync_interval_seconds,next_sync_at,
				created_by,updated_by,created_at,updated_at
			) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8,$9,$9)
			ON CONFLICT(project_id) DO UPDATE SET
				notion_root_page_id=EXCLUDED.notion_root_page_id,
				notion_root_page_url=EXCLUDED.notion_root_page_url,
				auto_sync_enabled=EXCLUDED.auto_sync_enabled,
				auto_sync_interval_seconds=EXCLUDED.auto_sync_interval_seconds,
				next_sync_at=EXCLUDED.next_sync_at,updated_by=EXCLUDED.updated_by,
				updated_at=EXCLUDED.updated_at
			RETURNING `+sourceColumns,
			sourceID, config.ProjectID, config.RootPageID, config.RootPageURL,
			config.AutoSyncEnabled, int(config.Interval/time.Second), next,
			config.ActorID, now))
		if err != nil {
			return err
		}
		if previousRoot != "" && previousRoot != config.RootPageID {
			if _, err := tx.ExecContext(ctx, `DELETE FROM model_source_pages WHERE source_id=$1`, item.ID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE model_sources SET notion_root_title='',discovered_page_count=0 WHERE source_id=$1`, item.ID); err != nil {
				return err
			}
			item.NotionRootTitle, item.DiscoveredPageCount = "", 0
		}
		action := "configured"
		if existed {
			action = "updated"
		}
		return store.sourceEvent(ctx, tx, item, action, "active")
	})
	return item, err
}

func (store PostgresStore) DisableSource(ctx context.Context, projectID, actorID string, now time.Time) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		item, err := scanSource(tx.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM model_sources WHERE project_id=$1 FOR UPDATE`, projectID))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE model_sources SET auto_sync_enabled=FALSE,next_sync_at=NULL,scheduler_lease_owner='',scheduler_lease_until=NULL,sync_status='idle',updated_by=$2,updated_at=$3 WHERE project_id=$1`, projectID, actorID, now); err != nil {
			return err
		}
		item.AutoSyncEnabled, item.NextSyncAt, item.SyncStatus = false, nil, SyncIdle
		item.UpdatedBy, item.UpdatedAt = actorID, now
		return store.sourceEvent(ctx, tx, item, "disabled", "hidden")
	})
}

func (store PostgresStore) ListSourcePages(ctx context.Context, projectID string) ([]SourcePage, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT p.notion_page_id::text,COALESCE(p.parent_page_id::text,''),p.title,
			p.page_url,p.depth,p.has_children,COALESCE(q.question_id::text,''),p.last_seen_at
		FROM model_source_pages p
		LEFT JOIN model_questions q ON q.source_id=p.source_id
			AND q.notion_page_id=p.notion_page_id AND q.archived_at IS NULL
		WHERE p.project_id=$1 ORDER BY p.depth,lower(p.title),p.notion_page_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SourcePage{}
	for rows.Next() {
		var item SourcePage
		if err := rows.Scan(&item.NotionPageID, &item.ParentPageID, &item.Title, &item.URL, &item.Depth, &item.HasChildren, &item.BoundQuestionID, &item.LastSeenAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) ListQuestions(ctx context.Context, projectID string) ([]Question, error) {
	rows, err := store.DB.QueryContext(ctx, `SELECT `+questionColumns+` FROM model_questions WHERE project_id=$1 AND archived_at IS NULL ORDER BY position,created_at,question_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Question{}
	for rows.Next() {
		item, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) GetQuestion(ctx context.Context, projectID, questionID string) (Question, error) {
	item, err := scanQuestion(store.DB.QueryRowContext(ctx, `SELECT `+questionColumns+` FROM model_questions WHERE project_id=$1 AND question_id=$2 AND archived_at IS NULL`, projectID, questionID))
	return item, mapModelNotFound(err)
}

func (store PostgresStore) CreateQuestion(ctx context.Context, item Question) (Question, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO model_questions(
				question_id,project_id,source_id,code,title,notion_page_id,
				notion_page_url,position,sync_status,created_by,updated_by,created_at,updated_at
			) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10,$11,$11)`,
			item.ID, item.ProjectID, item.SourceID, item.Code, item.Title,
			item.NotionPageID, item.NotionPageURL, item.Position, item.SyncStatus,
			item.CreatedBy, item.CreatedAt)
		if isUniqueViolation(err) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		return store.questionEvent(ctx, tx, item, "created", "active")
	})
	return item, err
}

func (store PostgresStore) UpdateQuestion(ctx context.Context, projectID, questionID string, input UpdateQuestionInput, actorID string, now time.Time) (Question, error) {
	var item Question
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		current, err := scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionColumns+` FROM model_questions WHERE project_id=$1 AND question_id=$2 AND archived_at IS NULL FOR UPDATE`, projectID, questionID))
		if err != nil {
			return mapModelNotFound(err)
		}
		if input.Code != nil {
			current.Code = *input.Code
		}
		if input.Title != nil {
			current.Title = *input.Title
		}
		if input.Position != nil {
			current.Position = *input.Position
		}
		if input.NotionPageID != nil {
			var pageURL string
			if err := tx.QueryRowContext(ctx, `SELECT page_url FROM model_source_pages WHERE project_id=$1 AND source_id=$2 AND notion_page_id=$3`, projectID, current.SourceID, *input.NotionPageID).Scan(&pageURL); err != nil {
				return mapModelNotFound(err)
			}
			current.NotionPageID, current.NotionPageURL = *input.NotionPageID, pageURL
		}
		current.UpdatedBy, current.UpdatedAt = actorID, now
		_, err = tx.ExecContext(ctx, `UPDATE model_questions SET code=$3,title=$4,notion_page_id=$5,notion_page_url=$6,position=$7,updated_by=$8,updated_at=$9 WHERE project_id=$1 AND question_id=$2`, projectID, questionID, current.Code, current.Title, current.NotionPageID, current.NotionPageURL, current.Position, actorID, now)
		if isUniqueViolation(err) {
			return ErrConflict
		}
		item = current
		if err != nil {
			return err
		}
		return store.questionEvent(ctx, tx, current, "updated", "active")
	})
	return item, err
}

func (store PostgresStore) ArchiveQuestion(ctx context.Context, projectID, questionID, actorID string, now time.Time) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		item, err := scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionColumns+` FROM model_questions WHERE project_id=$1 AND question_id=$2 AND archived_at IS NULL FOR UPDATE`, projectID, questionID))
		if err != nil {
			return mapModelNotFound(err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE model_questions SET archived_at=$3,sync_status='idle',updated_by=$4,updated_at=$3 WHERE project_id=$1 AND question_id=$2 AND archived_at IS NULL`, projectID, questionID, now, actorID)
		if err != nil {
			return err
		}
		if err := requireAffected(result); err != nil {
			return err
		}
		item.UpdatedBy, item.UpdatedAt = actorID, now
		return store.questionEvent(ctx, tx, item, "archived", "hidden")
	})
}

func (store PostgresStore) ListSnapshotSummaries(ctx context.Context, projectID, questionID string) ([]SnapshotSummary, error) {
	rows, err := store.DB.QueryContext(ctx, `SELECT `+snapshotColumns+` FROM model_snapshots s WHERE s.project_id=$1 AND s.question_id=$2 ORDER BY s.captured_at DESC,s.snapshot_id DESC`, projectID, questionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SnapshotSummary{}
	for rows.Next() {
		item, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item.SnapshotSummary)
	}
	return items, rows.Err()
}

func (store PostgresStore) GetSnapshot(ctx context.Context, projectID, questionID, snapshotID string) (Snapshot, error) {
	item, err := scanSnapshot(store.DB.QueryRowContext(ctx, `SELECT `+snapshotColumns+` FROM model_snapshots s WHERE s.project_id=$1 AND s.question_id=$2 AND s.snapshot_id=$3`, projectID, questionID, snapshotID))
	if err != nil {
		return Snapshot{}, mapModelNotFound(err)
	}
	assets, err := store.listSnapshotAssets(ctx, snapshotID)
	if err != nil {
		return Snapshot{}, err
	}
	item.Assets = assets
	return item, nil
}

func (store PostgresStore) UpdateSnapshot(ctx context.Context, projectID, questionID, snapshotID string, input UpdateSnapshotInput, actorID string, now time.Time) (Snapshot, error) {
	var rawTags interface{}
	if input.Tags != nil {
		encoded, err := json.Marshal(*input.Tags)
		if err != nil {
			return Snapshot{}, err
		}
		rawTags = encoded
	}
	_, err := store.DB.ExecContext(ctx, `UPDATE model_snapshots SET tags=COALESCE($4::jsonb,tags),version_note=COALESCE($5,version_note),metadata_updated_by=$6,metadata_updated_at=$7 WHERE project_id=$1 AND question_id=$2 AND snapshot_id=$3`, projectID, questionID, snapshotID, rawTags, input.VersionNote, actorID, now)
	if err != nil {
		return Snapshot{}, err
	}
	return store.GetSnapshot(ctx, projectID, questionID, snapshotID)
}

func (store PostgresStore) CreateSync(ctx context.Context, item Sync, jobInput jobs.CreateInput, nextSyncAt *time.Time) (Sync, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var err error
		item, err = store.CreateSyncInTransaction(ctx, tx, item, jobInput, nextSyncAt)
		return err
	})
	return item, err
}

func (store PostgresStore) CreateSyncInTransaction(ctx context.Context, tx transaction.Tx, item Sync, jobInput jobs.CreateInput, nextSyncAt *time.Time) (Sync, error) {
	// All synchronization requests lock the source first. This keeps manual
	// question clicks and discovery fan-out in one lock order and makes the
	// countdown reset atomic with active-task reuse.
	var sourceID string
	if err := tx.QueryRowContext(ctx, `SELECT source_id::text FROM model_sources WHERE source_id=$1 FOR UPDATE`, item.SourceID).Scan(&sourceID); err != nil {
		return Sync{}, mapModelNotFound(err)
	}
	if item.QuestionID != "" {
		var questionID string
		if err := tx.QueryRowContext(ctx, `SELECT question_id::text FROM model_questions WHERE question_id=$1 AND archived_at IS NULL FOR UPDATE`, item.QuestionID).Scan(&questionID); err != nil {
			return Sync{}, mapModelNotFound(err)
		}
	}
	if nextSyncAt != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE model_sources SET next_sync_at=CASE WHEN auto_sync_enabled THEN $2::timestamptz ELSE NULL END,updated_by=$3,updated_at=$4 WHERE source_id=$1`, item.SourceID, nextSyncAt, item.RequestedBy, item.RequestedAt); err != nil {
			return Sync{}, err
		}
	}

	activeQuery := `SELECT ` + syncColumns + ` FROM model_syncs WHERE source_id=$1 AND scope='source' AND status IN ('queued','running') ORDER BY requested_at DESC,sync_id DESC LIMIT 1`
	activeArg := item.SourceID
	if item.QuestionID != "" {
		activeQuery = `SELECT ` + syncColumns + ` FROM model_syncs WHERE question_id=$1 AND scope='question' AND status IN ('queued','running') ORDER BY requested_at DESC,sync_id DESC LIMIT 1`
		activeArg = item.QuestionID
	}
	active, err := scanSync(tx.QueryRowContext(ctx, activeQuery, activeArg))
	if err == nil {
		return active, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Sync{}, err
	}

	job, created, err := store.Jobs.CreateInTransaction(ctx, tx, item.RequestedBy, jobInput)
	if err != nil {
		return Sync{}, err
	}
	if !created {
		return scanSync(tx.QueryRowContext(ctx, `SELECT `+syncColumns+` FROM model_syncs WHERE job_id=$1`, job.ID))
	}
	item.JobID = job.ID
	_, err = tx.ExecContext(ctx, `INSERT INTO model_syncs(sync_id,project_id,source_id,question_id,scope,trigger,status,job_id,requested_by,requested_at,updated_at) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$10)`, item.ID, item.ProjectID, item.SourceID, item.QuestionID, item.Scope, item.Trigger, item.Status, item.JobID, item.RequestedBy, item.RequestedAt)
	if isUniqueViolation(err) {
		return Sync{}, ErrConflict
	}
	if err != nil {
		return Sync{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE model_sources SET sync_status='queued',last_sync_id=$2,last_error_code='',last_error_message='',updated_by=$3,updated_at=$4 WHERE source_id=$1`, item.SourceID, item.ID, item.RequestedBy, item.RequestedAt)
	if err != nil {
		return Sync{}, err
	}
	if item.QuestionID != "" {
		_, err = tx.ExecContext(ctx, `UPDATE model_questions SET sync_status='queued',last_sync_id=$2,last_error_code='',last_error_message='',updated_by=$3,updated_at=$4 WHERE question_id=$1`, item.QuestionID, item.ID, item.RequestedBy, item.RequestedAt)
		if err != nil {
			return Sync{}, err
		}
	}
	payload := map[string]interface{}{"sync_id": item.ID, "source_id": item.SourceID, "scope": item.Scope, "trigger": item.Trigger, "job_id": item.JobID, "requested_at": item.RequestedAt.Format(time.RFC3339Nano)}
	if item.QuestionID != "" {
		payload["question_id"] = item.QuestionID
	}
	_, err = store.Outbox.Write(ctx, tx, outbox.Event{Actor: map[string]string{"user_id": item.RequestedBy}, EventType: "model.sync.requested", Payload: payload, Producer: "model", ProjectID: item.ProjectID})
	return item, err
}

func (store PostgresStore) GetSyncByJob(ctx context.Context, jobID string) (Sync, error) {
	item, err := scanSync(store.DB.QueryRowContext(ctx, `SELECT `+syncColumns+` FROM model_syncs WHERE job_id=$1`, jobID))
	return item, mapModelNotFound(err)
}

func (store PostgresStore) LatestSnapshotHash(ctx context.Context, questionID string) (string, error) {
	var value string
	err := store.DB.QueryRowContext(ctx, `SELECT content_hash FROM model_snapshots WHERE question_id=$1 ORDER BY captured_at DESC,snapshot_id DESC LIMIT 1`, questionID).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (store PostgresStore) ClaimSyncInTransaction(ctx context.Context, tx transaction.Tx, job jobs.Job, now time.Time) error {
	if !ownedJob(job.JobType) {
		return nil
	}
	var questionID string
	err := tx.QueryRowContext(ctx, `UPDATE model_syncs SET status='running',started_at=COALESCE(started_at,$2),updated_at=$2 WHERE job_id=$1 AND status IN ('queued','running') RETURNING COALESCE(question_id::text,'')`, job.ID, now).Scan(&questionID)
	if err != nil {
		return mapModelNotFound(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model_sources SET sync_status='running',updated_at=$2 WHERE last_sync_id=$1`, jobPayloadString(job, "sync_id"), now); err != nil {
		return err
	}
	if questionID != "" {
		_, err = tx.ExecContext(ctx, `UPDATE model_questions SET sync_status='running',updated_at=$2 WHERE question_id=$1`, questionID, now)
	}
	return err
}

func (store PostgresStore) CompleteDiscoverInTransaction(ctx context.Context, tx transaction.Tx, job jobs.Job, result DiscoverResult, now time.Time) (Sync, error) {
	sync, err := store.syncForUpdate(ctx, tx, job.ID)
	if err != nil {
		return Sync{}, err
	}
	if sync.ID != result.SyncID || sync.Scope != SyncScopeSource {
		return Sync{}, ErrInvalid
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM model_source_pages WHERE source_id=$1`, sync.SourceID); err != nil {
		return Sync{}, err
	}
	for _, page := range result.Pages {
		parent := ""
		if page.ParentPageID != nil {
			parent = *page.ParentPageID
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO model_source_pages(source_id,project_id,notion_page_id,parent_page_id,title,page_url,depth,has_children,last_seen_at) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9)`, sync.SourceID, sync.ProjectID, page.PageID, parent, page.Title, page.URL, page.Depth, page.HasChildren, now); err != nil {
			return Sync{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model_syncs SET status='succeeded',finished_at=$2,error_code='',error_message='',updated_at=$2 WHERE sync_id=$1`, sync.ID, now); err != nil {
		return Sync{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE model_sources SET notion_root_title=$3,sync_status='succeeded',last_synced_at=$2,last_error_code='',last_error_message='',discovered_page_count=$4,updated_at=$2 WHERE source_id=$1`, sync.SourceID, now, result.RootTitle, len(result.Pages))
	return sync, err
}

func (store PostgresStore) ListDiscoveredQuestionsInTransaction(ctx context.Context, tx transaction.Tx, projectID string) ([]Question, error) {
	rows, err := tx.QueryContext(ctx, `SELECT q.question_id::text FROM model_questions q JOIN model_source_pages p ON p.source_id=q.source_id AND p.notion_page_id=q.notion_page_id WHERE q.project_id=$1 AND q.archived_at IS NULL ORDER BY q.position,q.created_at,q.question_id`, projectID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	questions := make([]Question, 0, len(ids))
	for _, id := range ids {
		question, err := scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionColumns+` FROM model_questions WHERE question_id=$1 AND archived_at IS NULL`, id))
		if err != nil {
			return nil, mapModelNotFound(err)
		}
		questions = append(questions, question)
	}
	return questions, nil
}

func (store PostgresStore) CompleteSnapshotInTransaction(ctx context.Context, tx transaction.Tx, job jobs.Job, result SnapshotResult, now time.Time) error {
	sync, err := store.syncForUpdate(ctx, tx, job.ID)
	if err != nil {
		return err
	}
	if sync.ID != result.SyncID || sync.QuestionID != result.QuestionID || sync.Scope != SyncScopeQuestion {
		return ErrInvalid
	}
	question, err := scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionColumns+` FROM model_questions WHERE question_id=$1 AND archived_at IS NULL FOR UPDATE`, sync.QuestionID))
	if err != nil {
		return mapModelNotFound(err)
	}
	var currentHash string
	if question.LatestSnapshotID != "" {
		if err := tx.QueryRowContext(ctx, `SELECT content_hash FROM model_snapshots WHERE snapshot_id=$1`, question.LatestSnapshotID).Scan(&currentHash); err != nil {
			return err
		}
	}
	if currentHash == result.ContentHash {
		if _, err := tx.ExecContext(ctx, `UPDATE model_syncs SET status='unchanged',finished_at=$2,error_code='',error_message='',updated_at=$2 WHERE sync_id=$1`, sync.ID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE model_questions SET sync_status='unchanged',last_synced_at=$2,last_error_code='',last_error_message='',updated_at=$2 WHERE question_id=$1`, question.ID, now); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE model_sources SET sync_status='unchanged',last_synced_at=$2,last_error_code='',last_error_message='',updated_at=$2 WHERE source_id=$1`, sync.SourceID, now)
		return err
	}
	snapshotID, err := store.Generator.New()
	if err != nil {
		return err
	}
	outline, _ := json.Marshal(result.Outline)
	blocks, _ := json.Marshal(result.Blocks)
	tags := []byte(`[]`)
	_, err = tx.ExecContext(ctx, `INSERT INTO model_snapshots(snapshot_id,project_id,question_id,previous_snapshot_id,notion_page_id,notion_page_url,title,outline,blocks,content_markdown,content_text,summary,content_hash,tags,version_note,captured_at,triggered_by,metadata_updated_by,metadata_updated_at,created_at) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'',$15,$16,$16,$15,$15)`, snapshotID, sync.ProjectID, question.ID, question.LatestSnapshotID, question.NotionPageID, question.NotionPageURL, result.Title, outline, blocks, result.ContentMarkdown, result.ContentText, result.Summary, result.ContentHash, tags, now, sync.RequestedBy)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	for _, media := range result.Media {
		if media.ArtifactID == "" || media.ArtifactVersionID == "" {
			return ErrInvalid
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO model_snapshot_assets(snapshot_id,source_block_id,artifact_id,artifact_version_id,filename,mime_type,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, snapshotID, media.SourceBlockID, media.ArtifactID, media.ArtifactVersionID, media.Filename, media.MIMEType, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model_syncs SET status='succeeded',finished_at=$2,created_snapshot_id=$3,error_code='',error_message='',updated_at=$2 WHERE sync_id=$1`, sync.ID, now, snapshotID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model_questions SET latest_snapshot_id=$2,snapshot_count=snapshot_count+1,sync_status='succeeded',last_synced_at=$3,last_error_code='',last_error_message='',updated_at=$3 WHERE question_id=$1`, question.ID, snapshotID, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model_sources SET sync_status='succeeded',last_synced_at=$2,last_error_code='',last_error_message='',updated_at=$2 WHERE source_id=$1`, sync.SourceID, now); err != nil {
		return err
	}
	var previous interface{}
	if question.LatestSnapshotID != "" {
		previous = question.LatestSnapshotID
	}
	_, err = store.Outbox.Write(ctx, tx, outbox.Event{Actor: map[string]string{"user_id": sync.RequestedBy}, EventType: "model.snapshot.created", Payload: map[string]interface{}{"snapshot_id": snapshotID, "question_id": question.ID, "source_id": sync.SourceID, "content_hash": result.ContentHash, "previous_snapshot_id": previous, "captured_at": now.Format(time.RFC3339Nano)}, Producer: "model", ProjectID: sync.ProjectID})
	return err
}

func (store PostgresStore) FailSyncInTransaction(ctx context.Context, tx transaction.Tx, job jobs.Job, failure jobs.Failure, now time.Time) error {
	if !ownedJob(job.JobType) {
		return nil
	}
	sync, err := store.syncForUpdate(ctx, tx, job.ID)
	if err != nil {
		return err
	}
	status := string(job.Status)
	if job.Status == jobs.StatusQueued {
		status = SyncQueued
	}
	terminal := status != SyncQueued
	var finished interface{}
	if terminal {
		finished = now
	}
	message := strings.TrimSpace(failure.Message)
	if runes := []rune(message); len(runes) > 4000 {
		message = string(runes[:4000])
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model_syncs SET status=$2,finished_at=$3,error_code=$4,error_message=$5,updated_at=$6 WHERE sync_id=$1`, sync.ID, status, finished, failure.Code, message, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE model_sources SET sync_status=$2,last_error_code=$3,last_error_message=$4,updated_at=$5 WHERE source_id=$1`, sync.SourceID, status, failure.Code, message, now); err != nil {
		return err
	}
	if sync.QuestionID != "" {
		_, err = tx.ExecContext(ctx, `UPDATE model_questions SET sync_status=$2,last_error_code=$3,last_error_message=$4,updated_at=$5 WHERE question_id=$1`, sync.QuestionID, status, failure.Code, message, now)
	}
	return err
}

func (store PostgresStore) ClaimDueSources(ctx context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]Source, error) {
	if limit < 1 {
		return []Source{}, nil
	}
	items := []Source{}
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+sourceColumns+` FROM model_sources WHERE auto_sync_enabled AND next_sync_at<=$1 AND (scheduler_lease_until IS NULL OR scheduler_lease_until<$1) ORDER BY next_sync_at,source_id FOR UPDATE SKIP LOCKED LIMIT $2`, now, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanSource(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, item := range items {
			if _, err := tx.ExecContext(ctx, `UPDATE model_sources SET scheduler_lease_owner=$2,scheduler_lease_until=$3 WHERE source_id=$1`, item.ID, owner, now.Add(lease)); err != nil {
				return err
			}
		}
		return nil
	})
	return items, err
}

func (store PostgresStore) AdvanceSchedule(ctx context.Context, sourceID, owner string, next, now time.Time) error {
	result, err := store.DB.ExecContext(ctx, `UPDATE model_sources SET next_sync_at=$3,scheduler_lease_owner='',scheduler_lease_until=NULL,updated_at=$4 WHERE source_id=$1 AND scheduler_lease_owner=$2`, sourceID, owner, next, now)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (store PostgresStore) syncForUpdate(ctx context.Context, tx transaction.Tx, jobID string) (Sync, error) {
	item, err := scanSync(tx.QueryRowContext(ctx, `SELECT `+syncColumns+` FROM model_syncs WHERE job_id=$1 FOR UPDATE`, jobID))
	return item, mapModelNotFound(err)
}

func (store PostgresStore) listSnapshotAssets(ctx context.Context, snapshotID string) ([]SnapshotAsset, error) {
	rows, err := store.DB.QueryContext(ctx, `SELECT source_block_id,artifact_id::text,artifact_version_id::text,filename,mime_type FROM model_snapshot_assets WHERE snapshot_id=$1 ORDER BY source_block_id,artifact_id`, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SnapshotAsset{}
	for rows.Next() {
		var item SnapshotAsset
		if err := rows.Scan(&item.SourceBlockID, &item.ArtifactID, &item.ArtifactVersionID, &item.Filename, &item.MIMEType); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanSource(row rowScanner) (Source, error) {
	var item Source
	err := row.Scan(&item.ID, &item.ProjectID, &item.NotionRootPageID, &item.NotionRootPageURL, &item.NotionRootTitle, &item.AutoSyncEnabled, &item.AutoSyncIntervalSeconds, &item.NextSyncAt, &item.LastSyncID, &item.SyncStatus, &item.LastSyncedAt, &item.LastErrorCode, &item.LastErrorMessage, &item.DiscoveredPageCount, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	item.LastSyncStatus = item.SyncStatus
	return item, err
}

func scanQuestion(row rowScanner) (Question, error) {
	var item Question
	err := row.Scan(&item.ID, &item.ProjectID, &item.SourceID, &item.Code, &item.Title, &item.NotionPageID, &item.NotionPageURL, &item.Position, &item.LatestSnapshotID, &item.SnapshotCount, &item.SyncStatus, &item.LastSyncID, &item.LastSyncedAt, &item.LastErrorCode, &item.LastErrorMessage, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanSync(row rowScanner) (Sync, error) {
	var item Sync
	err := row.Scan(&item.ID, &item.ProjectID, &item.SourceID, &item.QuestionID, &item.Scope, &item.Trigger, &item.Status, &item.JobID, &item.RequestedBy, &item.RequestedAt, &item.StartedAt, &item.FinishedAt, &item.CreatedSnapshotID, &item.ErrorCode, &item.ErrorMessage, &item.UpdatedAt)
	return item, err
}

func scanSnapshot(row rowScanner) (Snapshot, error) {
	var item Snapshot
	var outline, blocks, tags []byte
	var triggeredBy string
	err := row.Scan(&item.ID, &item.ProjectID, &item.QuestionID, &item.PreviousSnapshotID, &item.NotionPageID, &item.NotionPageURL, &item.Title, &outline, &blocks, &item.ContentMarkdown, &item.ContentText, &item.Summary, &item.ContentHash, &tags, &item.VersionNote, &item.CapturedAt, &triggeredBy, &item.CreatedAt, &item.MetadataUpdatedAt)
	if err != nil {
		return Snapshot{}, err
	}
	item.TriggeredBy = triggeredBy
	if err := json.Unmarshal(outline, &item.Outline); err != nil {
		return Snapshot{}, err
	}
	if err := json.Unmarshal(blocks, &item.Blocks); err != nil {
		return Snapshot{}, err
	}
	if err := json.Unmarshal(tags, &item.Tags); err != nil {
		return Snapshot{}, err
	}
	if item.Outline == nil {
		item.Outline = []OutlineItem{}
	}
	if item.Blocks == nil {
		item.Blocks = []Block{}
	}
	if item.Tags == nil {
		item.Tags = []string{}
	}
	return item, nil
}

func (store PostgresStore) CreateNotionOAuthAuthorization(ctx context.Context, item NotionOAuthAuthorization) error {
	if _, err := store.DB.ExecContext(ctx, `DELETE FROM model_notion_oauth_authorizations WHERE expires_at<=$1 AND status='pending'`, item.CreatedAt); err != nil {
		return err
	}
	_, err := store.DB.ExecContext(ctx, `
		INSERT INTO model_notion_oauth_authorizations(
			authorization_id,state_hash,project_id,user_id,root_page_id,root_page_url,
			auto_sync_enabled,auto_sync_interval_seconds,status,expires_at,created_at,updated_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'pending',$9,$10,$10)
	`, item.ID, item.StateHash, item.ProjectID, item.UserID, item.RootPageID, item.RootPageURL,
		item.AutoSyncEnabled, item.AutoSyncIntervalSeconds, item.ExpiresAt, item.CreatedAt)
	return err
}

func (store PostgresStore) ClaimNotionOAuthAuthorization(ctx context.Context, stateHash, userID string, now time.Time) (NotionOAuthAuthorization, error) {
	var item NotionOAuthAuthorization
	err := store.DB.QueryRowContext(ctx, `
		UPDATE model_notion_oauth_authorizations
		SET status='exchanging',consumed_at=$3,updated_at=$3
		WHERE state_hash=$1 AND user_id=$2 AND status='pending' AND expires_at>$3
		RETURNING authorization_id::text,state_hash,project_id::text,user_id::text,
		          root_page_id::text,root_page_url,auto_sync_enabled,
		          auto_sync_interval_seconds,status,expires_at,created_at,updated_at
	`, stateHash, userID, now).Scan(
		&item.ID, &item.StateHash, &item.ProjectID, &item.UserID, &item.RootPageID,
		&item.RootPageURL, &item.AutoSyncEnabled, &item.AutoSyncIntervalSeconds,
		&item.Status, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return NotionOAuthAuthorization{}, ErrConflict
	}
	return item, err
}

func (store PostgresStore) CompleteNotionOAuthAuthorization(ctx context.Context, authorizationID, status string, now time.Time) error {
	if status != "succeeded" && status != "denied" && status != "failed" {
		return ErrInvalid
	}
	result, err := store.DB.ExecContext(ctx, `
		UPDATE model_notion_oauth_authorizations SET status=$2,updated_at=$3
		WHERE authorization_id=$1 AND status='exchanging'
	`, authorizationID, status, now)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func requireAffected(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func mapModelNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

func ownedJob(jobType string) bool {
	return jobType == JobTypeDiscover || jobType == JobTypeSnapshot
}

func jobPayloadString(job jobs.Job, key string) string {
	value, _ := job.Payload[key].(string)
	return value
}

func (store PostgresStore) questionEvent(ctx context.Context, tx transaction.Tx, item Question, action, status string) error {
	_, err := store.Outbox.Write(ctx, tx, outbox.Event{
		Actor: map[string]string{"user_id": item.UpdatedBy}, EventType: "model.question.changed",
		Payload: map[string]interface{}{
			"question_id": item.ID, "source_id": item.SourceID, "code": item.Code,
			"title": item.Title, "notion_page_id": item.NotionPageID,
			"action": action, "status": status,
		},
		Producer: "model", ProjectID: item.ProjectID,
	})
	return err
}

func (store PostgresStore) sourceEvent(ctx context.Context, tx transaction.Tx, item Source, action, status string) error {
	_, err := store.Outbox.Write(ctx, tx, outbox.Event{
		Actor: map[string]string{"user_id": item.UpdatedBy}, EventType: "model.source.changed",
		Payload: map[string]interface{}{
			"source_id": item.ID, "notion_root_page_id": item.NotionRootPageID,
			"action": action, "status": status,
		},
		Producer: "model", ProjectID: item.ProjectID,
	})
	return err
}
