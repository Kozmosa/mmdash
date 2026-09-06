package artifact

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func folderParentArg(parentID *string) interface{} {
	if parentID == nil {
		return nil
	}
	return *parentID
}

func (store PostgresStore) CreateFolder(ctx context.Context, folder Folder) (Folder, error) {
	var created Folder
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		err := tx.QueryRowContext(ctx, `
			INSERT INTO artifact_folders(
				folder_id, project_id, parent_folder_id, name, position,
				created_at, updated_at
			)
			SELECT $1, $2, NULLIF($3, '')::uuid, $4,
				COALESCE((
					SELECT MAX(position) + 1 FROM artifact_folders
					WHERE project_id=$2
					  AND parent_folder_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
				), 0), CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
			RETURNING folder_id, project_id, parent_folder_id, name, position
		`, folder.ID, folder.ProjectID, folderParentArg(folder.ParentFolderID), folder.Name).Scan(
			&created.ID, &created.ProjectID, &created.ParentFolderID, &created.Name, &created.Position,
		)
		if err != nil {
			return mapFolderPostgresError(err)
		}
		created.Children = []Folder{}
		return store.audit(ctx, tx, "artifact.folder.created", created.ProjectID, created.ID, map[string]interface{}{
			"name": created.Name,
		})
	})
	return created, err
}

// EnsureFolderPath creates or resolves one complete Project-scoped folder
// path in a single transaction. The unique parent/name index serializes
// concurrent Article and Experiment jobs targeting the same path.
func (store PostgresStore) EnsureFolderPath(
	ctx context.Context,
	projectID string,
	segments []string,
) (Folder, error) {
	var leaf Folder
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var parentID *string
		for _, name := range segments {
			folderID, err := store.Generator.New()
			if err != nil {
				return err
			}
			created := true
			err = tx.QueryRowContext(ctx, `
				INSERT INTO artifact_folders(
					folder_id, project_id, parent_folder_id, name, position,
					created_at, updated_at
				)
				SELECT $1, $2, NULLIF($3, '')::uuid, $4,
					COALESCE((
						SELECT MAX(position) + 1 FROM artifact_folders
						WHERE project_id=$2
						  AND parent_folder_id IS NOT DISTINCT FROM NULLIF($3, '')::uuid
					), 0), CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
				ON CONFLICT DO NOTHING
				RETURNING folder_id, project_id, parent_folder_id, name, position
			`, folderID, projectID, folderParentArg(parentID), name).Scan(
				&leaf.ID, &leaf.ProjectID, &leaf.ParentFolderID, &leaf.Name, &leaf.Position,
			)
			if errors.Is(err, sql.ErrNoRows) {
				created = false
				err = tx.QueryRowContext(ctx, `
					SELECT folder_id, project_id, parent_folder_id, name, position
					FROM artifact_folders
					WHERE project_id=$1
					  AND parent_folder_id IS NOT DISTINCT FROM NULLIF($2, '')::uuid
					  AND lower(name)=lower($3)
				`, projectID, folderParentArg(parentID), name).Scan(
					&leaf.ID, &leaf.ProjectID, &leaf.ParentFolderID, &leaf.Name, &leaf.Position,
				)
			}
			if err != nil {
				return mapFolderPostgresError(err)
			}
			leaf.Children = []Folder{}
			if created {
				if err := store.audit(ctx, tx, "artifact.folder.created", projectID, leaf.ID, map[string]interface{}{
					"managed":          true,
					"name":             leaf.Name,
					"parent_folder_id": leaf.ParentFolderID,
				}); err != nil {
					return err
				}
			}
			parent := leaf.ID
			parentID = &parent
		}
		return nil
	})
	return leaf, err
}

func (store PostgresStore) GetFolderTree(ctx context.Context, projectID string) (FolderTree, error) {
	rows, err := store.DB.QueryContext(ctx, `
		SELECT folder_id, project_id, parent_folder_id, name, position
		FROM artifact_folders
		WHERE project_id=$1
		ORDER BY position, lower(name), folder_id
	`, projectID)
	if err != nil {
		return FolderTree{}, err
	}
	defer rows.Close()
	flat := make([]Folder, 0)
	for rows.Next() {
		var folder Folder
		if err := rows.Scan(&folder.ID, &folder.ProjectID, &folder.ParentFolderID, &folder.Name, &folder.Position); err != nil {
			return FolderTree{}, err
		}
		folder.Children = []Folder{}
		flat = append(flat, folder)
	}
	if err := rows.Err(); err != nil {
		return FolderTree{}, err
	}
	return buildFolderTree(flat), nil
}

