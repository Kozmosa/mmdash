package artifact

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
)

func TestPostgresLocalMultipartLifecycle(t *testing.T) {
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
	viewerID := generator.MustNew()
	projectID := generator.MustNew()
	agentInstanceID := generator.MustNew()
	folderID := generator.MustNew()
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_users(
			user_id,email,display_name,password_hash,status,created_at,updated_at
		) VALUES
		  ($1,$2,'Artifact Owner','test','active',$4,$4),
		  ($3,$5,'Artifact Viewer','test','active',$4,$4)
	`, userID, userID+"@test.local", viewerID, now,
		viewerID+"@test.local"); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(
			project_id,name,created_by,created_at,updated_at
		) VALUES($1,'Artifact integration',$2,$3,$3)
	`, projectID, userID, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO artifact_folders(
			folder_id,project_id,name,position,created_at,updated_at
		) VALUES($1,$2,'Article',0,$3,$3)
	`, folderID, projectID, now); err != nil {
		t.Fatalf("insert Artifact folder: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_members(
			project_id,user_id,role,created_at,updated_at
		) VALUES
		  ($1,$2,'owner',$3,$3),
		  ($1,$4,'viewer',$3,$3)
	`, projectID, userID, now, viewerID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO agent_instances(
			agent_instance_id,adapter_type,display_name,management_mode,
			runtime_url,status,created_by,created_at,updated_at
		) VALUES($1,'hermes','Artifact Agent','manual',
			'http://127.0.0.1:8642','active',$2,$3,$3)
	`, agentInstanceID, userID, now); err != nil {
		t.Fatalf("insert Agent instance: %v", err)
	}
	repositoryID := generator.MustNew()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO repo_repositories(
			repository_id,project_id,provider,canonical_remote_url,
			display_name,storage_key,default_branch,status,settings_version,
			webhook_id,connected_at,created_by,created_at,updated_at
		) VALUES(
			$1,$2,'local','C:/artifact-integration','Artifact integration',
			$3,'main','ready',1,$4,$5,$6,$5,$5
		)
	`, repositoryID, projectID, generator.MustNew(), generator.MustNew(),
		now, userID); err != nil {
		t.Fatalf("insert repository: %v", err)
	}
	t.Cleanup(func() {
		tx, txErr := db.BeginTx(context.Background(), nil)
		if txErr != nil {
			t.Errorf("begin cleanup: %v", txErr)
			return
		}
		_, _ = tx.ExecContext(
			context.Background(),
			`DELETE FROM system_outbox WHERE project_id=$1`,
			projectID,
		)
		_, _ = tx.ExecContext(
			context.Background(),
			`DELETE FROM artifact_artifacts WHERE project_id=$1`,
			projectID,
		)
		_, _ = tx.ExecContext(
			context.Background(),
			`DELETE FROM artifact_blobs WHERE project_id=$1`,
			projectID,
		)
		_, _ = tx.ExecContext(
			context.Background(),
			`DELETE FROM repo_repositories WHERE project_id=$1`,
			projectID,
		)
		_, _ = tx.ExecContext(
			context.Background(),
			`DELETE FROM agent_instances WHERE agent_instance_id=$1`,
			agentInstanceID,
		)
		_, _ = tx.ExecContext(
			context.Background(),
			`DELETE FROM projects WHERE project_id=$1`,
			projectID,
		)
		_, _ = tx.ExecContext(
			context.Background(),
			`DELETE FROM auth_users WHERE user_id IN ($1,$2)`,
			userID, viewerID,
		)
		if err := tx.Commit(); err != nil {
			t.Errorf("commit cleanup: %v", err)
		}
	})

	projectService := &project.Service{
		AgentGrants: artifactAgentGrantResolver{},
		Store:       project.PostgresStore{DB: db},
	}
	store := PostgresStore{
		DB: db, Generator: generator,
		Outbox: &outbox.Writer{
			Clock: clock.Fixed{Time: now}, Generator: generator,
		},
		Transaction: transaction.Manager{
			DB: transaction.SQLBeginner{DB: db},
		},
	}
	service := newLocalTestService(t, store, projectService, now)
	resultExperimentID := generator.MustNew()
	resultBytes := makeResultArchive(t, resultExperimentID, []byte("summary"))
	resultFolderPath := []string{"experiment", strings.Repeat("a", 40) + "_20260730T100000.000000Z"}
	const folderWorkers = 8
	folderIDs := make(chan string, folderWorkers)
	folderErrors := make(chan error, folderWorkers)
	var folderGroup sync.WaitGroup
	for range folderWorkers {
		folderGroup.Add(1)
		go func() {
			defer folderGroup.Done()
			leaf, ensureErr := store.EnsureFolderPath(ctx, projectID, resultFolderPath)
			if ensureErr != nil {
				folderErrors <- ensureErr
				return
			}
			folderIDs <- leaf.ID
		}()
	}
	folderGroup.Wait()
	close(folderIDs)
	close(folderErrors)
	for ensureErr := range folderErrors {
		t.Fatalf("concurrent managed folder ensure: %v", ensureErr)
	}
	var managedLeafID string
	for folderID := range folderIDs {
		if managedLeafID == "" {
			managedLeafID = folderID
		} else if folderID != managedLeafID {
			t.Fatalf("concurrent folder creation returned different leaves: %s and %s", managedLeafID, folderID)
		}
	}
	resultDetail, err := service.ArchiveExperimentResult(ctx, projectID, resultExperimentID, userID, resultFolderPath, digest(resultBytes), int64(len(resultBytes)), bytes.NewReader(resultBytes))
	if err != nil || resultDetail.CurrentVersion == nil || resultDetail.CurrentVersion.Filename != "execution-bundle.zip" || resultDetail.Artifact.Kind != KindExperimentResult {
		t.Fatalf("archive experiment result: %#v, %v", resultDetail, err)
	}
	if resultDetail.Artifact.SourceObjectID == nil || *resultDetail.Artifact.SourceObjectID != resultExperimentID {
		t.Fatalf("archive experiment result source relation: %#v", resultDetail.Artifact.SourceObjectID)
	}
	if resultDetail.Artifact.FolderID == nil {
		t.Fatalf("archive experiment result was left at the Artifact root: %#v", resultDetail.Artifact)
	}
	folderTree, err := store.GetFolderTree(ctx, projectID)
	if err != nil || len(folderTree.Items) != 1 || folderTree.Items[0].Name != "experiment" ||
		len(folderTree.Items[0].Children) != 1 || folderTree.Items[0].Children[0].Name != resultFolderPath[1] ||
		folderTree.Items[0].Children[0].ID != managedLeafID ||
		managedLeafID != *resultDetail.Artifact.FolderID {
		t.Fatalf("unexpected managed Experiment folder tree: %#v, %v", folderTree, err)
	}
	owner := auth.Identity{
		Kind: "session", User: auth.User{ID: userID},
	}
	viewer := auth.Identity{
		Kind: "session", User: auth.User{ID: viewerID},
	}
	agentIdentity := auth.Identity{
		AgentInstanceID:  agentInstanceID,
		AllowedTools:     []string{"artifact.upload"},
		CredentialStatus: "active",
		Kind:             "agent",
		ProjectID:        projectID,
		User:             auth.User{ID: userID},
	}

	contents := make([]byte, 6*1024*1024)
	for index := range contents {
		contents[index] = byte(index % 251)
	}
	input := InitializeUploadInput{
		Filename: "dataset.bin", Name: "Dataset",
		SizeBytes: int64(len(contents)), SHA256: digest(contents),
		MIMEType: "application/octet-stream", Kind: KindAttachment,
		Tags: []string{"raw"}, FolderID: &folderID,
		IdempotencyKey: "multipart-1",
	}
	if _, err := service.Initialize(ctx, viewer, projectID, input); !errors.Is(
		err, ErrForbidden,
	) {
		t.Fatalf("viewer should not initialize uploads: %v", err)
	}
	upload, err := service.Initialize(ctx, owner, projectID, input)
	if err != nil {
		t.Fatalf("initialize multipart: %v", err)
	}
	initializedArtifact, err := store.GetArtifact(ctx, projectID, upload.ArtifactID)
	if err != nil || initializedArtifact.FolderID == nil ||
		*initializedArtifact.FolderID != folderID {
		t.Fatalf("initialize did not assign folder atomically: %#v, %v", initializedArtifact, err)
	}
	repeated, err := service.Initialize(ctx, owner, projectID, input)
	if err != nil || repeated.UploadID != upload.UploadID {
		t.Fatalf("idempotent initialize: %#v, %v", repeated, err)
	}
	differentPlacement := input
	differentPlacement.FolderID = nil
	if _, err := service.Initialize(ctx, owner, projectID, differentPlacement); !errors.Is(err, ErrUploadConflict) {
		t.Fatalf("idempotency key accepted a different folder: %v", err)
	}
	grants, err := service.SignParts(
		ctx, owner, projectID, upload.UploadID, []int{2, 1},
	)
	if err != nil {
		t.Fatalf("sign out-of-order parts: %v", err)
	}
	part2 := uploadGrantedPart(
		t, service, grants.Items[0], contents[MultipartMinPartBytes:],
	)
	part1 := uploadGrantedPart(
		t, service, grants.Items[1], contents[:MultipartMinPartBytes],
	)
	recovered, err := service.GetUpload(ctx, owner, projectID, upload.UploadID)
	if err != nil || len(recovered.CompletedParts) != 2 {
		t.Fatalf("recover upload session: %#v, %v", recovered, err)
	}
	if _, _, err := service.Confirm(
		ctx, owner, projectID, upload.UploadID,
		[]ConfirmPart{{PartNumber: 1, ETag: part1.ETag}},
	); !errors.Is(err, ErrUploadIncomplete) {
		t.Fatalf("expected missing part rejection, got %v", err)
	}
	if _, _, err := service.Confirm(
		ctx, owner, projectID, upload.UploadID,
		[]ConfirmPart{
			{PartNumber: 1, ETag: "forged"},
			{PartNumber: 2, ETag: part2.ETag},
		},
	); !errors.Is(err, ErrPartInvalid) {
		t.Fatalf("expected forged ETag rejection, got %v", err)
	}
	confirmParts := []ConfirmPart{
		{PartNumber: 1, ETag: part1.ETag},
		{PartNumber: 2, ETag: part2.ETag},
	}
	type confirmResult struct {
		detail  Detail
		created bool
		err     error
	}
	confirmResults := make(chan confirmResult, 2)
	for index := 0; index < 2; index++ {
		go func() {
			resultDetail, resultCreated, resultErr := service.Confirm(
				ctx, owner, projectID, upload.UploadID, confirmParts,
			)
			confirmResults <- confirmResult{
				detail: resultDetail, created: resultCreated, err: resultErr,
			}
		}()
	}
	var detail Detail
	createdCount := 0
	successCount := 0
	for index := 0; index < 2; index++ {
		result := <-confirmResults
		if result.err != nil {
			if !errors.Is(result.err, ErrUploadConflict) {
				t.Fatalf("unexpected concurrent confirm error: %v", result.err)
			}
			continue
		}
		successCount++
		if result.created {
			createdCount++
			detail = result.detail
		}
	}
	if successCount < 1 || createdCount != 1 ||
		detail.CurrentVersion == nil ||
		detail.CurrentVersion.Status != StatusAvailable {
		t.Fatalf(
			"concurrent confirm success=%d created=%d detail=%#v",
			successCount, createdCount, detail,
		)
	}
	repeatedDetail, created, err := service.Confirm(
		ctx, owner, projectID, upload.UploadID,
		confirmParts,
	)
	if err != nil || created ||
		repeatedDetail.Artifact.ID != detail.Artifact.ID {
		t.Fatalf(
			"idempotent confirm: %#v, created=%v, err=%v",
			repeatedDetail, created, err,
		)
	}

	deduplicatedInput := input
	deduplicatedInput.Name = "Dataset copy"
	deduplicatedInput.IdempotencyKey = "deduplicated-2"
	deduplicated, err := service.Initialize(
		ctx, owner, projectID, deduplicatedInput,
	)
	if err != nil || deduplicated.UploadMode != "deduplicated" ||
		deduplicated.Status != UploadCompleted {
		t.Fatalf("project-local deduplication: %#v, %v", deduplicated, err)
	}

	agentContents := []byte("Agent-generated plot bytes")
	agentInput := InitializeUploadInput{
		Filename: "agent-plot.png", Name: "Agent plot",
		SizeBytes: int64(len(agentContents)), SHA256: digest(agentContents),
		MIMEType: "image/png", Kind: KindAgent,
		Tags: []string{"agent-output"}, IdempotencyKey: "agent-artifact-1",
	}
	if _, err := service.Initialize(ctx, owner, projectID, agentInput); !errors.Is(
		err, ErrKindInvalid,
	) {
		t.Fatalf("human forged Agent Artifact kind: %v", err)
	}
	agentUpload, err := service.Initialize(ctx, agentIdentity, projectID, agentInput)
	if err != nil {
		t.Fatalf("initialize Agent Artifact: %v", err)
	}
	otherAgent := agentIdentity
	otherAgent.AgentInstanceID = generator.MustNew()
	if _, err := service.SignParts(
		ctx, otherAgent, projectID, agentUpload.UploadID, []int{1},
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("another Agent instance continued upload: %v", err)
	}
	agentGrants, err := service.SignParts(
		ctx, agentIdentity, projectID, agentUpload.UploadID, []int{1},
	)
	if err != nil {
		t.Fatalf("sign Agent Artifact part: %v", err)
	}
	agentPart := uploadGrantedPart(t, service, agentGrants.Items[0], agentContents)
	agentDetail, created, err := service.Confirm(
		ctx, agentIdentity, projectID, agentUpload.UploadID,
		[]ConfirmPart{{PartNumber: 1, ETag: agentPart.ETag}},
	)
	if err != nil || !created || agentDetail.Artifact.Kind != KindAgent ||
		agentDetail.Artifact.Source != SourceAgent {
		t.Fatalf("complete Agent Artifact: %#v, created=%v, err=%v", agentDetail, created, err)
	}
	var storedAgentInstanceID string
	if err := db.QueryRowContext(ctx, `
		SELECT agent_instance_id FROM artifact_uploads WHERE upload_id=$1
	`, agentUpload.UploadID).Scan(&storedAgentInstanceID); err != nil ||
		storedAgentInstanceID != agentInstanceID {
		t.Fatalf("Agent upload provenance: %q, %v", storedAgentInstanceID, err)
	}

	versionUpload, err := service.InitializeVersion(
		ctx, owner, projectID, detail.Artifact.ID,
		InitializeVersionInput{
			Filename: "dataset-v2.bin", SizeBytes: int64(len(contents)),
			SHA256: digest(contents), MIMEType: "application/octet-stream",
			IdempotencyKey: "version-dedup",
		},
	)
	if err != nil || versionUpload.UploadMode != "deduplicated" {
		t.Fatalf("deduplicated new version: %#v, %v", versionUpload, err)
	}
	restored, err := service.RestoreVersion(
		ctx, owner, projectID, detail.Artifact.ID,
		detail.CurrentVersion.ID, "restore-v1",
	)
	if err != nil {
		t.Fatalf("restore historical version: %v", err)
	}
	restoredAgain, err := service.RestoreVersion(
		ctx, owner, projectID, detail.Artifact.ID,
		detail.CurrentVersion.ID, "restore-v1",
	)
	if err != nil ||
		*restoredAgain.Artifact.CurrentVersionID !=
			*restored.Artifact.CurrentVersionID {
		t.Fatalf("idempotent version restore: %#v, %v", restoredAgain, err)
	}
	versions, err := service.ListVersions(
		ctx, owner, projectID, detail.Artifact.ID,
	)
	if err != nil || len(versions.Items) != 3 {
		t.Fatalf("retained version history: %#v, %v", versions, err)
	}

	var blobID, objectKey string
	var references int64
	if err := db.QueryRowContext(ctx, `
		SELECT blob_id,object_key,reference_count
		FROM artifact_blobs
		WHERE project_id=$1 AND sha256=$2 AND size_bytes=$3
	`, projectID, input.SHA256, input.SizeBytes).Scan(
		&blobID, &objectKey, &references,
	); err != nil {
		t.Fatalf("read deduplicated blob: %v", err)
	}
	if references != 4 {
		t.Fatalf("expected four retained references, got %d", references)
	}
	if err := service.Trash(ctx, owner, projectID, detail.Artifact.ID); err != nil {
		t.Fatalf("trash first Artifact: %v", err)
	}
	restoredArtifact, err := service.Restore(
		ctx, owner, projectID, detail.Artifact.ID,
	)
	if err != nil ||
		restoredArtifact.Artifact.Status != StatusAvailable ||
		restoredArtifact.CurrentVersion == nil {
		t.Fatalf("restore trashed Artifact: %#v, %v", restoredArtifact, err)
	}
	if err := service.Trash(ctx, owner, projectID, detail.Artifact.ID); err != nil {
		t.Fatalf("trash restored Artifact: %v", err)
	}
	if err := service.Purge(ctx, owner, projectID, detail.Artifact.ID); err != nil {
		t.Fatalf("purge first Artifact: %v", err)
	}
	if _, err := service.Storage.Stat(ctx, objectKey); err != nil {
		t.Fatalf("shared object must survive first purge: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT reference_count FROM artifact_blobs WHERE blob_id=$1
	`, blobID).Scan(&references); err != nil || references != 1 {
		t.Fatalf("expected one remaining reference, got %d, %v", references, err)
	}
	if err := service.Trash(
		ctx, owner, projectID, deduplicated.ArtifactID,
	); err != nil {
		t.Fatalf("trash second Artifact: %v", err)
	}
	if err := service.Purge(
		ctx, owner, projectID, deduplicated.ArtifactID,
	); err != nil {
		t.Fatalf("purge second Artifact: %v", err)
	}
	if _, err := service.Storage.Stat(ctx, objectKey); !errors.Is(
		err, ErrObjectNotFound,
	) {
		t.Fatalf("unreferenced object should be removed, got %v", err)
	}

	gitReference := GitReference{
		RepositoryID: repositoryID, Workspace: "result",
		CommitSHA: strings.Repeat("a", 40), Path: "result/summary.txt",
	}
	gitContents := "immutable Git-backed Artifact"
	service.Git = staticGitReader{
		contents: gitContents, projectID: projectID, reference: gitReference,
	}
	gitDetail, err := service.RegisterGit(
		ctx, owner, projectID, RegisterGitInput{
			Name: "Git result", Filename: "summary.txt", MIMEType: "text/plain",
			Kind: KindOther, Source: SourceSystem,
			SourceObjectID: repositoryID, GitReference: gitReference,
			Tags: []string{"result"},
		},
	)
	if err != nil ||
		gitDetail.CurrentVersion == nil ||
		gitDetail.CurrentVersion.StorageClass != "git" {
		t.Fatalf("register Git Artifact: %#v, %v", gitDetail, err)
	}
	storedGit, err := store.GetDetail(
		ctx, projectID, gitDetail.Artifact.ID, false,
	)
	if err != nil ||
		storedGit.Artifact.SourceObjectID == nil ||
		*storedGit.Artifact.SourceObjectID != repositoryID {
		t.Fatalf("read Git source object: %#v, %v", storedGit, err)
	}
	gitDownload, err := service.Download(
		ctx, owner, projectID, gitDetail.Artifact.ID, "",
	)
	if err != nil {
		t.Fatalf("sign Git Artifact download: %v", err)
	}
	gitReader, gitVersion, err := service.OpenSignedDownload(
		ctx, grantToken(t, gitDownload.Transfer),
	)
	if err != nil || gitVersion.ID != gitDetail.CurrentVersion.ID {
		t.Fatalf("open Git Artifact download: %#v, %v", gitVersion, err)
	}
	gitBytes, readErr := io.ReadAll(gitReader)
	_ = gitReader.Close()
	if readErr != nil || string(gitBytes) != gitContents {
		t.Fatalf("read Git Artifact bytes: %q, %v", gitBytes, readErr)
	}
	if err := service.Trash(
		ctx, owner, projectID, gitDetail.Artifact.ID,
	); err != nil {
		t.Fatalf("trash Git Artifact: %v", err)
	}
	if err := service.Purge(
		ctx, owner, projectID, gitDetail.Artifact.ID,
	); err != nil {
		t.Fatalf("purge Git Artifact: %v", err)
	}

	hashMismatchContents := []byte("actual bytes")
	hashMismatchInput := InitializeUploadInput{
		Filename: "hash-mismatch.txt", SizeBytes: int64(len(hashMismatchContents)),
		SHA256: digest([]byte("different bytes")), MIMEType: "text/plain",
		Kind: KindAttachment, IdempotencyKey: "hash-mismatch",
	}
	hashUpload, err := service.Initialize(
		ctx, owner, projectID, hashMismatchInput,
	)
	if err != nil {
		t.Fatalf("initialize hash mismatch upload: %v", err)
	}
	hashGrants, err := service.SignParts(
		ctx, owner, projectID, hashUpload.UploadID, []int{1},
	)
	if err != nil {
		t.Fatalf("sign hash mismatch part: %v", err)
	}
	hashPart := uploadGrantedPart(
		t, service, hashGrants.Items[0], hashMismatchContents,
	)
	if _, _, err := service.Confirm(
		ctx, owner, projectID, hashUpload.UploadID,
		[]ConfirmPart{{PartNumber: 1, ETag: hashPart.ETag}},
	); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("expected full SHA-256 mismatch, got %v", err)
	}
	failedUpload, err := store.GetUpload(
		ctx, projectID, hashUpload.UploadID,
	)
	if err != nil || failedUpload.Status != UploadFailed {
		t.Fatalf("hash mismatch should fail upload: %#v, %v", failedUpload, err)
	}

	cancelInput := InitializeUploadInput{
		Filename: "cancel.txt", SizeBytes: 4,
		SHA256: digest([]byte("stop")), MIMEType: "text/plain",
		Kind: KindAttachment, IdempotencyKey: "cancel-upload",
	}
	cancelUpload, err := service.Initialize(ctx, owner, projectID, cancelInput)
	if err != nil {
		t.Fatalf("initialize cancelled upload: %v", err)
	}
	if err := service.Abort(
		ctx, owner, projectID, cancelUpload.UploadID,
	); err != nil {
		t.Fatalf("abort upload: %v", err)
	}
	if err := service.Abort(
		ctx, owner, projectID, cancelUpload.UploadID,
	); err != nil {
		t.Fatalf("repeat abort upload: %v", err)
	}
	abortedUpload, err := store.GetUpload(
		ctx, projectID, cancelUpload.UploadID,
	)
	if err != nil || abortedUpload.Status != UploadAborted ||
		!strings.HasPrefix(abortedUpload.ProviderUploadID, "aborted:") {
		t.Fatalf("aborted upload state: %#v, %v", abortedUpload, err)
	}

	expiredInput := InitializeUploadInput{
		Filename: "expired.txt", SizeBytes: 7,
		SHA256: digest([]byte("expired")), MIMEType: "text/plain",
		Kind: KindAttachment, IdempotencyKey: "expired-upload",
	}
	expiredUpload, err := service.Initialize(ctx, owner, projectID, expiredInput)
	if err != nil {
		t.Fatalf("initialize expiring upload: %v", err)
	}
	service.Clock = clock.Fixed{Time: now.Add(2 * time.Hour)}
	expiredCount, err := service.ExpireUploads(ctx, 10)
	if err != nil || expiredCount != 1 {
		t.Fatalf("expire upload: count=%d, err=%v", expiredCount, err)
	}
	expiredState, err := store.GetUpload(
		ctx, projectID, expiredUpload.UploadID,
	)
	if err != nil || expiredState.Status != UploadExpired ||
		!strings.HasPrefix(expiredState.ProviderUploadID, "aborted:") {
		t.Fatalf("expired upload state: %#v, %v", expiredState, err)
	}
	for eventType, minimum := range map[string]int{
		"artifact.created":   6,
		"artifact.available": 6,
		"artifact.deleted":   4,
	} {
		var count int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM system_outbox
			WHERE project_id=$1 AND event_type=$2
		`, projectID, eventType).Scan(&count); err != nil {
			t.Fatalf("count %s events: %v", eventType, err)
		}
		if count < minimum {
			t.Fatalf("expected at least %d %s events, got %d", minimum, eventType, count)
		}
	}
}

type artifactAgentGrantResolver struct{}

func (artifactAgentGrantResolver) ResolveAgentRole(
	context.Context,
	string,
	string,
) (project.Role, error) {
	return project.RoleAgent, nil
}

func makeResultArchive(t *testing.T, experimentID string, contents []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	manifest := `{"schema_version":"1","experiment_id":"` + experimentID + `","status":"succeeded","files":[{"path":"summary.md","sha256":"` + digest(contents) + `","size_bytes":` + fmt.Sprint(len(contents)) + `,"kind":"summary"}]}`
	manifestEntry, err := archive.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifestEntry.Write([]byte(manifest)); err != nil {
		t.Fatal(err)
	}
	fileEntry, err := archive.Create("summary.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileEntry.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
