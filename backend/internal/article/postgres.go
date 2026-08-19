package article

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

type transactionalAudit interface {
	RecordInTransaction(context.Context, transaction.Tx, audit.Event) error
}

type PostgresStore struct {
	Audit       transactionalAudit
	Clock       clock.Clock
	DB          *sql.DB
	Generator   identity.Generator
	Outbox      outbox.Writer
	Transaction transaction.Manager
}

func (store PostgresStore) GetDraft(ctx context.Context, projectID string) (Draft, error) {
	row := store.DB.QueryRowContext(ctx, `SELECT revision,yjs_update,state_vector,tiptap_json,manuscript_markdown,references_bib,manifest,actor_kind,provenance,updated_at FROM article_drafts WHERE project_id=$1`, projectID)
	var draft Draft
	var tiptap, manifest, provenance []byte
	draft.ProjectID = projectID
	if err := row.Scan(&draft.DraftRevision, &draft.YjsUpdate, &draft.StateVector, &tiptap, &draft.Markdown, &draft.ReferencesBIB, &manifest, &draft.ActorKind, &provenance, &draft.UpdatedAt); errors.Is(err, sql.ErrNoRows) {
		return Draft{ProjectID: projectID, DraftRevision: 0, TiptapJSON: map[string]interface{}{"type": "doc", "content": []interface{}{}}, Blocks: []Block{}, SyncStatus: "synced", UpdatedAt: store.now()}, nil
	} else if err != nil {
		return Draft{}, err
	}
	if json.Unmarshal(tiptap, &draft.TiptapJSON) != nil || json.Unmarshal(manifest, &draft.Manifest) != nil || json.Unmarshal(provenance, &draft.Provenance) != nil {
		return Draft{}, ErrInvalid
	}
	draft.Blocks, _ = store.listBlocks(ctx, projectID)
	draft.SyncStatus = "synced"
	return draft, nil
}

func (store PostgresStore) PersistDraft(ctx context.Context, projectID, actorID string, input PersistDraftInput, markdown string, blocks []Block, manifest map[string]interface{}, referencesBIB string) (Draft, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		_, err := store.persistDraftInTransaction(ctx, tx, projectID, actorID, input, markdown, blocks, manifest, referencesBIB)
		return err
	})
	if err != nil {
		return Draft{}, err
	}
	return store.GetDraft(ctx, projectID)
}

func (store PostgresStore) persistDraftInTransaction(ctx context.Context, tx transaction.Tx, projectID, actorID string, input PersistDraftInput, markdown string, blocks []Block, manifest map[string]interface{}, referencesBIB string) (int64, error) {
	now := store.now()
	encodedDocument, _ := json.Marshal(input.TiptapJSON)
	encodedManifest, _ := json.Marshal(manifest)
	encodedProvenance, _ := json.Marshal(input.Provenance)
	var current int64
	err := tx.QueryRowContext(ctx, `SELECT revision FROM article_drafts WHERE project_id=$1 FOR UPDATE`, projectID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		current = 0
	} else if err != nil {
		return 0, err
	}
	if current != input.ExpectedRevision {
		return 0, ErrConflict
	}
	revision := current + 1
	_, err = tx.ExecContext(ctx, `INSERT INTO article_drafts(project_id,revision,yjs_update,state_vector,tiptap_json,manuscript_markdown,references_bib,manifest,actor_kind,provenance,updated_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(project_id) DO UPDATE SET revision=EXCLUDED.revision,yjs_update=EXCLUDED.yjs_update,state_vector=EXCLUDED.state_vector,tiptap_json=EXCLUDED.tiptap_json,manuscript_markdown=EXCLUDED.manuscript_markdown,references_bib=EXCLUDED.references_bib,manifest=EXCLUDED.manifest,actor_kind=EXCLUDED.actor_kind,provenance=EXCLUDED.provenance,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at`, projectID, revision, input.YjsUpdate, input.StateVector, encodedDocument, markdown, referencesBIB, encodedManifest, input.ActorKind, encodedProvenance, actorID, now)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM article_blocks WHERE project_id=$1`, projectID); err != nil {
		return 0, err
	}
	for _, block := range blocks {
		attrs, _ := json.Marshal(block.Attrs)
		if _, err := tx.ExecContext(ctx, `INSERT INTO article_blocks(block_id,project_id,draft_revision,position,block_type,text_content,attributes,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, block.BlockID, projectID, revision, block.Ordinal, block.NodeType, block.Text, attrs, block.UpdatedAt); err != nil {
			return 0, err
		}
	}
	stateVectorDigest := sha256.Sum256([]byte(input.StateVector))
	if err := store.record(ctx, tx, "article.draft.flushed", projectID, actorID, "draft", projectID, map[string]interface{}{"draft_revision": revision, "state_vector_sha256": fmt.Sprintf("%x", stateVectorDigest), "block_count": len(blocks), "status": "synced"}); err != nil {
		return 0, err
	}
	return revision, nil
}

func (store PostgresStore) ReviewBlock(ctx context.Context, projectID, blockID, actorID string) (Block, error) {
	var reviewed Block
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var attrsJSON []byte
		if err := tx.QueryRowContext(ctx, `SELECT block_type,position,text_content,attributes,updated_at FROM article_blocks WHERE project_id=$1 AND block_id=$2 FOR UPDATE`, projectID, blockID).Scan(&reviewed.NodeType, &reviewed.Ordinal, &reviewed.Text, &attrsJSON, &reviewed.UpdatedAt); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		reviewed.BlockID = blockID
		_ = json.Unmarshal(attrsJSON, &reviewed.Attrs)
		now := store.now()
		reviewed.Tag = "reviewed"
		reviewed.UpdatedAt = now
		reviewed.Provenance = object(reviewed.Attrs["provenance"])
		reviewed.Provenance["reviewed_by"] = actorID
		reviewed.Provenance["reviewed_at"] = now.UTC().Format(time.RFC3339Nano)
		reviewed.Attrs["tag"] = reviewed.Tag
		reviewed.Attrs["provenance"] = reviewed.Provenance
		encoded, _ := json.Marshal(reviewed.Attrs)
		if _, err := tx.ExecContext(ctx, `UPDATE article_blocks SET attributes=$3,updated_at=$4 WHERE project_id=$1 AND block_id=$2`, projectID, blockID, encoded, now); err != nil {
			return err
		}
		return store.record(ctx, tx, "article.block.reviewed", projectID, actorID, "article_block", blockID, map[string]interface{}{"block_id": blockID, "status": "reviewed"})
	})
	return reviewed, err
}

