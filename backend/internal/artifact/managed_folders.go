package artifact

import (
	"context"
	"strings"
)

func (service Service) ensureManagedFolder(
	ctx context.Context,
	projectID string,
	segments []string,
) (*string, error) {
	if projectID == "" || len(segments) == 0 || len(segments) > 8 {
		return nil, ErrInvalid
	}
	normalized := make([]string, len(segments))
	for index, segment := range segments {
		normalized[index] = strings.TrimSpace(segment)
		if !validFolderName(normalized[index]) {
			return nil, ErrInvalid
		}
	}
	leaf, err := service.Store.EnsureFolderPath(ctx, projectID, normalized)
	if err != nil {
		return nil, err
	}
	if leaf.ID == "" {
		return nil, ErrNotFound
	}
	return &leaf.ID, nil
}

func (service Service) ensureManagedArtifactPlacement(
	ctx context.Context,
	projectID string,
	artifactID string,
	folderID *string,
) error {
	item, err := service.Store.GetArtifact(ctx, projectID, artifactID)
	if err != nil {
		return err
	}
	if equalOptionalString(item.FolderID, folderID) {
		return nil
	}
	_, err = service.Store.MoveArtifact(
		ctx, projectID, artifactID, folderID, service.now(),
	)
	return err
}
