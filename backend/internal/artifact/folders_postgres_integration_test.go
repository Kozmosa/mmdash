package artifact

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
)

func TestPostgresRecursiveFolderDeletionPreservesArtifactsAtRoot(t *testing.T) {
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	generator := identity.Generator{}
	userID := generator.MustNew()
	projectID := generator.MustNew()
	parentID := generator.MustNew()
	childID := generator.MustNew()
	artifactID := generator.MustNew()
	versionID := generator.MustNew()
	blobID := generator.MustNew()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	digest := strings.Repeat("a", 64)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []struct {
		query string
		args  []interface{}
	}{
		{`INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at) VALUES($1,$2,'Folder owner','test','active',$3,$3)`, []interface{}{userID, userID + "@test.local", now}},
		{`INSERT INTO projects(project_id,name,created_by,created_at,updated_at) VALUES($1,'Folder integration',$2,$3,$3)`, []interface{}{projectID, userID, now}},
		{`INSERT INTO artifact_folders(folder_id,project_id,parent_folder_id,name,position,created_at,updated_at) VALUES($1,$2,NULL,'Parent',0,$4,$4),($3,$2,$1,'Child',0,$4,$4)`, []interface{}{parentID, projectID, childID, now}},
		{`INSERT INTO artifact_blobs(blob_id,project_id,sha256,size_bytes,backend,object_key,reference_count,created_at,updated_at) VALUES($1,$2,$3,1,'local',$4,1,$5,$5)`, []interface{}{blobID, projectID, digest, "projects/" + projectID + "/folder-test", now}},
		{`INSERT INTO artifact_artifacts(artifact_id,project_id,kind,source,name,tags,status,current_version_id,created_by,created_at,updated_at,folder_id) VALUES($1,$2,'attachment','user_upload','Folder file','{}','available',$3,$4,$5,$5,$6)`, []interface{}{artifactID, projectID, versionID, userID, now, childID}},
		{`INSERT INTO artifact_versions(version_id,artifact_id,project_id,version_no,storage_class,blob_id,filename,mime_type,size_bytes,sha256,status,created_by,created_at,available_at) VALUES($1,$2,$3,1,'object',$4,'file.txt','text/plain',1,$5,'available',$6,$7,$7)`, []interface{}{versionID, artifactID, projectID, blobID, digest, userID, now}},
	} {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed folder integration: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM artifact_artifacts WHERE artifact_id=$1`, artifactID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM artifact_blobs WHERE blob_id=$1`, blobID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})

	store := PostgresStore{
		DB: db,
		Transaction: transaction.Manager{
			DB: transaction.SQLBeginner{DB: db},
		},
	}
	if err := store.DeleteFolder(ctx, projectID, parentID, false, now); !errors.Is(err, ErrFolderHasChildren) {
		t.Fatalf("non-recursive delete should reject children: %v", err)
	}
	if err := store.DeleteFolder(ctx, projectID, parentID, true, now); err != nil {
		t.Fatalf("recursive folder delete: %v", err)
	}
	var folderCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artifact_folders WHERE project_id=$1`, projectID).Scan(&folderCount); err != nil {
		t.Fatal(err)
	}
	var root bool
	if err := db.QueryRowContext(ctx, `SELECT folder_id IS NULL FROM artifact_artifacts WHERE project_id=$1 AND artifact_id=$2`, projectID, artifactID).Scan(&root); err != nil {
		t.Fatal(err)
	}
	if folderCount != 0 || !root {
		t.Fatalf("folder hierarchy or Artifact placement survived: folders=%d root=%v", folderCount, root)
	}
}