func (store PostgresStore) listBlocks(ctx context.Context, projectID string) ([]Block, error) {
	rows, err := store.DB.QueryContext(ctx, `SELECT block_id,block_type,position,text_content,attributes,updated_at FROM article_blocks WHERE project_id=$1 ORDER BY position`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Block{}
	for rows.Next() {
		var item Block
		var attrs []byte
		if err := rows.Scan(&item.BlockID, &item.NodeType, &item.Ordinal, &item.Text, &attrs, &item.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(attrs, &item.Attrs)
		item.Tag, _ = item.Attrs["tag"].(string)
		item.Provenance = object(item.Attrs["provenance"])
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) CreatePatch(ctx context.Context, item Patch) (Patch, error) {
	patchJSON, _ := json.Marshal(item.Patch)
	provenance, _ := json.Marshal(item.Provenance)
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO article_patches(patch_id,project_id,base_revision,status,patch,rationale,provenance,proposed_by,created_at,updated_at) VALUES($1,$2,$3,'proposed',$4,$5,$6,$7,$8,$8)`, item.PatchID, item.ProjectID, item.BaseRevision, patchJSON, item.Rationale, provenance, item.CreatedBy, item.CreatedAt); err != nil {
			return err
		}
		return store.record(ctx, tx, "article.patch.proposed", item.ProjectID, item.CreatedBy, "patch", item.PatchID, map[string]interface{}{"patch_id": item.PatchID, "base_revision": item.BaseRevision, "status": "proposed"})
	})
	if err != nil {
		return Patch{}, err
	}
	return store.getPatch(ctx, item.ProjectID, item.PatchID)
}

func (store PostgresStore) ListPatches(ctx context.Context, projectID, status string) ([]Patch, error) {
	rows, err := store.DB.QueryContext(ctx, `SELECT patch_id,project_id,base_revision,accepted_revision,status,patch,rationale,provenance,proposed_by,COALESCE(reviewed_by,''),created_at,updated_at FROM article_patches WHERE project_id=$1 AND ($2='' OR status=$2) ORDER BY created_at DESC`, projectID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Patch{}
	for rows.Next() {
		item, err := scanPatch(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (store PostgresStore) getPatch(ctx context.Context, projectID, id string) (Patch, error) {
	item, err := scanPatch(store.DB.QueryRowContext(ctx, `SELECT patch_id,project_id,base_revision,accepted_revision,status,patch,rationale,provenance,proposed_by,COALESCE(reviewed_by,''),created_at,updated_at FROM article_patches WHERE project_id=$1 AND patch_id=$2`, projectID, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Patch{}, ErrNotFound
	}
	return item, err
}
func scanPatch(scan func(...interface{}) error) (Patch, error) {
	var item Patch
	var patchJSON, provenance []byte
	var accepted sql.NullInt64
	err := scan(&item.PatchID, &item.ProjectID, &item.BaseRevision, &accepted, &item.Status, &patchJSON, &item.Rationale, &provenance, &item.CreatedBy, &item.ReviewedBy, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return Patch{}, err
	}
	if accepted.Valid {
		item.AcceptedRevision = &accepted.Int64
	}
	_ = json.Unmarshal(patchJSON, &item.Patch)
	_ = json.Unmarshal(provenance, &item.Provenance)
	return item, nil
}

func (store PostgresStore) ReviewPatch(ctx context.Context, projectID, id, decision, actorID string, accepted *int64) (Patch, error) {
	now := store.now()
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE article_patches SET status=$3,accepted_revision=$4,reviewed_by=$5,reviewed_at=$6,updated_at=$6 WHERE project_id=$1 AND patch_id=$2 AND status='proposed'`, projectID, id, decision, accepted, actorID, now)
		if err != nil {
			return err
		}
		count, _ := result.RowsAffected()
		if count != 1 {
			return ErrConflict
		}
		payload := map[string]interface{}{"patch_id": id, "status": decision}
		if accepted != nil {
			payload["accepted_revision"] = *accepted
		}
		return store.record(ctx, tx, "article.patch.reviewed", projectID, actorID, "patch", id, payload)
	})
	if err != nil {
		return Patch{}, err
	}
	return store.getPatch(ctx, projectID, id)
}

func (store PostgresStore) AcceptPatch(ctx context.Context, projectID, id, actorID string, input PersistDraftInput, markdown string, blocks []Block, manifest map[string]interface{}, referencesBIB string) (Patch, error) {
	now := store.now()
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM article_patches WHERE project_id=$1 AND patch_id=$2 FOR UPDATE`, projectID, id).Scan(&status); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		} else if status != "proposed" {
			return ErrConflict
		}
		revision, err := store.persistDraftInTransaction(ctx, tx, projectID, actorID, input, markdown, blocks, manifest, referencesBIB)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE article_patches SET status='accepted',accepted_revision=$3,reviewed_by=$4,reviewed_at=$5,updated_at=$5 WHERE project_id=$1 AND patch_id=$2`, projectID, id, revision, actorID, now); err != nil {
			return err
		}
		return store.record(ctx, tx, "article.patch.reviewed", projectID, actorID, "patch", id, map[string]interface{}{"patch_id": id, "status": "accepted", "accepted_revision": revision})
	})
	if err != nil {
		return Patch{}, err
	}
	return store.getPatch(ctx, projectID, id)
}

