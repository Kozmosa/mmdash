package artifact

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/datahub"
	"github.com/mmdash/mmdash/backend/internal/project"
)

// DataHubProjectionReader exposes a storage-safe authoritative snapshot.
type DataHubProjectionReader interface {
	DataHubSnapshot(context.Context, string, string) (datahub.ArtifactProjection, error)
}

// DataHubProjectionSink persists the derived Data Hub objects and activity.
type DataHubProjectionSink interface {
	ProjectArtifact(context.Context, contract.EventEnvelope, datahub.ArtifactProjection) error
}

// DataHubProjector refreshes both Artifact and attachment registry projections
// from authoritative state after a committed lifecycle event.
type DataHubProjector struct {
	Reader DataHubProjectionReader
	Sink   DataHubProjectionSink
}

func (projector DataHubProjector) Project(
	ctx context.Context,
	event contract.EventEnvelope,
) error {
	if event.ProjectID == nil || projector.Reader == nil || projector.Sink == nil {
		return datahub.ErrInvalid
	}
	artifactID, ok := event.Payload["artifact_id"].(string)
	if !ok || strings.TrimSpace(artifactID) == "" {
		return datahub.ErrInvalid
	}
	snapshot, err := projector.Reader.DataHubSnapshot(
		ctx, *event.ProjectID, artifactID,
	)
	if err != nil {
		return err
	}
	return projector.Sink.ProjectArtifact(ctx, event, snapshot)
}

// DataHubSnapshot returns only projection-safe Artifact metadata.
func (store PostgresStore) DataHubSnapshot(
	ctx context.Context,
	projectID string,
	artifactID string,
) (datahub.ArtifactProjection, error) {
	detail, err := store.GetDetail(ctx, projectID, artifactID, true)
	if err != nil {
		return datahub.ArtifactProjection{}, err
	}
	snapshot := datahub.ArtifactProjection{
		ID: detail.Artifact.ID, ProjectID: detail.Artifact.ProjectID,
		Kind: detail.Artifact.Kind, Source: detail.Artifact.Source,
		Tags: detail.Artifact.Tags, Name: detail.Artifact.Name,
		Description: stringValue(detail.Artifact.Description),
		Status:      detail.Artifact.Status,
		OccurredAt:  detail.Artifact.UpdatedAt,
		Registry:    []datahub.AttachmentRegistryProjection{},
	}
	if detail.CurrentVersion != nil {
		version := detail.CurrentVersion
		snapshot.Version = &datahub.ArtifactVersionProjection{
			ID: version.ID, VersionNo: version.VersionNo,
			Filename: version.Filename, SHA256: version.SHA256,
			MIMEType: version.MIMEType, SizeBytes: version.SizeBytes,
			Status: version.Status, OccurredAt: version.CreatedAt,
		}
	}
	rows, err := store.DB.QueryContext(ctx, `
		SELECT attachment_id::text,artifact_id::text,
		       COALESCE(version_id::text,''),source,
		       description,COALESCE(recommended_usage,'[]'),status,updated_at
		FROM artifact_registry_entries
		WHERE project_id=$1 AND artifact_id=$2
		ORDER BY created_at,attachment_id
	`, projectID, artifactID)
	if err != nil {
		return datahub.ArtifactProjection{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry datahub.AttachmentRegistryProjection
		var description sql.NullString
		var recommended []byte
		if err := rows.Scan(
			&entry.ID, &entry.ArtifactID, &entry.VersionID, &entry.Source,
			&description, &recommended, &entry.Status, &entry.OccurredAt,
		); err != nil {
			return datahub.ArtifactProjection{}, err
		}
		if description.Valid {
			entry.Description = description.String
		}
		if err := json.Unmarshal(recommended, &entry.RecommendedUsage); err != nil {
			return datahub.ArtifactProjection{}, err
		}
		if entry.RecommendedUsage == nil {
			entry.RecommendedUsage = []string{}
		}
		snapshot.Registry = append(snapshot.Registry, entry)
	}
	return snapshot, rows.Err()
}

// GetRegistryEntry resolves one authoritative attachment registry row.
func (store PostgresStore) GetRegistryEntry(
	ctx context.Context,
	projectID string,
	attachmentID string,
) (AttachmentRegistryEntry, error) {
	var entry AttachmentRegistryEntry
	var description sql.NullString
	var recommended []byte
	err := store.DB.QueryRowContext(ctx, `
		SELECT attachment_id::text,project_id::text,artifact_id::text,
		       COALESCE(version_id::text,''),source,description,
		       COALESCE(recommended_usage,'[]'),
		       status,created_by::text,created_at,updated_at
		FROM artifact_registry_entries
		WHERE project_id=$1 AND attachment_id=$2
	`, projectID, attachmentID).Scan(
		&entry.ID, &entry.ProjectID, &entry.ArtifactID, &entry.VersionID,
		&entry.Source, &description, &recommended, &entry.Status,
		&entry.CreatedBy, &entry.CreatedAt, &entry.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AttachmentRegistryEntry{}, ErrNotFound
	}
	if err != nil {
		return AttachmentRegistryEntry{}, err
	}
	if description.Valid {
		entry.Description = &description.String
	}
	if err := json.Unmarshal(recommended, &entry.RecommendedUsage); err != nil {
		return AttachmentRegistryEntry{}, err
	}
	if entry.RecommendedUsage == nil {
		entry.RecommendedUsage = []string{}
	}
	return entry, nil
}

type registryReader interface {
	GetRegistryEntry(context.Context, string, string) (AttachmentRegistryEntry, error)
}

// DataHubReaderAdapter resolves Data Hub objects through Artifact authorization
// and generates transfer grants only for the current authorized read.
type DataHubReaderAdapter struct {
	Registry registryReader
	Service  *Service
}

func (reader DataHubReaderAdapter) Read(
	ctx context.Context,
	identity auth.Identity,
	object datahub.Object,
) (interface{}, error) {
	if reader.Service == nil {
		return nil, ErrNotAvailable
	}
	switch object.ObjectType {
	case "artifact":
		return reader.readArtifact(
			ctx, identity, object.ProjectID, object.SourceID,
		)
	case "attachment_registry_entry":
		if reader.Registry == nil {
			return nil, ErrNotAvailable
		}
		entry, err := reader.Registry.GetRegistryEntry(
			ctx, object.ProjectID, object.SourceID,
		)
		if err != nil || entry.Status != "active" {
			return nil, ErrNotFound
		}
		content, err := reader.readArtifact(
			ctx, identity, object.ProjectID, entry.ArtifactID,
		)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"registry_entry": entry,
			"artifact":       content,
		}, nil
	default:
		return nil, datahub.ErrAdapterNotFound
	}
}

