package datahub

import (
	"context"
	"fmt"
	"time"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

// ArtifactVersionProjection is the immutable Version metadata safe to index in
// Data Hub. Storage provider identifiers and object keys are deliberately absent.
type ArtifactVersionProjection struct {
	ID         string
	VersionNo  int
	Filename   string
	SHA256     string
	MIMEType   string
	SizeBytes  int64
	Status     string
	OccurredAt time.Time
}

// AttachmentRegistryProjection is one authoritative consumer-facing attachment
// registry row. It never contains transfer URLs or storage credentials.
type AttachmentRegistryProjection struct {
	ID               string
	ArtifactID       string
	VersionID        string
	Source           string
	Description      string
	RecommendedUsage []string
	Status           string
	OccurredAt       time.Time
}

// ArtifactProjection is the authoritative snapshot used by an event projector.
type ArtifactProjection struct {
	ID          string
	ProjectID   string
	Kind        string
	Source      string
	Tags        []string
	Name        string
	Description string
	Status      string
	OccurredAt  time.Time
	Version     *ArtifactVersionProjection
	Registry    []AttachmentRegistryProjection
}

// ProjectArtifact applies one Artifact lifecycle event exactly once.
func (store PostgresStore) ProjectArtifact(
	ctx context.Context,
	event contract.EventEnvelope,
	snapshot ArtifactProjection,
) error {
	if event.ProjectID == nil ||
		snapshot.ProjectID != *event.ProjectID ||
		snapshot.ID == "" ||
		event.Actor["user_id"] == "" ||
		(event.EventType != "artifact.created" &&
			event.EventType != "artifact.available" &&
			event.EventType != "artifact.deleted") {
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
	registryObjectIDs := make([]string, len(snapshot.Registry))
	for index := range registryObjectIDs {
		registryObjectIDs[index], err = store.Generator.New()
		if err != nil {
			return err
		}
	}
	status := snapshot.Status
	if event.EventType == "artifact.deleted" || status == "trashed" {
		status = "hidden"
	}
	metadata := artifactMetadata(snapshot)
	actor := jsonBytes(event.Actor)
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var projected bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM data_activity WHERE event_id=$1)
		`, event.EventID).Scan(&projected); err != nil {
			return err
		}
		if projected {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO data_objects(
				object_id,project_id,object_type,source_module,source_id,
				title,summary,status,metadata,occurred_at,created_at,updated_at
			) VALUES($1,$2,'artifact','artifact',$3,$4,$5,$6,$7,$8,$9,$9)
			ON CONFLICT(source_module,object_type,source_id) DO UPDATE
			SET title=EXCLUDED.title,summary=EXCLUDED.summary,
			    status=EXCLUDED.status,metadata=EXCLUDED.metadata,
			    version=data_objects.version+1,
			    occurred_at=EXCLUDED.occurred_at,updated_at=EXCLUDED.updated_at
		`, objectID, snapshot.ProjectID, snapshot.ID, snapshot.Name,
			snapshot.Description, status, jsonBytes(metadata),
			event.OccurredAt, store.Clock.Now().UTC()); err != nil {
			return fmt.Errorf("project artifact object: %w", err)
		}
		for index, entry := range snapshot.Registry {
			entryStatus := entry.Status
			if status == "hidden" {
				entryStatus = "hidden"
			}
			entryMetadata := map[string]interface{}{
				"artifact_id":       entry.ArtifactID,
				"version_id":        entry.VersionID,
				"source":            entry.Source,
				"recommended_usage": nonNilStrings(entry.RecommendedUsage),
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO data_objects(
					object_id,project_id,object_type,source_module,source_id,
					title,summary,status,metadata,occurred_at,created_at,updated_at
				) VALUES(
					$1,$2,'attachment_registry_entry','artifact',$3,
					$4,$5,$6,$7,$8,$9,$9
				)
				ON CONFLICT(source_module,object_type,source_id) DO UPDATE
				SET title=EXCLUDED.title,summary=EXCLUDED.summary,
				    status=EXCLUDED.status,metadata=EXCLUDED.metadata,
				    version=data_objects.version+1,
				    occurred_at=EXCLUDED.occurred_at,
				    updated_at=EXCLUDED.updated_at
			`, registryObjectIDs[index], snapshot.ProjectID, entry.ID,
				snapshot.Name, entry.Description, entryStatus,
				jsonBytes(entryMetadata), event.OccurredAt,
				store.Clock.Now().UTC()); err != nil {
				return fmt.Errorf("project attachment registry object: %w", err)
			}
		}
		if status == "hidden" {
			if _, err := tx.ExecContext(ctx, `
				UPDATE data_objects
				SET status='hidden',version=version+1,
				    occurred_at=$3,updated_at=$4
				WHERE project_id=$1
				  AND source_module='artifact'
				  AND object_type='attachment_registry_entry'
				  AND metadata->>'artifact_id'=$2
			`, snapshot.ProjectID, snapshot.ID, event.OccurredAt,
				store.Clock.Now().UTC()); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO data_activity(
				activity_id,project_id,object_id,event_id,activity_type,
				title,summary,actor,metadata,occurred_at,created_at
			)
			SELECT $1,$2,object_id,$3,$4,$5,$6,$7,$8,$9,$10
			FROM data_objects
			WHERE source_module='artifact' AND object_type='artifact'
			  AND source_id=$11
			ON CONFLICT(event_id) WHERE event_id IS NOT NULL DO NOTHING
		`, activityID, snapshot.ProjectID, event.EventID, event.EventType,
			snapshot.Name, snapshot.Description, actor, jsonBytes(metadata),
			event.OccurredAt, store.Clock.Now().UTC(), snapshot.ID)
		if err != nil {
			return fmt.Errorf("project artifact activity: %w", err)
		}
		return nil
	})
}

func artifactMetadata(snapshot ArtifactProjection) map[string]interface{} {
	metadata := map[string]interface{}{
		"artifact_id": snapshot.ID,
		"kind":        snapshot.Kind,
		"source":      snapshot.Source,
		"tags":        nonNilStrings(snapshot.Tags),
	}
	if snapshot.Version != nil {
		metadata["current_version_id"] = snapshot.Version.ID
		metadata["version_no"] = snapshot.Version.VersionNo
		metadata["filename"] = snapshot.Version.Filename
		metadata["sha256"] = snapshot.Version.SHA256
		metadata["mime_type"] = snapshot.Version.MIMEType
		metadata["size_bytes"] = snapshot.Version.SizeBytes
	}
	return metadata
}