func (store PostgresStore) CreateReference(ctx context.Context, item Reference) (Reference, bool, error) {
	metadata, _ := json.Marshal(item.Metadata)
	created := false
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO article_references(reference_id,project_id,reference_type,source_object_id,source_version_id,title,citation_key,metadata,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10) ON CONFLICT(project_id,reference_type,source_object_id,source_version_id) DO NOTHING`, item.ReferenceID, item.ProjectID, item.ReferenceType, item.SourceObjectID, item.SourceVersionID, item.Title, item.CitationKey, metadata, item.CreatedBy, item.CreatedAt)
		if err != nil {
			return err
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			return nil
		}
		created = true
		return store.auditOnly(ctx, tx, "article.reference.created", item.ProjectID, item.CreatedBy, "article_reference", item.ReferenceID, map[string]interface{}{"reference_type": item.ReferenceType, "source_object_id": item.SourceObjectID, "source_version_id": item.SourceVersionID})
	})
	if err != nil {
		return Reference{}, false, err
	}
	if !created {
		existing, err := store.getReferenceBySource(ctx, item)
		return existing, false, err
	}
	return item, true, nil
}
func (store PostgresStore) getReferenceBySource(ctx context.Context, item Reference) (Reference, error) {
	return scanReference(store.DB.QueryRowContext(ctx, referenceSelect+` WHERE project_id=$1 AND reference_type=$2 AND source_object_id=$3 AND source_version_id=$4`, item.ProjectID, item.ReferenceType, item.SourceObjectID, item.SourceVersionID).Scan)
}
func (store PostgresStore) ListReferences(ctx context.Context, projectID string) ([]Reference, error) {
	rows, err := store.DB.QueryContext(ctx, referenceSelect+` WHERE project_id=$1 ORDER BY created_at,reference_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Reference{}
	for rows.Next() {
		item, err := scanReference(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const referenceSelect = `SELECT reference_id,project_id,reference_type,source_object_id,source_version_id,title,COALESCE(citation_key,''),metadata,created_by,created_at FROM article_references`

func scanReference(scan func(...interface{}) error) (Reference, error) {
	var item Reference
	var metadata []byte
	if err := scan(&item.ReferenceID, &item.ProjectID, &item.ReferenceType, &item.SourceObjectID, &item.SourceVersionID, &item.Title, &item.CitationKey, &metadata, &item.CreatedBy, &item.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Reference{}, ErrNotFound
		}
		return Reference{}, err
	}
	_ = json.Unmarshal(metadata, &item.Metadata)
	return item, nil
}
func (store PostgresStore) DeleteReference(ctx context.Context, projectID, id, actorID string) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM article_references WHERE project_id=$1 AND reference_id=$2`, projectID, id)
		if err != nil {
			return err
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			return ErrNotFound
		}
		return store.auditOnly(ctx, tx, "article.reference.deleted", projectID, actorID, "article_reference", id, nil)
	})
}

func (store PostgresStore) CreateCommit(ctx context.Context, item Commit) (Commit, bool, error) {
	frozen, _ := json.Marshal(item.FrozenReferences)
	tiptap, _ := json.Marshal(item.TiptapJSON)
	created := false
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO article_commits(commit_id,project_id,draft_revision,state_vector,yjs_update,tiptap_json,git_commit_sha,previous_git_commit_sha,message,manuscript_sha256,references_sha256,manifest_sha256,frozen_references,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT(project_id,git_commit_sha) DO NOTHING`, item.CommitID, item.ProjectID, item.DraftRevision, item.StateVector, item.YjsUpdate, tiptap, item.CommitSHA, item.PreviousCommitSHA, item.Message, item.ManuscriptSHA256, item.ReferencesSHA256, item.ManifestSHA256, frozen, item.CreatedBy, item.CreatedAt)
		if err != nil {
			return err
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			return nil
		}
		created = true
		return store.record(ctx, tx, "article.commit.created", item.ProjectID, item.CreatedBy, "article_commit", item.CommitID, map[string]interface{}{
			"commit_id": item.CommitID, "commit_sha": item.CommitSHA, "draft_revision": item.DraftRevision,
			"manuscript_sha256": item.ManuscriptSHA256, "status": "committed",
		})
	})
	if err != nil {
		return Commit{}, false, err
	}
	if !created {
		existing, err := store.getCommitBySHA(ctx, item.ProjectID, item.CommitSHA)
		return existing, false, err
	}
	return item, true, nil
}
func (store PostgresStore) GetCommit(ctx context.Context, projectID, id string) (Commit, error) {
	return scanCommit(store.DB.QueryRowContext(ctx, commitSelect+` WHERE c.project_id=$1 AND c.commit_id=$2`, projectID, id).Scan)
}
func (store PostgresStore) getCommitBySHA(ctx context.Context, projectID, sha string) (Commit, error) {
	return scanCommit(store.DB.QueryRowContext(ctx, commitSelect+` WHERE c.project_id=$1 AND c.git_commit_sha=$2`, projectID, sha).Scan)
}
func (store PostgresStore) ListCommits(ctx context.Context, projectID string) ([]Commit, error) {
	rows, err := store.DB.QueryContext(ctx, commitSelect+` WHERE c.project_id=$1 ORDER BY c.created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Commit{}
	for rows.Next() {
		item, err := scanCommit(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const commitSelect = `SELECT c.commit_id,c.project_id,c.git_commit_sha,c.draft_revision,c.state_vector,c.manuscript_sha256,c.message,c.created_by,c.created_at,c.previous_git_commit_sha,c.references_sha256,c.manifest_sha256,c.frozen_references,c.yjs_update,c.tiptap_json FROM article_commits c`

func scanCommit(scan func(...interface{}) error) (Commit, error) {
	var item Commit
	var frozen, tiptap []byte
	if err := scan(&item.CommitID, &item.ProjectID, &item.CommitSHA, &item.DraftRevision, &item.StateVector, &item.ManuscriptSHA256, &item.Message, &item.CreatedBy, &item.CreatedAt, &item.PreviousCommitSHA, &item.ReferencesSHA256, &item.ManifestSHA256, &frozen, &item.YjsUpdate, &tiptap); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Commit{}, ErrNotFound
		}
		return Commit{}, err
	}
	_ = json.Unmarshal(frozen, &item.FrozenReferences)
	_ = json.Unmarshal(tiptap, &item.TiptapJSON)
	return item, nil
}

func (store PostgresStore) CreateTemplate(ctx context.Context, item Template) (Template, bool, error) {
	manifest, _ := json.Marshal(item.Manifest)
	created := false
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO article_templates(template_id,project_id,artifact_id,artifact_version_id,name,template_version,manifest,status,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10) ON CONFLICT(project_id,artifact_version_id) DO NOTHING`, item.TemplateID, item.ProjectID, item.ArtifactID, item.VersionID, item.Manifest.Name, item.Manifest.Version, manifest, item.Status, item.CreatedBy, item.CreatedAt)
		if err != nil {
			return err
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			return nil
		}
		created = true
		return store.auditOnly(ctx, tx, "article.template.registered", item.ProjectID, item.CreatedBy, "article_template", item.TemplateID, map[string]interface{}{"artifact_id": item.ArtifactID, "version_id": item.VersionID})
	})
	if err != nil {
		return Template{}, false, err
	}
	if !created {
		existing, err := store.getTemplateByVersion(ctx, item.ProjectID, item.VersionID)
		return existing, false, err
	}
	return item, true, nil
}
func (store PostgresStore) GetTemplate(ctx context.Context, projectID, id string) (Template, error) {
	return scanTemplate(store.DB.QueryRowContext(ctx, templateSelect+` WHERE project_id=$1 AND template_id=$2`, projectID, id).Scan)
}
func (store PostgresStore) getTemplateByVersion(ctx context.Context, projectID, id string) (Template, error) {
	return scanTemplate(store.DB.QueryRowContext(ctx, templateSelect+` WHERE project_id=$1 AND artifact_version_id=$2`, projectID, id).Scan)
}
func (store PostgresStore) ListTemplates(ctx context.Context, projectID string) ([]Template, error) {
	rows, err := store.DB.QueryContext(ctx, templateSelect+` WHERE project_id=$1 ORDER BY updated_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Template{}
	for rows.Next() {
		item, err := scanTemplate(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const templateSelect = `SELECT template_id,project_id,artifact_id,artifact_version_id,manifest,status,COALESCE((CASE WHEN status='rejected' THEN 'TEMPLATE_TEST_FAILED' ELSE '' END),''),created_by,created_at,updated_at,COALESCE(test_build_id::text,'') FROM article_templates`

func scanTemplate(scan func(...interface{}) error) (Template, error) {
	var item Template
	var manifest []byte
	if err := scan(&item.TemplateID, &item.ProjectID, &item.ArtifactID, &item.VersionID, &manifest, &item.Status, &item.ErrorCode, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt, &item.TestBuildID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Template{}, ErrNotFound
		}
		return Template{}, err
	}
	_ = json.Unmarshal(manifest, &item.Manifest)
	return item, nil
}

func (store PostgresStore) CreateBuild(ctx context.Context, item Build, jobInput jobs.CreateInput, writer jobs.TransactionalWriter) (Build, bool, error) {
	var created bool
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var err error
		created, err = store.createBuildInTransaction(ctx, tx, item, jobInput, writer)
		return err
	})
	if err != nil {
		return Build{}, false, err
	}
	if !created && item.IdempotencyKey != "" {
		existing, err := store.getBuildByIdempotency(ctx, item.ProjectID, item.BuildKind, item.IdempotencyKey)
		return existing, false, err
	}
	createdBuild, err := store.GetBuild(ctx, item.ProjectID, item.BuildID)
	return createdBuild, true, err
}

func (store PostgresStore) createBuildInTransaction(ctx context.Context, tx transaction.Tx, item Build, jobInput jobs.CreateInput, writer jobs.TransactionalWriter) (bool, error) {
	if item.BuildKind == BuildPreview {
		if _, err := tx.ExecContext(ctx, `UPDATE article_builds SET status='superseded',finished_at=$2,updated_at=$2 WHERE project_id=$1 AND build_kind='preview' AND status IN ('queued','running')`, item.ProjectID, item.CreatedAt); err != nil {
			return false, err
		}
	}
	draftRevision := interface{}(nil)
	if item.DraftRevision != nil {
		draftRevision = *item.DraftRevision
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO article_builds(build_id,project_id,build_kind,status,commit_id,draft_revision,template_id,template_version_id,engine,bibliography_tool,idempotency_key,created_by,created_at,updated_at) VALUES($1,$2,$3,'queued',NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,NULLIF($10,''),$11,$12,$12) ON CONFLICT(project_id,build_kind,idempotency_key) DO NOTHING`, item.BuildID, item.ProjectID, item.BuildKind, item.CommitID, draftRevision, item.TemplateID, item.TemplateVersionID, item.Engine, item.BibliographyTool, item.IdempotencyKey, item.CreatedBy, item.CreatedAt)
	if err != nil {
		return false, err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return false, nil
	}
	job, _, err := writer.CreateInTransaction(ctx, tx, item.CreatedBy, jobInput)
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE article_builds SET job_id=$2 WHERE build_id=$1`, item.BuildID, job.ID); err != nil {
		return false, err
	}
	payload := map[string]interface{}{"build_id": item.BuildID, "build_kind": item.BuildKind, "job_id": job.ID, "template_version_id": item.TemplateVersionID, "status": "queued"}
	if item.CommitID != "" {
		payload["commit_id"] = item.CommitID
	}
	if item.DraftRevision != nil && *item.DraftRevision > 0 {
		payload["draft_revision"] = *item.DraftRevision
	}
	if err = store.record(ctx, tx, "article.build.queued", item.ProjectID, item.CreatedBy, "article_build", item.BuildID, payload); err != nil {
		return false, err
	}
	return true, nil
}

func (store PostgresStore) GetBuild(ctx context.Context, projectID, id string) (Build, error) {
	item, err := scanBuild(store.DB.QueryRowContext(ctx, buildSelect+` WHERE b.project_id=$1 AND b.build_id=$2`, projectID, id).Scan)
	if err != nil {
		return Build{}, err
	}
	item.Outputs, _ = store.listOutputs(ctx, id)
	return item, nil
}
func (store PostgresStore) getBuildByIdempotency(ctx context.Context, projectID, kind, key string) (Build, error) {
	item, err := scanBuild(store.DB.QueryRowContext(ctx, buildSelect+` WHERE b.project_id=$1 AND b.build_kind=$2 AND b.idempotency_key=$3`, projectID, kind, key).Scan)
	if err != nil {
		return Build{}, err
	}
	item.Outputs, _ = store.listOutputs(ctx, item.BuildID)
	return item, nil
}
func (store PostgresStore) GetBuildByJob(ctx context.Context, tx transaction.Tx, jobID string) (Build, error) {
	return scanBuild(tx.QueryRowContext(ctx, buildSelect+` WHERE b.job_id=$1`, jobID).Scan)
}
func (store PostgresStore) ListBuilds(ctx context.Context, projectID, commitID string) ([]Build, error) {
	rows, err := store.DB.QueryContext(ctx, buildSelect+` WHERE b.project_id=$1 AND ($2='' OR b.commit_id::text=$2) ORDER BY b.created_at DESC`, projectID, commitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Build{}
	for rows.Next() {
		item, err := scanBuild(rows.Scan)
		if err != nil {
			return nil, err
		}
		item.Outputs, _ = store.listOutputs(ctx, item.BuildID)
		items = append(items, item)
	}
	return items, rows.Err()
}

const buildSelect = `SELECT b.build_id,b.project_id,b.build_kind,b.status,COALESCE(b.commit_id::text,''),c.git_commit_sha,b.draft_revision,COALESCE(b.job_id::text,''),b.template_id,t.artifact_id,b.template_version_id,b.engine,b.bibliography_tool,b.toolchain,COALESCE(b.error_code,''),COALESCE(b.error_message,''),b.created_by,b.created_at,b.updated_at,b.finished_at,COALESCE(b.idempotency_key,'') FROM article_builds b JOIN article_templates t ON t.template_id=b.template_id LEFT JOIN article_commits c ON c.commit_id=b.commit_id`

func scanBuild(scan func(...interface{}) error) (Build, error) {
	var item Build
	var draft sql.NullInt64
	var toolchain []byte
	if err := scan(&item.BuildID, &item.ProjectID, &item.BuildKind, &item.Status, &item.CommitID, &item.CommitSHA, &draft, &item.JobID, &item.TemplateID, &item.TemplateArtifactID, &item.TemplateVersionID, &item.Engine, &item.BibliographyTool, &toolchain, &item.ErrorCode, &item.ErrorMessage, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt, &item.FinishedAt, &item.IdempotencyKey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Build{}, ErrNotFound
		}
		return Build{}, err
	}
	if draft.Valid {
		item.DraftRevision = &draft.Int64
	}
	_ = json.Unmarshal(toolchain, &item.Toolchain)
	if item.Toolchain == nil {
		item.Toolchain = map[string]interface{}{}
	}
	item.Outputs = []BuildOutput{}
	return item, nil
}
func (store PostgresStore) listOutputs(ctx context.Context, buildID string) ([]BuildOutput, error) {
	rows, err := store.DB.QueryContext(ctx, `SELECT output_role,artifact_id,artifact_version_id,filename,mime_type,sha256,size_bytes FROM article_build_outputs WHERE build_id=$1 ORDER BY output_role`, buildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BuildOutput{}
	for rows.Next() {
		var item BuildOutput
		if err := rows.Scan(&item.Role, &item.ArtifactID, &item.VersionID, &item.Filename, &item.MIMEType, &item.SHA256, &item.SizeBytes); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) MarkBuildRunning(ctx context.Context, tx transaction.Tx, jobID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE article_builds SET status=CASE WHEN status='queued' THEN 'running' ELSE status END,started_at=COALESCE(started_at,$2),updated_at=$2 WHERE job_id=$1`, jobID, store.now())
	return err
}
func (store PostgresStore) CompleteBuild(ctx context.Context, tx transaction.Tx, jobID string, result map[string]interface{}) (Build, error) {
	item, err := store.GetBuildByJob(ctx, tx, jobID)
	if err != nil {
		return Build{}, err
	}
	if item.Status == BuildSuperseded {
		return item, nil
	}
	toolchain, _ := json.Marshal(result["toolchain"])
	now := store.now()
	if _, err = tx.ExecContext(ctx, `UPDATE article_builds SET status='succeeded',toolchain=$2,error_code=NULL,error_message=NULL,finished_at=$3,updated_at=$3 WHERE build_id=$1 AND status IN ('queued','running')`, item.BuildID, toolchain, now); err != nil {
		return Build{}, err
	}
	if item.BuildKind == BuildTemplateTest {
		_, err = tx.ExecContext(ctx, `UPDATE article_templates SET status='ready',updated_at=$2 WHERE template_id=$1`, item.TemplateID, now)
		if err != nil {
			return Build{}, err
		}
	}
	var outputCount int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM article_build_outputs WHERE build_id=$1`, item.BuildID).Scan(&outputCount); err != nil {
		return Build{}, err
	}
	payload := map[string]interface{}{"build_id": item.BuildID, "build_kind": item.BuildKind, "template_version_id": item.TemplateVersionID, "output_count": outputCount, "status": "succeeded"}
	if item.CommitID != "" {
		payload["commit_id"] = item.CommitID
	}
	if item.DraftRevision != nil && *item.DraftRevision > 0 {
		payload["draft_revision"] = *item.DraftRevision
	}
	if err = store.record(ctx, tx, "article.build.completed", item.ProjectID, item.CreatedBy, "article_build", item.BuildID, payload); err != nil {
		return Build{}, err
	}
	if err = store.completePublicationForBuild(ctx, tx, item, now); err != nil {
		return Build{}, err
	}
	item.Status = BuildSucceeded
	item.UpdatedAt = now
	item.FinishedAt = &now
	return item, nil
}
func (store PostgresStore) FailBuild(ctx context.Context, tx transaction.Tx, jobID, code, message string) (Build, error) {
	item, err := store.GetBuildByJob(ctx, tx, jobID)
	if err != nil {
		return Build{}, err
	}
	if item.Status == BuildSuperseded {
		return item, nil
	}
	now := store.now()
	if _, err = tx.ExecContext(ctx, `UPDATE article_builds SET status='failed',error_code=$2,error_message=$3,finished_at=$4,updated_at=$4 WHERE build_id=$1 AND status IN ('queued','running')`, item.BuildID, code, message, now); err != nil {
		return Build{}, err
	}
	if item.BuildKind == BuildTemplateTest {
		_, _ = tx.ExecContext(ctx, `UPDATE article_templates SET status='rejected',updated_at=$2 WHERE template_id=$1`, item.TemplateID, now)
	}
	_ = store.FailPublication(ctx, tx, item.BuildID, code)
	payload := map[string]interface{}{"build_id": item.BuildID, "build_kind": item.BuildKind, "error_code": code, "status": "failed"}
	if item.CommitID != "" {
		payload["commit_id"] = item.CommitID
	}
	if item.DraftRevision != nil && *item.DraftRevision > 0 {
		payload["draft_revision"] = *item.DraftRevision
	}
	if err = store.record(ctx, tx, "article.build.failed", item.ProjectID, item.CreatedBy, "article_build", item.BuildID, payload); err != nil {
		return Build{}, err
	}
	item.Status = BuildFailed
	item.ErrorCode = code
	item.ErrorMessage = message
	item.UpdatedAt = now
	item.FinishedAt = &now
	return item, nil
}
func (store PostgresStore) AddBuildOutput(ctx context.Context, buildID string, item BuildOutput) error {
	_, err := store.DB.ExecContext(ctx, `INSERT INTO article_build_outputs(build_id,output_role,artifact_id,artifact_version_id,filename,mime_type,sha256,size_bytes,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(build_id,output_role) DO UPDATE SET artifact_id=EXCLUDED.artifact_id,artifact_version_id=EXCLUDED.artifact_version_id,filename=EXCLUDED.filename,mime_type=EXCLUDED.mime_type,sha256=EXCLUDED.sha256,size_bytes=EXCLUDED.size_bytes,created_at=EXCLUDED.created_at`, buildID, item.Role, item.ArtifactID, item.VersionID, item.Filename, item.MIMEType, item.SHA256, item.SizeBytes, store.now())
	return err
}

func (store PostgresStore) CreateRelease(ctx context.Context, item Release) (Release, bool, error) {
	outputs, _ := json.Marshal(item.Outputs)
	created := false
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO article_releases(release_id,project_id,commit_id,build_id,template_id,template_version_id,tag,title,notes,output_versions,created_by,created_at) SELECT $1,$2,$3,$4,b.template_id,b.template_version_id,$5,$6,$7,$8,$9,$10 FROM article_builds b WHERE b.build_id=$4 AND b.project_id=$2 AND b.commit_id=$3 AND b.build_kind='formal' AND b.status='succeeded' ON CONFLICT(project_id,tag) DO NOTHING`, item.ReleaseID, item.ProjectID, item.CommitID, item.BuildID, item.Tag, item.Title, item.Notes, outputs, item.CreatedBy, item.CreatedAt)
		if err != nil {
			return err
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			return nil
		}
		created = true
		return store.record(ctx, tx, "article.release.created", item.ProjectID, item.CreatedBy, "article_release", item.ReleaseID, map[string]interface{}{
			"release_id": item.ReleaseID, "commit_id": item.CommitID, "build_id": item.BuildID,
			"tag": item.Tag, "title": item.Title, "status": "released",
		})
	})
	if err != nil {
		return Release{}, false, err
	}
	if !created {
		existing, err := store.getReleaseByTag(ctx, item.ProjectID, item.Tag)
		return existing, false, err
	}
	createdRelease, err := store.GetRelease(ctx, item.ProjectID, item.ReleaseID)
	return createdRelease, true, err
}
func (store PostgresStore) GetRelease(ctx context.Context, projectID, id string) (Release, error) {
	return scanRelease(store.DB.QueryRowContext(ctx, releaseSelect+` WHERE r.project_id=$1 AND r.release_id=$2`, projectID, id).Scan)
}
func (store PostgresStore) getReleaseByTag(ctx context.Context, projectID, tag string) (Release, error) {
	return scanRelease(store.DB.QueryRowContext(ctx, releaseSelect+` WHERE r.project_id=$1 AND r.tag=$2`, projectID, tag).Scan)
}
func (store PostgresStore) ListReleases(ctx context.Context, projectID string) ([]Release, error) {
	rows, err := store.DB.QueryContext(ctx, releaseSelect+` WHERE r.project_id=$1 ORDER BY r.created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Release{}
	for rows.Next() {
		item, err := scanRelease(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const releaseSelect = `SELECT r.release_id,r.project_id,r.commit_id,c.git_commit_sha,r.build_id,r.tag,r.title,r.notes,r.template_version_id,b.engine,b.toolchain,r.output_versions,r.created_by,r.created_at FROM article_releases r JOIN article_commits c ON c.commit_id=r.commit_id JOIN article_builds b ON b.build_id=r.build_id`

func scanRelease(scan func(...interface{}) error) (Release, error) {
	var item Release
	var toolchain, outputs []byte
	if err := scan(&item.ReleaseID, &item.ProjectID, &item.CommitID, &item.CommitSHA, &item.BuildID, &item.Tag, &item.Title, &item.Notes, &item.TemplateVersionID, &item.Engine, &toolchain, &outputs, &item.CreatedBy, &item.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Release{}, ErrNotFound
		}
		return Release{}, err
	}
	_ = json.Unmarshal(toolchain, &item.Toolchain)
	_ = json.Unmarshal(outputs, &item.Outputs)
	return item, nil
}

func (store PostgresStore) CreatePublication(ctx context.Context, item Publication) (Publication, bool, error) {
	result, err := store.DB.ExecContext(ctx, `INSERT INTO article_publications(publication_id,project_id,idempotency_key,status,commit_id,build_id,tag,title,notes,created_by,created_at,updated_at) VALUES($1,$2,$3,'building',$4,$5,$6,$7,$8,$9,$10,$10) ON CONFLICT(project_id,idempotency_key) DO NOTHING`, item.PublicationID, item.ProjectID, item.IdempotencyKey, item.CommitID, item.BuildID, item.Tag, item.Title, item.Notes, item.CreatedBy, item.CreatedAt)
	if err != nil {
		return Publication{}, false, err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		existing, err := store.GetPublication(ctx, item.ProjectID, item.IdempotencyKey)
		return existing, false, err
	}
	created, err := store.GetPublication(ctx, item.ProjectID, item.PublicationID)
	return created, true, err
}

func (store PostgresStore) CreatePublicationBuild(ctx context.Context, publication Publication, build Build, jobInput jobs.CreateInput, writer jobs.TransactionalWriter) (Publication, bool, error) {
	created := false
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var existingID string
		err := tx.QueryRowContext(ctx, `SELECT publication_id FROM article_publications WHERE project_id=$1 AND idempotency_key=$2 FOR UPDATE`, publication.ProjectID, publication.IdempotencyKey).Scan(&existingID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		buildCreated, err := store.createBuildInTransaction(ctx, tx, build, jobInput, writer)
		if err != nil {
			return err
		}
		if !buildCreated {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO article_publications(publication_id,project_id,idempotency_key,status,commit_id,build_id,tag,title,notes,created_by,created_at,updated_at) VALUES($1,$2,$3,'building',$4,$5,$6,$7,$8,$9,$10,$10)`, publication.PublicationID, publication.ProjectID, publication.IdempotencyKey, publication.CommitID, build.BuildID, publication.Tag, publication.Title, publication.Notes, publication.CreatedBy, publication.CreatedAt)
		if err != nil {
			return err
		}
		created = true
		return store.auditOnly(ctx, tx, "article.publication.requested", publication.ProjectID, publication.CreatedBy, "article_publication", publication.PublicationID, map[string]interface{}{"commit_id": publication.CommitID, "build_id": build.BuildID, "tag": publication.Tag})
	})
	if err != nil {
		return Publication{}, false, err
	}
	if !created {
		existing, err := store.GetPublication(ctx, publication.ProjectID, publication.IdempotencyKey)
		return existing, false, err
	}
	item, err := store.GetPublication(ctx, publication.ProjectID, publication.PublicationID)
	return item, true, err
}