func (reader DataHubReaderAdapter) readArtifact(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	artifactID string,
) (map[string]interface{}, error) {
	detail, err := reader.Service.Get(
		ctx, identity, projectID, artifactID, false,
	)
	if err != nil {
		return nil, err
	}
	content := map[string]interface{}{"detail": detail}
	if detail.CurrentVersion == nil ||
		detail.Artifact.Status != StatusAvailable ||
		detail.CurrentVersion.Status != StatusAvailable {
		return content, nil
	}
	download, err := reader.Service.Download(
		ctx, identity, projectID, artifactID, detail.CurrentVersion.ID,
	)
	if err != nil {
		return nil, err
	}
	previews, err := reader.Service.ListPreviews(
		ctx, identity, projectID, artifactID, detail.CurrentVersion.ID,
	)
	if errors.Is(err, ErrNotFound) {
		previews = PreviewList{Items: []Preview{}}
	} else if err != nil {
		return nil, err
	}
	content["download"] = download
	content["previews"] = previews
	return content, nil
}

// ValidateProjectReferences implements Project's narrow Artifact validation
// boundary without giving Project access to Artifact tables.
func (service Service) ValidateProjectReferences(
	ctx context.Context,
	projectID string,
	artifactIDs []string,
) error {
	seen := map[string]struct{}{}
	for _, artifactID := range artifactIDs {
		trimmed := strings.TrimSpace(artifactID)
		if trimmed == "" || trimmed != artifactID {
			return project.ErrInvalid
		}
		if _, exists := seen[artifactID]; exists {
			return project.ErrInvalid
		}
		seen[artifactID] = struct{}{}
		item, err := service.Store.GetArtifact(ctx, projectID, artifactID)
		if err != nil ||
			item.ProjectID != projectID ||
			item.Status != StatusAvailable ||
			item.CurrentVersionID == nil {
			return project.ErrInvalid
		}
	}
	return nil
}

type projectSourceReader interface {
	Get(context.Context, auth.Identity, string) (project.Project, error)
}

// ProjectHomeReader supplies the Data Hub Problem section with the Project's
// validated source Artifacts and current preview state.
type ProjectHomeReader struct {
	Projects projectSourceReader
	Service  *Service
}

func (reader ProjectHomeReader) ProblemItems(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) ([]interface{}, error) {
	if reader.Projects == nil || reader.Service == nil {
		return []interface{}{}, nil
	}
	item, err := reader.Projects.Get(ctx, identity, projectID)
	if err != nil {
		return nil, err
	}
	results := make([]interface{}, 0, len(item.SourceArtifactIDs))
	for _, artifactID := range item.SourceArtifactIDs {
		detail, err := reader.Service.Get(
			ctx, identity, projectID, artifactID, false,
		)
		if errors.Is(err, ErrNotFound) {
			results = append(results, map[string]interface{}{
				"artifact_id": artifactID, "status": "unavailable",
			})
			continue
		}
		if err != nil {
			return nil, err
		}
		if detail.CurrentVersion == nil {
			return nil, ErrNotFound
		}
		previews, err := reader.Service.ListPreviews(
			ctx, identity, projectID, artifactID, detail.CurrentVersion.ID,
		)
		if errors.Is(err, ErrNotFound) {
			previews = PreviewList{Items: []Preview{}}
		} else if err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"detail": detail, "previews": previews,
		})
	}
	return results, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