func buildFolderTree(flat []Folder) FolderTree {
	byID := make(map[string]Folder, len(flat))
	children := make(map[string][]string)
	roots := make([]string, 0)
	for _, folder := range flat {
		folder.Children = []Folder{}
		byID[folder.ID] = folder
		if folder.ParentFolderID == nil {
			roots = append(roots, folder.ID)
		} else {
			children[*folder.ParentFolderID] = append(children[*folder.ParentFolderID], folder.ID)
		}
	}
	var build func(string) Folder
	build = func(id string) Folder {
		folder := byID[id]
		for _, childID := range children[id] {
			folder.Children = append(folder.Children, build(childID))
		}
		return folder
	}
	items := make([]Folder, 0, len(roots))
	for _, id := range roots {
		items = append(items, build(id))
	}
	return FolderTree{Items: items}
}

func (store PostgresStore) RenameFolder(
	ctx context.Context,
	projectID, folderID, name string,
	now time.Time,
) (Folder, error) {
	var folder Folder
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		err := tx.QueryRowContext(ctx, `
			UPDATE artifact_folders
			SET name=$3, updated_at=$4
			WHERE project_id=$1 AND folder_id=$2
			RETURNING folder_id, project_id, parent_folder_id, name, position
		`, projectID, folderID, name, now).Scan(
			&folder.ID, &folder.ProjectID, &folder.ParentFolderID, &folder.Name, &folder.Position,
		)
		if err != nil {
			return mapFolderPostgresError(err)
		}
		folder.Children = []Folder{}
		return store.audit(ctx, tx, "artifact.folder.renamed", projectID, folderID, map[string]interface{}{
			"name": name,
		})
	})
	return folder, err
}