func (store PostgresStore) RetryPublicationBuild(ctx context.Context, publication Publication, previousBuildID string, build Build, jobInput jobs.CreateInput, writer jobs.TransactionalWriter) (Publication, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var status, currentBuildID string
		if err := tx.QueryRowContext(ctx, `SELECT status,build_id FROM article_publications WHERE project_id=$1 AND publication_id=$2 FOR UPDATE`, publication.ProjectID, publication.PublicationID).Scan(&status, &currentBuildID); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if status != "failed" || currentBuildID != previousBuildID {
			return ErrConflict
		}
		created, err := store.createBuildInTransaction(ctx, tx, build, jobInput, writer)
		if err != nil {
			return err
		}
		if !created {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE article_publications SET status='building',build_id=$3,release_id=NULL,error_code=NULL,updated_at=$4 WHERE project_id=$1 AND publication_id=$2`, publication.ProjectID, publication.PublicationID, build.BuildID, build.CreatedAt)
		if err != nil {
			return err
		}
		return store.auditOnly(ctx, tx, "article.publication.retried", publication.ProjectID, build.CreatedBy, "article_publication", publication.PublicationID, map[string]interface{}{"previous_build_id": previousBuildID, "build_id": build.BuildID})
	})
	if err != nil {
		return Publication{}, err
	}
	return store.GetPublication(ctx, publication.ProjectID, publication.PublicationID)
}
func (store PostgresStore) GetPublication(ctx context.Context, projectID, id string) (Publication, error) {
	return scanPublication(store.DB.QueryRowContext(ctx, publicationSelect+` WHERE project_id=$1 AND (publication_id::text=$2 OR idempotency_key=$2)`, projectID, id).Scan)
}
func (store PostgresStore) GetPublicationByBuild(ctx context.Context, tx transaction.Tx, buildID string) (Publication, error) {
	return scanPublication(tx.QueryRowContext(ctx, publicationSelect+` WHERE build_id=$1`, buildID).Scan)
}

const publicationSelect = `SELECT publication_id,project_id,commit_id,build_id,COALESCE(release_id::text,''),status,tag,title,notes,COALESCE(error_code,''),created_by,created_at,updated_at FROM article_publications`

func scanPublication(scan func(...interface{}) error) (Publication, error) {
	var item Publication
	if err := scan(&item.PublicationID, &item.ProjectID, &item.CommitID, &item.BuildID, &item.ReleaseID, &item.Status, &item.Tag, &item.Title, &item.Notes, &item.ErrorCode, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Publication{}, ErrNotFound
		}
		return Publication{}, err
	}
	return item, nil
}
func (store PostgresStore) CompletePublication(ctx context.Context, tx transaction.Tx, buildID, releaseID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE article_publications SET status='released',release_id=$2,error_code=NULL,updated_at=$3 WHERE build_id=$1`, buildID, releaseID, store.now())
	return err
}
func (store PostgresStore) FailPublication(ctx context.Context, tx transaction.Tx, buildID, code string) error {
	_, err := tx.ExecContext(ctx, `UPDATE article_publications SET status='failed',error_code=$2,updated_at=$3 WHERE build_id=$1 AND status='building'`, buildID, code, store.now())
	return err
}
func (store PostgresStore) completePublicationForBuild(ctx context.Context, tx transaction.Tx, build Build, now time.Time) error {
	publication, err := store.GetPublicationByBuild(ctx, tx, build.BuildID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	outputsRows, err := tx.QueryContext(ctx, `SELECT output_role,artifact_id,artifact_version_id,filename,mime_type,sha256,size_bytes FROM article_build_outputs WHERE build_id=$1 ORDER BY output_role`, build.BuildID)
	if err != nil {
		return err
	}
	outputs := []BuildOutput{}
	for outputsRows.Next() {
		var output BuildOutput
		if err := outputsRows.Scan(&output.Role, &output.ArtifactID, &output.VersionID, &output.Filename, &output.MIMEType, &output.SHA256, &output.SizeBytes); err != nil {
			_ = outputsRows.Close()
			return err
		}
		outputs = append(outputs, output)
	}
	_ = outputsRows.Close()
	releaseID, err := store.Generator.New()
	if err != nil {
		return err
	}
	encoded, _ := json.Marshal(outputs)
	result, err := tx.ExecContext(ctx, `INSERT INTO article_releases(release_id,project_id,commit_id,build_id,template_id,template_version_id,tag,title,notes,output_versions,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(project_id,tag) DO NOTHING`, releaseID, build.ProjectID, build.CommitID, build.BuildID, build.TemplateID, build.TemplateVersionID, publication.Tag, publication.Title, publication.Notes, encoded, publication.CreatedBy, now)
	if err != nil {
		return err
	}
	created := true
	if count, _ := result.RowsAffected(); count == 0 {
		created = false
		var existingBuildID string
		if err = tx.QueryRowContext(ctx, `SELECT release_id,build_id FROM article_releases WHERE project_id=$1 AND tag=$2`, build.ProjectID, publication.Tag).Scan(&releaseID, &existingBuildID); errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		} else if err != nil {
			return err
		}
		if existingBuildID != build.BuildID {
			return ErrConflict
		}
	}
	if err = store.CompletePublication(ctx, tx, build.BuildID, releaseID); err != nil {
		return err
	}
	if !created {
		return nil
	}
	return store.record(ctx, tx, "article.release.created", build.ProjectID, publication.CreatedBy, "article_release", releaseID, map[string]interface{}{"release_id": releaseID, "build_id": build.BuildID, "commit_id": build.CommitID, "tag": publication.Tag, "title": publication.Title, "status": "released"})
}

