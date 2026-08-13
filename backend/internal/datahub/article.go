package datahub

import (
	"context"
	"fmt"
	"strings"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

type ArticleBlockReader interface {
	ArticleProjectionBlocks(context.Context, string) ([]map[string]interface{}, error)
}

type ArticleProjector struct {
	Reader ArticleBlockReader
	Store  PostgresStore
}

func (projector ArticleProjector) Project(ctx context.Context, event contract.EventEnvelope) error {
	blocks := []map[string]interface{}{}
	if event.EventType == "article.draft.flushed" {
		if projector.Reader == nil || event.ProjectID == nil {
			return ErrInvalid
		}
		var err error
		blocks, err = projector.Reader.ArticleProjectionBlocks(ctx, *event.ProjectID)
		if err != nil {
			return err
		}
	}
	return projector.Store.ProjectArticle(ctx, event, blocks)
}

// ProjectArticle materializes searchable Article cards and immutable timeline
// entries while authoritative content remains in Article/Repo/Artifact.
func (store PostgresStore) ProjectArticle(ctx context.Context, event contract.EventEnvelope, blocks []map[string]interface{}) error {
	if event.ProjectID == nil || strings.TrimSpace(*event.ProjectID) == "" {
		return ErrInvalid
	}
	object := articleProjection(event)
	if object.sourceID == "" {
		return ErrInvalid
	}
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		done, err := projectionDone(ctx, tx, event.EventID)
		if err != nil || done {
			return err
		}
		objectID, err := store.Generator.New()
		if err != nil {
			return err
		}
		activityID, err := store.Generator.New()
		if err != nil {
			return err
		}
		now := store.Clock.Now().UTC()
		if _, err = tx.ExecContext(ctx, `INSERT INTO data_objects(object_id,project_id,object_type,source_module,source_id,title,summary,status,metadata,occurred_at,created_at,updated_at) VALUES($1,$2,$3,'article',$4,$5,$6,$7,$8,$9,$10,$10) ON CONFLICT(source_module,object_type,source_id) DO UPDATE SET title=EXCLUDED.title,summary=EXCLUDED.summary,status=EXCLUDED.status,metadata=EXCLUDED.metadata,version=data_objects.version+1,occurred_at=EXCLUDED.occurred_at,updated_at=EXCLUDED.updated_at`, objectID, *event.ProjectID, object.objectType, object.sourceID, object.title, object.summary, object.status, jsonBytes(object.metadata), event.OccurredAt, now); err != nil {
			return err
		}
		if event.EventType == "article.draft.flushed" {
			if _, err = tx.ExecContext(ctx, `UPDATE data_objects SET status='deleted',version=version+1,updated_at=$2 WHERE project_id=$1 AND source_module='article' AND object_type='article_block' AND status<>'deleted'`, *event.ProjectID, now); err != nil {
				return err
			}
			for _, block := range blocks {
				blockID, _ := block["block_id"].(string)
				if blockID == "" {
					return ErrInvalid
				}
				blockObjectID, generateErr := store.Generator.New()
				if generateErr != nil {
					return generateErr
				}
				title := strings.TrimSpace(fmt.Sprint(block["text"]))
				title = truncateRunes(title, 160)
				if title == "" {
					title = fmt.Sprint(block["node_type"])
				}
				if _, err = tx.ExecContext(ctx, `INSERT INTO data_objects(object_id,project_id,object_type,source_module,source_id,title,summary,status,metadata,occurred_at,created_at,updated_at) VALUES($1,$2,'article_block','article',$3,$4,$5,'active',$6,$7,$8,$8) ON CONFLICT(source_module,object_type,source_id) DO UPDATE SET project_id=EXCLUDED.project_id,title=EXCLUDED.title,summary=EXCLUDED.summary,status='active',metadata=EXCLUDED.metadata,version=data_objects.version+1,occurred_at=EXCLUDED.occurred_at,updated_at=EXCLUDED.updated_at`, blockObjectID, *event.ProjectID, blockID, title, fmt.Sprint(block["node_type"]), jsonBytes(block), event.OccurredAt, now); err != nil {
					return err
				}
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO data_activity(activity_id,project_id,object_id,event_id,activity_type,title,summary,actor,metadata,occurred_at,created_at) SELECT $1,$2,object_id,$3,$4,$5,$6,$7,$8,$9,$10 FROM data_objects WHERE source_module='article' AND object_type=$11 AND source_id=$12 ON CONFLICT(event_id) WHERE event_id IS NOT NULL DO NOTHING`, activityID, *event.ProjectID, event.EventID, event.EventType, object.title, object.summary, jsonBytes(event.Actor), jsonBytes(object.metadata), event.OccurredAt, now, object.objectType, object.sourceID)
		return err
	})
}

func truncateRunes(value string, maximum int) string {
	characters := []rune(value)
	if len(characters) <= maximum {
		return value
	}
	return string(characters[:maximum])
}

type articleProjectionObject struct {
	objectType, sourceID, title, summary, status string
	metadata                                     map[string]interface{}
}

func articleProjection(event contract.EventEnvelope) articleProjectionObject {
	payload := event.Payload
	metadata := map[string]interface{}{}
	for key, value := range payload {
		metadata[key] = value
	}
	switch event.EventType {
	case "article.draft.flushed":
		id := fmt.Sprint(payload["draft_revision"])
		return articleProjectionObject{"article_draft", id, "Article draft r" + id, "Collaborative Markdown draft", "active", metadata}
	case "article.patch.proposed", "article.patch.reviewed":
		id, _ := payload["patch_id"].(string)
		status, _ := payload["status"].(string)
		if status == "" {
			status = "proposed"
		}
		return articleProjectionObject{"article_patch", id, "Article patch", status, status, metadata}
	case "article.commit.created":
		id, _ := payload["commit_id"].(string)
		return articleProjectionObject{"article_commit", id, "Article commit", fmt.Sprint(payload["commit_sha"]), "committed", metadata}
	case "article.build.queued", "article.build.completed", "article.build.failed":
		id, _ := payload["build_id"].(string)
		status, _ := payload["status"].(string)
		if status == "" {
			status = strings.TrimPrefix(event.EventType, "article.build.")
		}
		return articleProjectionObject{"article_build", id, "Article build", status, status, metadata}
	case "article.release.created":
		id, _ := payload["release_id"].(string)
		return articleProjectionObject{"article_release", id, "Article release " + fmt.Sprint(payload["tag"]), "Immutable reviewed release", "released", metadata}
	default:
		return articleProjectionObject{}
	}
}
