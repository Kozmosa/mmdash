package artifact

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/jobs"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
)

func TestPostgresLocalPreviewJobLifecycle(t *testing.T) {
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
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM artifact_artifacts WHERE project_id=$1`,
			projectID,
		)
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM artifact_blobs WHERE project_id=$1`,
			projectID,
		)
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM projects WHERE project_id=$1`,
			projectID,
		)
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM auth_users WHERE user_id=$1`,
			userID,
		)
	})
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_users(
			user_id,email,display_name,password_hash,status,created_at,updated_at
		) VALUES($1,$2,'Preview Owner','test','active',$3,$3)
	`, userID, userID+"@test.local", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(
			project_id,name,created_by,created_at,updated_at
		) VALUES($1,'Preview integration',$2,$3,$3)
	`, projectID, userID, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_members(
			project_id,user_id,role,created_at,updated_at
		) VALUES($1,$2,'owner',$3,$3)
	`, projectID, userID, now); err != nil {
		t.Fatalf("insert project member: %v", err)
	}

	fixedClock := clock.Fixed{Time: now}
	manager := transaction.Manager{DB: transaction.SQLBeginner{DB: db}}
	outboxWriter := outbox.Writer{Clock: fixedClock, Generator: generator}
	jobStore := jobs.PostgresStore{
		Clock: fixedClock, DB: db, Generator: generator,
		Outbox: outboxWriter, Transaction: manager,
	}
	artifactStore := PostgresStore{
		DB: db, Generator: generator, Jobs: jobStore, Transaction: manager,
	}
	local, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create local store: %v", err)
	}
	signer, err := NewTransferSigner(
		strings.Repeat("p", 32), "http://localhost:8080",
	)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	workerSigner, err := NewTransferSigner(
		strings.Repeat("p", 32), "http://core:8080",
	)
	if err != nil {
		t.Fatalf("create Worker signer: %v", err)
	}
	projectService := &project.Service{
		Store: project.PostgresStore{DB: db},
	}
	artifactService := Service{
		Access: projectService, Clock: fixedClock, Generator: generator,
		MaxPreviewOutputBytes: 1024 * 1024, MaxUploadBytes: 1024 * 1024,
		MultipartPartBytes: MultipartMinPartBytes, Signer: signer,
		Storage: local, Store: artifactStore,
		TransferTTL: time.Minute, UploadSessionTTL: time.Hour,
		WorkerSigner: workerSigner,
	}
	hook := artifactService
	jobStore.Hooks = []jobs.LifecycleHook{hook}
	jobService := jobs.Service{
		Clock: fixedClock, Hooks: []jobs.LifecycleHook{hook},
		Projects: projectService, Store: jobStore,
	}
	artifactService.Jobs = jobService

	owner := auth.Identity{
		Kind: "session", User: auth.User{ID: userID},
	}
	worker := auth.Identity{
		Kind: "api", User: auth.User{ID: userID, SystemRole: "admin"},
	}
	contents := []byte("name,value\nalpha,1\nbeta,2\n")
	upload, err := artifactService.Initialize(
		ctx, owner, projectID, InitializeUploadInput{
			Filename: "preview.csv", Name: "Preview CSV",
			SizeBytes: int64(len(contents)), SHA256: digest(contents),
			MIMEType: "text/csv", Kind: KindAttachment,
			IdempotencyKey: "preview-integration",
		},
	)
	if err != nil {
		t.Fatalf("initialize upload: %v", err)
	}
	grants, err := artifactService.SignParts(
		ctx, owner, projectID, upload.UploadID, []int{1},
	)
	if err != nil {
		t.Fatalf("sign upload: %v", err)
	}
	part := uploadGrantedPart(t, artifactService, grants.Items[0], contents)
	detail, _, err := artifactService.Confirm(
		ctx, owner, projectID, upload.UploadID,
		[]ConfirmPart{{PartNumber: 1, ETag: part.ETag}},
	)
	if err != nil || detail.CurrentVersion == nil {
		t.Fatalf("confirm upload: %#v %v", detail, err)
	}

	job, err := jobService.Claim(ctx, worker, jobs.ClaimInput{
		JobTypes:     []string{previewJobType},
		LeaseSeconds: 60, WorkerID: "preview-worker",
	})
	if err != nil || job == nil {
		t.Fatalf("claim preview job: %#v %v", job, err)
	}
	if _, err := artifactService.PreviewJobTransfer(
		ctx, owner, job.ID, PreviewTransferInput{
			Direction: previewTransferInput,
			VersionID: detail.CurrentVersion.ID,
		},
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("session must not receive Worker transfer: %v", err)
	}
	inputGrant, err := artifactService.PreviewJobTransfer(
		ctx, worker, job.ID, PreviewTransferInput{
			Direction: previewTransferInput,
			VersionID: detail.CurrentVersion.ID,
		},
	)
	if err != nil {
		t.Fatalf("sign preview input: %v", err)
	}
	if !strings.HasPrefix(
		inputGrant.URL, "http://core:8080/v1/artifact-transfers/",
	) {
		t.Fatalf("preview input did not use the internal Core URL: %s", inputGrant.URL)
	}
	reader, metadata, err := artifactService.OpenSignedTransfer(
		ctx, grantToken(t, inputGrant),
	)
	if err != nil {
		t.Fatalf("open preview input: %v", err)
	}
	actual, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil ||
		!bytes.Equal(actual, contents) ||
		metadata.SizeBytes != int64(len(contents)) {
		t.Fatalf(
			"preview input mismatch: size=%d read=%v close=%v",
			metadata.SizeBytes, readErr, closeErr,
		)
	}

	thumbnail := []byte("\x89PNG\r\n\x1a\nbounded-preview")
	outputGrant, err := artifactService.PreviewJobTransfer(
		ctx, worker, job.ID, PreviewTransferInput{
			Direction:   previewTransferOutput,
			VersionID:   detail.CurrentVersion.ID,
			PreviewType: PreviewThumbnail, Filename: "thumbnail.png",
			MIMEType: "image/png", SizeBytes: int64(len(thumbnail)),
			SHA256: digest(thumbnail),
		},
	)
	if err != nil {
		t.Fatalf("sign preview output: %v", err)
	}
	outputPart, err := artifactService.PutSignedTransfer(
		ctx, grantToken(t, outputGrant), bytes.NewReader(thumbnail),
		int64(len(thumbnail)),
	)
	if err != nil {
		t.Fatalf("put preview output: %v", err)
	}
	result := map[string]interface{}{
		"project_id": projectID, "artifact_id": detail.Artifact.ID,
		"version_id":   detail.CurrentVersion.ID,
		"preview_id":   job.Payload["preview_id"],
		"preview_type": PreviewCSV, "status": PreviewAvailable,
		"structural_summary": map[string]interface{}{
			"columns": []interface{}{"name", "value"}, "row_count": float64(2),
		},
		"error_code": nil,
		"outputs": []interface{}{map[string]interface{}{
			"preview_type": PreviewThumbnail, "etag": "forged",
		}},
	}
	if _, err := jobService.Complete(
		ctx, worker, job.ID, "preview-worker", result,
	); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatalf("forged preview ETag must fail: %v", err)
	}
	result["outputs"] = []interface{}{map[string]interface{}{
		"preview_type": PreviewThumbnail, "etag": outputPart.ETag,
	}}
	completed, err := jobService.Complete(
		ctx, worker, job.ID, "preview-worker", result,
	)
	if err != nil || completed.Status != jobs.StatusSucceeded {
		t.Fatalf("complete preview job: %#v %v", completed, err)
	}

	previews, err := artifactService.ListPreviews(
		ctx, owner, projectID, detail.Artifact.ID, detail.CurrentVersion.ID,
	)
	if err != nil || len(previews.Items) != 2 {
		t.Fatalf("list previews: %#v %v", previews, err)
	}
	var thumbnailPreview Preview
	for _, preview := range previews.Items {
		if preview.PreviewType == PreviewThumbnail {
			thumbnailPreview = preview
		}
	}
	if thumbnailPreview.Status != PreviewAvailable ||
		thumbnailPreview.Transfer == nil {
		t.Fatalf("thumbnail preview is unavailable: %#v", thumbnailPreview)
	}
	reader, _, err = artifactService.OpenSignedTransfer(
		ctx, grantToken(t, *thumbnailPreview.Transfer),
	)
	if err != nil {
		t.Fatalf("open thumbnail: %v", err)
	}
	downloaded, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil || !bytes.Equal(downloaded, thumbnail) {
		t.Fatalf("thumbnail bytes mismatch: %v", readErr)
	}
	var references int
	if err := db.QueryRowContext(ctx, `
		SELECT reference_count
		FROM artifact_blobs
		WHERE project_id=$1 AND sha256=$2
	`, projectID, digest(thumbnail)).Scan(&references); err != nil ||
		references != 1 {
		t.Fatalf("thumbnail blob reference count: %d %v", references, err)
	}

	restored, err := artifactService.RestoreVersion(
		ctx, owner, projectID, detail.Artifact.ID, detail.CurrentVersion.ID,
		"preview-expiry-version",
	)
	if err != nil || restored.CurrentVersion == nil {
		t.Fatalf("restore Version for expiry test: %#v %v", restored, err)
	}
	expiryJob, err := jobService.Claim(ctx, worker, jobs.ClaimInput{
		JobTypes:     []string{previewJobType},
		LeaseSeconds: 60, WorkerID: "preview-expiry-worker",
	})
	if err != nil || expiryJob == nil {
		t.Fatalf("claim expiry preview job: %#v %v", expiryJob, err)
	}
	expiringContents := []byte("\x89PNG\r\n\x1a\nexpiring")
	expiringGrant, err := artifactService.PreviewJobTransfer(
		ctx, worker, expiryJob.ID, PreviewTransferInput{
			Direction:   previewTransferOutput,
			VersionID:   restored.CurrentVersion.ID,
			PreviewType: PreviewThumbnail, Filename: "expiring.png",
			MIMEType: "image/png", SizeBytes: int64(len(expiringContents)),
			SHA256: digest(expiringContents),
		},
	)
	if err != nil {
		t.Fatalf("create expiring preview transfer: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE artifact_preview_transfers SET expires_at=$2
		WHERE job_id=$1
	`, expiryJob.ID, now); err != nil {
		t.Fatalf("expire preview transfer fixture: %v", err)
	}
	expired, err := artifactService.ExpirePreviewTransfers(ctx, 10)
	if err != nil || expired != 1 {
		t.Fatalf("expire preview transfer: count=%d err=%v", expired, err)
	}
	expiredTransfer, err := artifactStore.GetPreviewTransfer(
		ctx, expiryJob.ID, PreviewThumbnail,
	)
	if err != nil ||
		expiredTransfer.Status != "expired" ||
		expiredTransfer.AbortedAt == nil {
		t.Fatalf("expired preview transfer state: %#v %v", expiredTransfer, err)
	}
	if _, err := artifactService.PutSignedTransfer(
		ctx, grantToken(t, expiringGrant), bytes.NewReader(expiringContents),
		int64(len(expiringContents)),
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired preview grant must be rejected: %v", err)
	}
}
