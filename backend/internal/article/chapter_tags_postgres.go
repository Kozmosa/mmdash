package article

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func scanChapterTag(scan func(...interface{}) error) (ChapterTag, error) {
	var item ChapterTag
	var reviewedBy sql.NullString
	var reviewedAt sql.NullTime
	if err := scan(
		&item.ChapterTagID, &item.ProjectID, &item.HeadingBlockID,
		&item.Status, &item.HeadingBlockType, &item.HeadingFingerprint,
		&item.StaleReason, &item.UpdatedBy, &item.UpdatedAt,
		&reviewedBy, &reviewedAt,
	); err != nil {
		return ChapterTag{}, err
	}
	if reviewedBy.Valid {
		item.ReviewedBy = &reviewedBy.String
	}
	if reviewedAt.Valid {
		value := reviewedAt.Time
		item.ReviewedAt = &value
	}
	return item, nil
}

const chapterTagColumns = `chapter_tag_id,project_id,heading_block_id,status,heading_block_type,heading_fingerprint,COALESCE(stale_reason,''),updated_by,updated_at,reviewed_by,reviewed_at`

func (store PostgresStore) CreateChapterTag(ctx context.Context, item ChapterTag) (ChapterTag, bool, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO article_chapter_tags(chapter_tag_id,project_id,heading_block_id,status,heading_block_type,heading_fingerprint,updated_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(project_id,heading_block_id) DO NOTHING`, item.ChapterTagID, item.ProjectID, item.HeadingBlockID, item.Status, item.HeadingBlockType, item.HeadingFingerprint, item.UpdatedBy, item.UpdatedAt)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
		return store.record(ctx, tx, "article.chapter.created", item.ProjectID, item.UpdatedBy, "article_chapter_tag", item.ChapterTagID, map[string]interface{}{"chapter_tag_id": item.ChapterTagID, "heading_block_id": item.HeadingBlockID, "status": item.Status})
	})
	if err != nil {
		return ChapterTag{}, false, err
	}
	value, err := store.GetChapterTag(ctx, item.ProjectID, item.ChapterTagID)
	if err != nil {
		// A concurrent creator owns the unique heading binding. Read it by
		// binding so callers still receive the authoritative record.
		value, err = store.getChapterTagByHeading(ctx, item.ProjectID, item.HeadingBlockID)
	}
	return value, value.ChapterTagID == item.ChapterTagID, err
}

func (store PostgresStore) GetChapterTag(ctx context.Context, projectID, chapterTagID string) (ChapterTag, error) {
	item, err := scanChapterTag(store.DB.QueryRowContext(ctx, `SELECT `+chapterTagColumns+` FROM article_chapter_tags WHERE project_id=$1 AND chapter_tag_id=$2`, projectID, chapterTagID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return ChapterTag{}, ErrNotFound
	}
	return item, err
}

func (store PostgresStore) getChapterTagByHeading(ctx context.Context, projectID, headingBlockID string) (ChapterTag, error) {
	item, err := scanChapterTag(store.DB.QueryRowContext(ctx, `SELECT `+chapterTagColumns+` FROM article_chapter_tags WHERE project_id=$1 AND heading_block_id=$2`, projectID, headingBlockID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return ChapterTag{}, ErrNotFound
	}
	return item, err
}

func (store PostgresStore) ListChapterTags(ctx context.Context, projectID string) ([]ChapterTag, error) {
	rows, err := store.DB.QueryContext(ctx, `SELECT `+chapterTagColumns+` FROM article_chapter_tags WHERE project_id=$1 ORDER BY updated_at DESC, heading_block_id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ChapterTag{}
	for rows.Next() {
		item, err := scanChapterTag(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store PostgresStore) UpdateChapterTag(ctx context.Context, projectID, chapterTagID, status, actorID string) (ChapterTag, error) {
	var result ChapterTag
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var headingBlockID string
		if err := tx.QueryRowContext(ctx, `SELECT heading_block_id FROM article_chapter_tags WHERE project_id=$1 AND chapter_tag_id=$2 FOR UPDATE`, projectID, chapterTagID).Scan(&headingBlockID); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var block Block
		var attrs []byte
		if err := tx.QueryRowContext(ctx, `SELECT block_type,text_content,attributes FROM article_blocks WHERE project_id=$1 AND block_id=$2`, projectID, headingBlockID).Scan(&block.NodeType, &block.Text, &attrs); errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		} else if err != nil {
			return err
		}
		_ = json.Unmarshal(attrs, &block.Attrs)
		if block.NodeType != "heading" {
			return ErrConflict
		}
		now := store.now()
		if _, err := tx.ExecContext(ctx, `UPDATE article_chapter_tags SET status=$3,heading_block_type='heading',heading_fingerprint=$4,stale_reason=NULL,reviewed_by=NULL,reviewed_at=NULL,updated_by=$5,updated_at=$6 WHERE project_id=$1 AND chapter_tag_id=$2`, projectID, chapterTagID, status, chapterHeadingFingerprint(block), actorID, now); err != nil {
			return err
		}
		return store.record(ctx, tx, "article.chapter.updated", projectID, actorID, "article_chapter_tag", chapterTagID, map[string]interface{}{"chapter_tag_id": chapterTagID, "status": status})
	})
	if err != nil {
		return ChapterTag{}, err
	}
	result, err = store.GetChapterTag(ctx, projectID, chapterTagID)
	return result, err
}