func (store PostgresStore) UpsertZoteroBinding(ctx context.Context, item ZoteroBinding, actorID string) (ZoteroBinding, error) {
	_, err := store.DB.ExecContext(ctx, `INSERT INTO article_zotero_bindings(project_id,library_type,library_id,collection_key,secret_setting_key,updated_by,updated_at) VALUES($1,$2,$3,NULLIF($4,''),'article.zotero',$5,$6) ON CONFLICT(project_id) DO UPDATE SET library_type=EXCLUDED.library_type,library_id=EXCLUDED.library_id,collection_key=EXCLUDED.collection_key,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at`, item.ProjectID, item.LibraryType, item.LibraryID, item.CollectionKey, actorID, store.now())
	if err != nil {
		return ZoteroBinding{}, err
	}
	return store.GetZoteroBinding(ctx, item.ProjectID)
}
func (store PostgresStore) GetZoteroBinding(ctx context.Context, projectID string) (ZoteroBinding, error) {
	var item ZoteroBinding
	item.ReadOnly = true
	item.APIKeyConfigured = true
	if err := store.DB.QueryRowContext(ctx, `SELECT project_id,library_type,library_id,COALESCE(collection_key,'') FROM article_zotero_bindings WHERE project_id=$1`, projectID).Scan(&item.ProjectID, &item.LibraryType, &item.LibraryID, &item.CollectionKey); errors.Is(err, sql.ErrNoRows) {
		return ZoteroBinding{}, ErrNotFound
	} else if err != nil {
		return ZoteroBinding{}, err
	}
	return item, nil
}
func (store PostgresStore) DeleteZoteroBinding(ctx context.Context, projectID string) error {
	_, err := store.DB.ExecContext(ctx, `DELETE FROM article_zotero_bindings WHERE project_id=$1`, projectID)
	return err
}