func (store PostgresStore) MoveFolder(
	ctx context.Context,
	projectID, folderID string,
	parentID *string,
	position *int,
	now time.Time,
) (Folder, error) {
	var folder Folder
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM artifact_folders WHERE project_id=$1 AND folder_id=$2)
		`, projectID, folderID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		if parentID != nil {
			if *parentID == folderID {
				return ErrFolderCycle
			}
			var parentExists bool
			if err := tx.QueryRowContext(ctx, `
				SELECT EXISTS(SELECT 1 FROM artifact_folders WHERE project_id=$1 AND folder_id=$2)
			`, projectID, *parentID).Scan(&parentExists); err != nil {
				return err
			}
			if !parentExists {
				return ErrNotFound
			}
			var descendant bool
			if err := tx.QueryRowContext(ctx, `
				WITH RECURSIVE descendants(folder_id) AS (
					SELECT folder_id FROM artifact_folders WHERE project_id=$1 AND parent_folder_id=$2
					UNION ALL
					SELECT child.folder_id FROM artifact_folders child
					JOIN descendants parent ON child.parent_folder_id=parent.folder_id
					WHERE child.project_id=$1
				)
				SELECT EXISTS(SELECT 1 FROM descendants WHERE folder_id=$3)
			`, projectID, folderID, *parentID).Scan(&descendant); err != nil {
				return err
			}
			if descendant {
				return ErrFolderCycle
			}
		}
		positionSQL := "position"
		args := []interface{}{projectID, folderID, folderParentArg(parentID), now}
		if position != nil {
			positionSQL = "$5"
			args = append(args, *position)
		}
		query := `UPDATE artifact_folders SET parent_folder_id=NULLIF($3, '')::uuid, updated_at=$4, position=` + positionSQL + ` WHERE project_id=$1 AND folder_id=$2 RETURNING folder_id, project_id, parent_folder_id, name, position`
		if err := tx.QueryRowContext(ctx, query, args...).Scan(
			&folder.ID, &folder.ProjectID, &folder.ParentFolderID, &folder.Name, &folder.Position,
		); err != nil {
			return mapFolderPostgresError(err)
		}
		folder.Children = []Folder{}
		return store.audit(ctx, tx, "artifact.folder.moved", projectID, folderID, map[string]interface{}{
			"parent_folder_id": parentID,
			"position":         position,
		})
	})
	return folder, err
}

func (store PostgresStore) DeleteFolder(
	ctx context.Context,
	projectID, folderID string,
	recursive bool,
	now time.Time,
) error {
	return store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM artifact_folders
				WHERE project_id=$1 AND folder_id=$2
			)
		`, projectID, folderID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		var childCount int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM artifact_folders
			WHERE project_id=$1 AND parent_folder_id=$2
		`, projectID, folderID).Scan(&childCount); err != nil {
			return err
		}
		if childCount > 0 && !recursive {
			return ErrFolderHasChildren
		}
		if recursive {
			if _, err := tx.ExecContext(ctx, `
				WITH RECURSIVE descendants(folder_id) AS (
					SELECT folder_id FROM artifact_folders
					WHERE project_id=$1 AND folder_id=$2
					UNION ALL
					SELECT child.folder_id FROM artifact_folders child
					JOIN descendants parent
					  ON child.parent_folder_id=parent.folder_id
					WHERE child.project_id=$1
				)
				UPDATE artifact_artifacts
				SET folder_id=NULL, updated_at=$3
				WHERE project_id=$1
				  AND folder_id IN (SELECT folder_id FROM descendants)
			`, projectID, folderID, now); err != nil {
				return err
			}
			// Break the self-referential RESTRICT edges before deleting the
			// selected subtree. Artifact relations were moved to root above, so
			// recursive folder deletion never deletes Artifact data.
			if _, err := tx.ExecContext(ctx, `
				WITH RECURSIVE descendants(folder_id) AS (
					SELECT folder_id FROM artifact_folders
					WHERE project_id=$1 AND folder_id=$2
					UNION ALL
					SELECT child.folder_id FROM artifact_folders child
					JOIN descendants parent
					  ON child.parent_folder_id=parent.folder_id
					WHERE child.project_id=$1
				)
				UPDATE artifact_folders
				SET parent_folder_id=NULL, updated_at=$3
				WHERE project_id=$1
				  AND folder_id IN (SELECT folder_id FROM descendants)
			`, projectID, folderID, now); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				WITH RECURSIVE descendants(folder_id) AS (
					SELECT folder_id FROM artifact_folders
					WHERE project_id=$1 AND folder_id=$2
					UNION ALL
					SELECT child.folder_id FROM artifact_folders child
					JOIN descendants parent
					  ON child.parent_folder_id=parent.folder_id
					WHERE child.project_id=$1
				)
				DELETE FROM artifact_folders
				WHERE project_id=$1
				  AND folder_id IN (SELECT folder_id FROM descendants)
			`, projectID, folderID); err != nil {
				return mapFolderPostgresError(err)
			}
			return store.audit(ctx, tx, "artifact.folder.deleted", projectID, folderID, map[string]interface{}{
				"artifacts_moved_to_root": true,
				"recursive":               true,
			})
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE artifact_artifacts
			SET folder_id=NULL, updated_at=$3
			WHERE project_id=$1 AND folder_id=$2
		`, projectID, folderID, now)
		if err != nil {
			return err
		}
		_, _ = result.RowsAffected()
		result, err = tx.ExecContext(ctx, `
			DELETE FROM artifact_folders WHERE project_id=$1 AND folder_id=$2
		`, projectID, folderID)
		if err != nil {
			return mapFolderPostgresError(err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			return ErrNotFound
		}
		return store.audit(ctx, tx, "artifact.folder.deleted", projectID, folderID, map[string]interface{}{
			"direct_artifacts_moved_to_root": true,
			"recursive":                      false,
		})
	})
}

func (store PostgresStore) MoveArtifact(
	ctx context.Context,
	projectID, artifactID string,
	folderID *string,
	now time.Time,
) (Detail, error) {
	err := store.Transaction.Within(ctx, nil, func(tx transaction.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE artifact_artifacts
			SET folder_id=NULLIF($3, '')::uuid, updated_at=$4
			WHERE project_id=$1 AND artifact_id=$2 AND status <> 'trashed'
		`, projectID, artifactID, folderParentArg(folderID), now)
		if err != nil {
			return mapFolderPostgresError(err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			return ErrNotFound
		}
		return store.audit(ctx, tx, "artifact.folder.artifact_moved", projectID, artifactID, map[string]interface{}{
			"folder_id": folderID,
		})
	})
	if err != nil {
		return Detail{}, err
	}
	return store.GetDetail(ctx, projectID, artifactID, false)
}

func mapFolderPostgresError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "duplicate key") || strings.Contains(message, "unique constraint") {
		return ErrFolderConflict
	}
	if strings.Contains(message, "foreign key") {
		return ErrNotFound
	}
	return err
}