func (store PostgresStore) DeleteChapterTag(ctx context.Context, projectID, chapterTagID, actorID string) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM article_chapter_tags WHERE project_id=$1 AND chapter_tag_id=$2`, projectID, chapterTagID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			return ErrNotFound
		}
		return store.record(ctx, tx, "article.chapter.deleted", projectID, actorID, "article_chapter_tag", chapterTagID, map[string]interface{}{"chapter_tag_id": chapterTagID})
	})
}

func (store PostgresStore) ReviewChapterTag(ctx context.Context, projectID, chapterTagID, actorID string) (ChapterTag, error) {
	var result ChapterTag
	stale := false
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var item ChapterTag
		var reviewedBy sql.NullString
		var reviewedAt sql.NullTime
		if err := tx.QueryRowContext(ctx, `SELECT chapter_tag_id,project_id,heading_block_id,status,heading_block_type,heading_fingerprint,COALESCE(stale_reason,''),updated_by,updated_at,reviewed_by,reviewed_at FROM article_chapter_tags WHERE project_id=$1 AND chapter_tag_id=$2 FOR UPDATE`, projectID, chapterTagID).Scan(&item.ChapterTagID, &item.ProjectID, &item.HeadingBlockID, &item.Status, &item.HeadingBlockType, &item.HeadingFingerprint, &item.StaleReason, &item.UpdatedBy, &item.UpdatedAt, &reviewedBy, &reviewedAt); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var block Block
		var attrs []byte
		if err := tx.QueryRowContext(ctx, `SELECT block_type,text_content,attributes FROM article_blocks WHERE project_id=$1 AND block_id=$2`, projectID, item.HeadingBlockID).Scan(&block.NodeType, &block.Text, &attrs); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				stale = true
				return markChapterTagNeedsReview(ctx, tx, item, actorID, "heading_missing_or_id_changed", store.now())
			}
			return err
		}
		_ = json.Unmarshal(attrs, &block.Attrs)
		if block.NodeType != "heading" {
			stale = true
			return markChapterTagNeedsReview(ctx, tx, item, actorID, "heading_type_changed", store.now())
		}
		if chapterHeadingFingerprint(block) != item.HeadingFingerprint {
			stale = true
			return markChapterTagNeedsReview(ctx, tx, item, actorID, "heading_content_changed", store.now())
		}
		now := store.now()
		if _, err := tx.ExecContext(ctx, `UPDATE article_chapter_tags SET status='reviewed',reviewed_by=$3,reviewed_at=$4,updated_by=$3,updated_at=$4,stale_reason=NULL WHERE project_id=$1 AND chapter_tag_id=$2`, projectID, chapterTagID, actorID, now); err != nil {
			return err
		}
		result = item
		result.Status = ChapterTagReviewed
		result.UpdatedBy = actorID
		result.UpdatedAt = now
		result.ReviewedBy = &actorID
		result.ReviewedAt = &now
		result.StaleReason = ""
		return store.record(ctx, tx, "article.chapter.reviewed", projectID, actorID, "article_chapter_tag", chapterTagID, map[string]interface{}{"chapter_tag_id": chapterTagID, "status": ChapterTagReviewed, "reviewed_by": actorID})
	})
	if err == nil && stale {
		value, getErr := store.GetChapterTag(ctx, projectID, chapterTagID)
		if getErr != nil {
			return ChapterTag{}, getErr
		}
		return value, ErrConflict
	}
	return result, err
}

func markChapterTagNeedsReview(ctx context.Context, tx transaction.Tx, item ChapterTag, actorID, reason string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `UPDATE article_chapter_tags SET status='needs_review',stale_reason=$3,updated_by=$4,updated_at=$5 WHERE project_id=$1 AND chapter_tag_id=$2`, item.ProjectID, item.ChapterTagID, reason, actorID, now); err != nil {
		return err
	}
	return nil
}

func (store PostgresStore) reconcileChapterTagsInTransaction(ctx context.Context, tx transaction.Tx, projectID, actorID string, blocks []Block) error {
	rows, err := tx.QueryContext(ctx, `SELECT `+chapterTagColumns+` FROM article_chapter_tags WHERE project_id=$1 FOR UPDATE`, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	existing := map[string]ChapterTag{}
	for rows.Next() {
		item, err := scanChapterTag(rows.Scan)
		if err != nil {
			return err
		}
		existing[item.HeadingBlockID] = item
	}
	if err := rows.Err(); err != nil {
		return err
	}
	byID := map[string]Block{}
	for _, block := range blocks {
		byID[block.BlockID] = block
	}
	headingBlocks := headingBlocksByID(blocks)
	now := store.now()
	for headingID, item := range existing {
		block, currentHeading := headingBlocks[headingID]
		if !currentHeading {
			block = byID[headingID]
		}
		status, reason, blockType, changed := reconcileChapterTagState(item, block, currentHeading)
		if !changed {
			continue
		}
		fingerprint := item.HeadingFingerprint
		if currentHeading {
			fingerprint = chapterHeadingFingerprint(block)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE article_chapter_tags SET status=$3,heading_block_type=$4,heading_fingerprint=$5,stale_reason=NULLIF($6,''),reviewed_by=NULL,reviewed_at=NULL,updated_by=$7,updated_at=$8 WHERE project_id=$1 AND chapter_tag_id=$2`, projectID, item.ChapterTagID, status, blockType, fingerprint, reason, actorID, now); err != nil {
			return err
		}
	}
	for headingID, block := range headingBlocks {
		if _, exists := existing[headingID]; exists {
			continue
		}
		id, err := store.Generator.New()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO article_chapter_tags(chapter_tag_id,project_id,heading_block_id,status,heading_block_type,heading_fingerprint,updated_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, projectID, headingID, initialChapterTagStatus(block), block.NodeType, chapterHeadingFingerprint(block), actorID, now); err != nil {
			return err
		}
	}
	return nil
}