func (store PostgresStore) record(ctx context.Context, tx transaction.Tx, eventType, projectID, actorID, resourceType, resourceID string, payload map[string]interface{}) error {
	if err := store.auditOnly(ctx, tx, eventType, projectID, actorID, resourceType, resourceID, payload); err != nil {
		return err
	}
	_, err := store.Outbox.Write(ctx, tx, outbox.Event{Actor: map[string]string{"actor_id": actorID}, EventType: eventType, Payload: payload, Producer: "article", ProjectID: projectID})
	return err
}

func (store PostgresStore) auditOnly(ctx context.Context, tx transaction.Tx, action, projectID, actorID, resourceType, resourceID string, metadata map[string]interface{}) error {
	if store.Audit == nil {
		return nil
	}
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	return store.Audit.RecordInTransaction(ctx, tx, audit.Event{Action: action, ActorID: actorID, ActorKind: "user", Category: "article", Metadata: metadata, Outcome: "success", ProjectID: projectID, ResourceID: resourceID, ResourceType: resourceType, Source: "core"})
}
func (store PostgresStore) now() time.Time {
	if store.Clock == nil {
		return time.Now().UTC()
	}
	return store.Clock.Now().UTC()
}

var _ Store = PostgresStore{}
var _ = fmt.Sprintf
