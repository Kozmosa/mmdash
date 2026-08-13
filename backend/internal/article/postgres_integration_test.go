package article

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgconn"
	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/platform/identity"
)

func TestPostgresReleaseIsImmutableAndTagCreationIsConcurrentSafe(t *testing.T) {
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MMDASH_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	if err = db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	generator := identity.Generator{}
	userID, projectID := generator.MustNew(), generator.MustNew()
	blobID, artifactID, versionID := generator.MustNew(), generator.MustNew(), generator.MustNew()
	templateID, commitID := generator.MustNew(), generator.MustNew()
	buildOne, buildTwo := generator.MustNew(), generator.MustNew()
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	execSeed := func(query string, args ...interface{}) {
		t.Helper()
		if _, seedErr := seed.ExecContext(ctx, query, args...); seedErr != nil {
			_ = seed.Rollback()
			t.Fatalf("seed Article release graph: %v", seedErr)
		}
	}
	execSeed(`INSERT INTO auth_users(user_id,email,display_name,password_hash,status,created_at,updated_at) VALUES($1,$2,'Article integration','test','active',$3,$3)`, userID, userID+"@article-integration.test", now)
	execSeed(`INSERT INTO projects(project_id,name,created_by,created_at,updated_at) VALUES($1,'Article integration',$2,$3,$3)`, projectID, userID, now)
	execSeed(`INSERT INTO artifact_blobs(blob_id,project_id,sha256,size_bytes,backend,object_key,reference_count,created_at,updated_at) VALUES($1,$2,repeat('a',64),1,'local',$3,1,$4,$4)`, blobID, projectID, "article-integration/"+blobID, now)
	execSeed(`INSERT INTO artifact_artifacts(artifact_id,project_id,kind,source,name,current_version_id,status,created_by,created_at,updated_at) VALUES($1,$2,'attachment','user_upload','Template',$3,'available',$4,$5,$5)`, artifactID, projectID, versionID, userID, now)
	execSeed(`INSERT INTO artifact_versions(version_id,artifact_id,project_id,version_no,storage_class,blob_id,filename,mime_type,size_bytes,sha256,status,created_by,created_at,available_at) VALUES($1,$2,$3,1,'object',$4,'template.zip','application/zip',1,repeat('a',64),'available',$5,$6,$6)`, versionID, artifactID, projectID, blobID, userID, now)
	execSeed(`INSERT INTO article_templates(template_id,project_id,artifact_id,artifact_version_id,name,template_version,manifest,status,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,'Template','1.0.0','{}'::jsonb,'ready',$5,$6,$6)`, templateID, projectID, artifactID, versionID, userID, now)
	execSeed(`INSERT INTO article_commits(commit_id,project_id,draft_revision,state_vector,yjs_update,tiptap_json,git_commit_sha,previous_git_commit_sha,message,manuscript_sha256,references_sha256,manifest_sha256,created_by,created_at) VALUES($1,$2,1,'','e30=','{}'::jsonb,repeat('b',40),repeat('c',40),'checkpoint',repeat('d',64),repeat('e',64),repeat('f',64),$3,$4)`, commitID, projectID, userID, now)
	execSeed(`INSERT INTO article_builds(build_id,project_id,build_kind,status,commit_id,template_id,template_version_id,engine,bibliography_tool,created_by,created_at,updated_at,finished_at) VALUES ($1,$2,'formal','succeeded',$3,$4,$5,'pdflatex','none',$6,$7,$7,$7), ($8,$2,'formal','succeeded',$3,$4,$5,'pdflatex','none',$6,$7,$7,$7)`, buildOne, projectID, commitID, templateID, versionID, userID, now, buildTwo)
	if err = seed.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = db.ExecContext(cleanup, `DELETE FROM article_releases WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM article_builds WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM article_templates WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM article_commits WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM artifact_artifacts WHERE artifact_id=$1`, artifactID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM artifact_blobs WHERE blob_id=$1`, blobID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM projects WHERE project_id=$1`, projectID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM auth_users WHERE user_id=$1`, userID)
	})

	insert := func(releaseID, buildID string) error {
		_, insertErr := db.ExecContext(ctx, `INSERT INTO article_releases(
			release_id,project_id,commit_id,build_id,template_id,template_version_id,
			tag,title,notes,output_versions,created_by,created_at
		) VALUES($1,$2,$3,$4,$5,$6,'v1.0.0','Paper','','{}'::jsonb,$7,$8)`,
			releaseID, projectID, commitID, buildID, templateID, versionID, userID, now)
		return insertErr
	}
	errorsByInsert := make(chan error, 2)
	var wait sync.WaitGroup
	for _, buildID := range []string{buildOne, buildTwo} {
		wait.Add(1)
		go func(build string) { defer wait.Done(); errorsByInsert <- insert(generator.MustNew(), build) }(buildID)
	}
	wait.Wait()
	close(errorsByInsert)
	succeeded, conflicted := 0, 0
	for insertErr := range errorsByInsert {
		if insertErr == nil {
			succeeded++
			continue
		}
		var pgErr *pgconn.PgError
		if errors.As(insertErr, &pgErr) && pgErr.Code == "23505" {
			conflicted++
			continue
		}
		t.Fatalf("unexpected concurrent release error: %v", insertErr)
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent logical tag was not serialized: success=%d conflict=%d", succeeded, conflicted)
	}

	_, err = db.ExecContext(ctx, `UPDATE article_releases SET title='mutated' WHERE project_id=$1 AND tag='v1.0.0'`, projectID)
	var immutableErr *pgconn.PgError
	if !errors.As(err, &immutableErr) || immutableErr.Code != "55000" {
		t.Fatalf("immutable Release update was not rejected: %v", err)
	}
}
