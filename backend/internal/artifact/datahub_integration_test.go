package artifact

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/datahub"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
)

func TestArtifactDataHubProjectionAndControlledRead(t *testing.T) {
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
	artifactID := generator.MustNew()
	versionID := generator.MustNew()
	blobID := generator.MustNew()
	attachmentID := generator.MustNew()
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC)
	objectKey := "projects/" + projectID + "/blobs/sha256/aa/" +
		strings.Repeat("a", 64)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_users(
			user_id,email,display_name,password_hash,status,created_at,updated_at
		) VALUES($1,$2,'Data Hub Owner','test','active',$3,$3)
	`, userID, userID+"@test.local", now); err != nil {
		t.Fatalf("insert user fixture: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(
			project_id,name,created_by,created_at,updated_at
		) VALUES($1,'Artifact Data Hub',$2,$3,$3)
	`, projectID, userID, now); err != nil {
		t.Fatalf("insert project fixture: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_members(
			project_id,user_id,role,created_at,updated_at
		) VALUES($1,$2,'owner',$3,$3)
	`, projectID, userID, now); err != nil {
		t.Fatalf("insert membership fixture: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifact_blobs(
			blob_id,project_id,sha256,size_bytes,backend,object_key,
			reference_count,created_at,updated_at
		) VALUES($1,$2,$3,4,'local',$4,0,$5,$5)
	`, blobID, projectID, strings.Repeat("a", 64), objectKey, now); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert blob fixture: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifact_artifacts(
			artifact_id,project_id,kind,source,name,tags,description,
			recommended_usage,status,current_version_id,created_by,
			created_at,updated_at
		) VALUES(
			$1,$2,'problem','user_upload','Problem statement',
			ARRAY['source','pdf'],'Original problem','[]','available',
			$3,$4,$5,$5
		)
	`, artifactID, projectID, versionID, userID, now); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert Artifact fixture: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifact_versions(
			version_id,artifact_id,project_id,version_no,storage_class,
			blob_id,filename,mime_type,size_bytes,sha256,status,
			created_by,created_at,available_at
		) VALUES(
			$1,$2,$3,1,'object',$4,'problem.pdf','application/pdf',
			4,$5,'available',$6,$7,$7
		)
	`, versionID, artifactID, projectID, blobID,
		strings.Repeat("a", 64), userID, now); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert Version fixture: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artifact_registry_entries(
			attachment_id,project_id,artifact_id,version_id,source,
			description,recommended_usage,status,created_by,created_at,updated_at
		) VALUES(
			$1,$2,$3,$4,'user_upload','Original problem','[]',
			'active',$5,$6,$6
		)
	`, attachmentID, projectID, artifactID, versionID, userID, now); err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert Artifact fixture: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit Artifact fixture: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cleanupErr := db.BeginTx(context.Background(), nil)
		if cleanupErr != nil {
			t.Errorf("begin cleanup: %v", cleanupErr)
			return
		}
		_, _ = cleanup.ExecContext(
			context.Background(),
			`DELETE FROM data_activity WHERE project_id=$1`,
			projectID,
		)
		_, _ = cleanup.ExecContext(
			context.Background(),
			`DELETE FROM data_objects WHERE project_id=$1`,
			projectID,
		)
		_, _ = cleanup.ExecContext(
			context.Background(),
			`DELETE FROM artifact_artifacts WHERE project_id=$1`,
			projectID,
		)
		_, _ = cleanup.ExecContext(
			context.Background(),
			`DELETE FROM artifact_blobs WHERE project_id=$1`,
			projectID,
		)
		_, _ = cleanup.ExecContext(
			context.Background(),
			`DELETE FROM projects WHERE project_id=$1`,
			projectID,
		)
		_, _ = cleanup.ExecContext(
			context.Background(),
			`DELETE FROM auth_users WHERE user_id=$1`,
			userID,
		)
		if err := cleanup.Commit(); err != nil {
			t.Errorf("commit cleanup: %v", err)
		}
	})

	manager := transaction.Manager{DB: transaction.SQLBeginner{DB: db}}
	artifactStore := PostgresStore{
		DB: db, Generator: generator, Transaction: manager,
	}
	dataStore := datahub.PostgresStore{
		Clock: clock.Fixed{Time: now}, DB: db, Generator: generator,
		Transaction: manager,
	}
	snapshot, err := artifactStore.DataHubSnapshot(ctx, projectID, artifactID)
	if err != nil {
		t.Fatalf("read projection snapshot: %v", err)
	}
	eventProjectID := projectID
	available := contract.EventEnvelope{
		Actor:   map[string]string{"user_id": userID},
		EventID: generator.MustNew(), EventType: "artifact.available",
		OccurredAt: now, Producer: "artifact", ProjectID: &eventProjectID,
		SchemaVersion: 1,
		Payload:       map[string]interface{}{"artifact_id": artifactID},
	}
	if err := dataStore.ProjectArtifact(ctx, available, snapshot); err != nil {
		t.Fatalf("project available Artifact: %v", err)
	}
	if err := dataStore.ProjectArtifact(ctx, available, snapshot); err != nil {
		t.Fatalf("repeat projection must be idempotent: %v", err)
	}
	page, err := dataStore.ListObjects(
		ctx, projectID, "", pagination.Request{Limit: 20},
	)
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("list Artifact projections: %#v, %v", page, err)
	}
	var artifactObject datahub.Object
	var registryObject datahub.Object
	for _, object := range page.Items {
		if object.ObjectType == "artifact" {
			artifactObject = object
		} else if object.ObjectType == "attachment_registry_entry" {
			registryObject = object
		}
	}
	if artifactObject.SourceID != artifactID ||
		artifactObject.Metadata["current_version_id"] != versionID {
		t.Fatalf("unexpected Artifact projection: %#v", artifactObject)
	}
	var activityCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM data_activity WHERE event_id=$1
	`, available.EventID).Scan(&activityCount); err != nil || activityCount != 1 {
		t.Fatalf("idempotent activity count=%d, err=%v", activityCount, err)
	}

	signer, err := NewTransferSigner(
		strings.Repeat("s", 32), "http://core.local",
	)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	projectService := &project.Service{
		Store: project.PostgresStore{DB: db},
	}
	service := &Service{
		Access: projectService, Clock: clock.Fixed{Time: now},
		Signer: signer, Storage: storage, Store: artifactStore,
		TransferTTL: time.Minute,
	}
	content, err := (DataHubReaderAdapter{
		Registry: artifactStore, Service: service,
	}).Read(ctx, auth.Identity{
		Kind: "session", User: auth.User{ID: userID},
	}, artifactObject)
	if err != nil {
		t.Fatalf("read Artifact through authoritative adapter: %v", err)
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "artifact-transfers") ||
		strings.Contains(string(encoded), objectKey) {
		t.Fatalf("controlled read leaked storage state or missed grant: %s", encoded)
	}
	registryContent, err := (DataHubReaderAdapter{
		Registry: artifactStore, Service: service,
	}).Read(ctx, auth.Identity{
		Kind: "session", User: auth.User{ID: userID},
	}, registryObject)
	if err != nil {
		t.Fatalf("read attachment registry through authoritative adapter: %v", err)
	}
	registryEncoded, _ := json.Marshal(registryContent)
	if !strings.Contains(string(registryEncoded), attachmentID) ||
		strings.Contains(string(registryEncoded), objectKey) {
		t.Fatalf("unexpected attachment registry read: %s", registryEncoded)
	}
	if err := service.ValidateProjectReferences(
		ctx, projectID, []string{artifactID},
	); err != nil {
		t.Fatalf("validate same-project available source: %v", err)
	}
	if err := service.ValidateProjectReferences(
		ctx, projectID, []string{generator.MustNew()},
	); err == nil {
		t.Fatal("unavailable source reference must be rejected")
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE projects SET source_artifact_ids=$2::jsonb,updated_at=$3
		WHERE project_id=$1
	`, projectID, jsonBytesForTest([]string{artifactID}), now); err != nil {
		t.Fatalf("attach source Artifact to Project: %v", err)
	}
	problemItems, err := (ProjectHomeReader{
		Projects: projectService, Service: service,
	}).ProblemItems(ctx, auth.Identity{
		Kind: "session", User: auth.User{ID: userID},
	}, projectID)
	if err != nil || len(problemItems) != 1 {
		t.Fatalf("read Project Problem source cards: %#v, %v", problemItems, err)
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE artifact_artifacts
		SET status='trashed',trashed_by=$3,trashed_at=$4,updated_at=$4
		WHERE project_id=$1 AND artifact_id=$2
	`, projectID, artifactID, userID, now.Add(time.Minute)); err != nil {
		t.Fatalf("trash Artifact fixture: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE artifact_registry_entries
		SET status='hidden',updated_at=$3
		WHERE project_id=$1 AND artifact_id=$2
	`, projectID, artifactID, now.Add(time.Minute)); err != nil {
		t.Fatalf("trash Artifact fixture: %v", err)
	}
	snapshot, err = artifactStore.DataHubSnapshot(ctx, projectID, artifactID)
	if err != nil {
		t.Fatal(err)
	}
	deleted := available
	deleted.EventID = generator.MustNew()
	deleted.EventType = "artifact.deleted"
	deleted.OccurredAt = now.Add(time.Minute)
	if err := dataStore.ProjectArtifact(ctx, deleted, snapshot); err != nil {
		t.Fatalf("project Artifact tombstone: %v", err)
	}
	page, err = dataStore.ListObjects(
		ctx, projectID, "", pagination.Request{Limit: 20},
	)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("trashed Artifact must be hidden from data.list: %#v, %v", page, err)
	}
	var hiddenCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM data_objects
		WHERE project_id=$1 AND source_module='artifact' AND status='hidden'
	`, projectID).Scan(&hiddenCount); err != nil || hiddenCount != 2 {
		t.Fatalf("expected two retained tombstones, got %d, %v", hiddenCount, err)
	}
	if err := service.ValidateProjectReferences(
		ctx, projectID, []string{artifactID},
	); err == nil {
		t.Fatal("trashed source reference must be rejected")
	}
	problemItems, err = (ProjectHomeReader{
		Projects: projectService, Service: service,
	}).ProblemItems(ctx, auth.Identity{
		Kind: "session", User: auth.User{ID: userID},
	}, projectID)
	if err != nil || len(problemItems) != 1 {
		t.Fatalf("read unavailable Project source card: %#v, %v", problemItems, err)
	}
	unavailable, _ := json.Marshal(problemItems[0])
	if !strings.Contains(string(unavailable), `"status":"unavailable"`) {
		t.Fatalf("trashed source did not degrade safely: %s", unavailable)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE artifact_artifacts
		SET status='available',trashed_by=NULL,trashed_at=NULL,updated_at=$3
		WHERE project_id=$1 AND artifact_id=$2
	`, projectID, artifactID, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("restore Artifact fixture: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE artifact_registry_entries
		SET status='active',updated_at=$3
		WHERE project_id=$1 AND artifact_id=$2
	`, projectID, artifactID, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("restore registry fixture: %v", err)
	}
	snapshot, err = artifactStore.DataHubSnapshot(ctx, projectID, artifactID)
	if err != nil {
		t.Fatal(err)
	}
	restored := available
	restored.EventID = generator.MustNew()
	restored.OccurredAt = now.Add(2 * time.Minute)
	if err := dataStore.ProjectArtifact(ctx, restored, snapshot); err != nil {
		t.Fatalf("reactivate restored Artifact projection: %v", err)
	}
	page, err = dataStore.ListObjects(
		ctx, projectID, "", pagination.Request{Limit: 20},
	)
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("restored Artifact projections: %#v, %v", page, err)
	}
}

func jsonBytesForTest(value interface{}) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}
