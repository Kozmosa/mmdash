package artifact

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
)

func TestPostgresMinIOMultipartServiceLifecycle(t *testing.T) {
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	endpoint := os.Getenv("MMDASH_TEST_MINIO_ENDPOINT")
	accessKey := os.Getenv("MMDASH_TEST_MINIO_ACCESS_KEY")
	secretKey := os.Getenv("MMDASH_TEST_MINIO_SECRET_KEY")
	if databaseURL == "" || endpoint == "" || accessKey == "" || secretKey == "" {
		t.Skip("PostgreSQL and MinIO integration settings are not configured")
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
	storage, err := NewS3BlobStore(S3BlobStoreConfig{
		AccessKey: accessKey, SecretKey: secretKey,
		Backend: "minio", Bucket: "mmdash",
		Endpoint: endpoint, PublicEndpoint: endpoint, Region: "us-east-1",
	})
	if err != nil {
		t.Fatalf("create MinIO store: %v", err)
	}
	if err := storage.Check(ctx); err != nil {
		t.Fatalf("check MinIO store: %v", err)
	}

	generator := identity.Generator{}
	userID := generator.MustNew()
	projectID := generator.MustNew()
	now := time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO auth_users(
			user_id,email,display_name,password_hash,status,created_at,updated_at
		) VALUES($1,$2,'Artifact MinIO Owner','test','active',$3,$3)
	`, userID, userID+"@test.local", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(
			project_id,name,created_by,created_at,updated_at
		) VALUES($1,'Artifact MinIO integration',$2,$3,$3)
	`, projectID, userID, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_members(
			project_id,user_id,role,created_at,updated_at
		) VALUES($1,$2,'owner',$3,$3)
	`, projectID, userID, now); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	t.Cleanup(func() {
		rows, queryErr := db.QueryContext(context.Background(), `
			SELECT staging_key FROM artifact_uploads WHERE project_id=$1
			UNION
			SELECT object_key FROM artifact_blobs WHERE project_id=$1
		`, projectID)
		if queryErr == nil {
			for rows.Next() {
				var key string
				if rows.Scan(&key) == nil &&
					!strings.HasPrefix(key, "deduplicated/") {
					_ = storage.Delete(context.Background(), key)
				}
			}
			rows.Close()
		}
		tx, txErr := db.BeginTx(context.Background(), nil)
		if txErr != nil {
			t.Errorf("begin cleanup: %v", txErr)
			return
		}
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
			`DELETE FROM projects WHERE project_id=$1`,
			projectID,
		)
		_, _ = tx.ExecContext(
			context.Background(),
			`DELETE FROM auth_users WHERE user_id=$1`,
			userID,
		)
		if err := tx.Commit(); err != nil {
			t.Errorf("commit cleanup: %v", err)
		}
	})

	projectService := &project.Service{
		Store: project.PostgresStore{DB: db},
	}
	store := PostgresStore{
		DB: db,
		Transaction: transaction.Manager{
			DB: transaction.SQLBeginner{DB: db},
		},
	}
	signer, err := NewTransferSigner(
		strings.Repeat("s", 32), "http://localhost:3000",
	)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	service := Service{
		Access: projectService, Clock: clock.Fixed{Time: now},
		Generator: generator, MaxUploadBytes: 20 * 1024 * 1024,
		MultipartPartBytes: MultipartMinPartBytes,
		Signer:             signer, Storage: storage, Store: store,
		TransferTTL: time.Minute, UploadSessionTTL: time.Hour,
	}
	owner := auth.Identity{
		Kind: "session", User: auth.User{ID: userID},
	}
	contents := []byte("real MinIO service lifecycle " + projectID)
	input := InitializeUploadInput{
		Filename: "minio.txt", Name: "MinIO",
		SizeBytes: int64(len(contents)), SHA256: digest(contents),
		MIMEType: "text/plain", Kind: KindAttachment,
		IdempotencyKey: "minio-upload",
	}
	upload, err := service.Initialize(ctx, owner, projectID, input)
	if err != nil {
		t.Fatalf("initialize MinIO upload: %v", err)
	}
	stranger := auth.Identity{
		Kind: "session", User: auth.User{ID: generator.MustNew()},
	}
	if _, err := service.SignParts(
		ctx, stranger, projectID, upload.UploadID, []int{1},
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unauthorized MinIO part signing should fail: %v", err)
	}
	grants, err := service.SignParts(
		ctx, owner, projectID, upload.UploadID, []int{1},
	)
	if err != nil {
		t.Fatalf("sign MinIO part: %v", err)
	}
	request, err := http.NewRequest(
		http.MethodPut, grants.Items[0].Transfer.URL, bytes.NewReader(contents),
	)
	if err != nil {
		t.Fatalf("create MinIO PUT: %v", err)
	}
	request.ContentLength = int64(len(contents))
	for key, value := range grants.Items[0].Transfer.Headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("PUT real MinIO part: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected MinIO PUT status: %d", response.StatusCode)
	}
	etag := response.Header.Get("ETag")
	if etag == "" {
		t.Fatal("MinIO PUT did not return ETag")
	}
	recovered, err := service.GetUpload(ctx, owner, projectID, upload.UploadID)
	if err != nil || len(recovered.CompletedParts) != 1 {
		t.Fatalf("recover real MinIO upload: %#v, %v", recovered, err)
	}
	detail, created, err := service.Confirm(
		ctx, owner, projectID, upload.UploadID,
		[]ConfirmPart{{PartNumber: 1, ETag: etag}},
	)
	if err != nil || !created || detail.CurrentVersion == nil {
		t.Fatalf("confirm real MinIO upload: %#v, %v, %v", detail, created, err)
	}
	repeated, repeatedCreated, err := service.Confirm(
		ctx, owner, projectID, upload.UploadID,
		[]ConfirmPart{{PartNumber: 1, ETag: etag}},
	)
	if err != nil || repeatedCreated || repeated.CurrentVersion == nil ||
		repeated.CurrentVersion.ID != detail.CurrentVersion.ID {
		t.Fatalf(
			"idempotent MinIO confirm: %#v created=%v err=%v",
			repeated, repeatedCreated, err,
		)
	}
	download, err := service.Download(
		ctx, owner, projectID, detail.Artifact.ID, "",
	)
	if err != nil || download.Transfer.Method != http.MethodGet {
		t.Fatalf("sign MinIO download: %#v, %v", download, err)
	}
	downloadResponse, err := http.Get(download.Transfer.URL)
	if err != nil {
		t.Fatalf("GET real MinIO object: %v", err)
	}
	downloaded, readErr := io.ReadAll(downloadResponse.Body)
	downloadResponse.Body.Close()
	if readErr != nil ||
		downloadResponse.StatusCode != http.StatusOK ||
		!bytes.Equal(downloaded, contents) {
		t.Fatalf(
			"unexpected MinIO download: status=%d bytes=%q err=%v",
			downloadResponse.StatusCode, downloaded, readErr,
		)
	}
	if err := service.Trash(ctx, owner, projectID, detail.Artifact.ID); err != nil {
		t.Fatalf("trash MinIO Artifact: %v", err)
	}
	if err := service.Purge(ctx, owner, projectID, detail.Artifact.ID); err != nil {
		t.Fatalf("purge MinIO Artifact: %v", err)
	}

	multipartContents := bytes.Repeat(
		[]byte("m"), int(MultipartMinPartBytes)+37,
	)
	multipartUpload, err := service.Initialize(
		ctx, owner, projectID, InitializeUploadInput{
			Filename:       "out-of-order.bin",
			SizeBytes:      int64(len(multipartContents)),
			SHA256:         digest(multipartContents),
			MIMEType:       "application/octet-stream",
			Kind:           KindAttachment,
			IdempotencyKey: "minio-out-of-order",
		},
	)
	if err != nil {
		t.Fatalf("initialize multipart MinIO upload: %v", err)
	}
	multipartGrants, err := service.SignParts(
		ctx, owner, projectID, multipartUpload.UploadID, []int{2, 1},
	)
	if err != nil {
		t.Fatalf("sign out-of-order MinIO parts: %v", err)
	}
	grantsByPart := make(map[int]PartGrant, len(multipartGrants.Items))
	for _, grant := range multipartGrants.Items {
		grantsByPart[grant.PartNumber] = grant
	}
	partTwo := multipartContents[MultipartMinPartBytes:]
	partTwoETag := putMinIOPart(t, grantsByPart[2], partTwo)
	recoveredMultipart, err := service.GetUpload(
		ctx, owner, projectID, multipartUpload.UploadID,
	)
	if err != nil || len(recoveredMultipart.CompletedParts) != 1 ||
		recoveredMultipart.CompletedParts[0].PartNumber != 2 {
		t.Fatalf(
			"recover out-of-order MinIO part: %#v err=%v",
			recoveredMultipart, err,
		)
	}
	if _, _, err := service.Confirm(
		ctx, owner, projectID, multipartUpload.UploadID,
		[]ConfirmPart{{PartNumber: 2, ETag: partTwoETag}},
	); !errors.Is(err, ErrUploadIncomplete) {
		t.Fatalf("missing MinIO part should reject confirm: %v", err)
	}
	partOneETag := putMinIOPart(
		t, grantsByPart[1], multipartContents[:MultipartMinPartBytes],
	)
	multipartDetail, multipartCreated, err := service.Confirm(
		ctx, owner, projectID, multipartUpload.UploadID,
		[]ConfirmPart{
			{PartNumber: 1, ETag: partOneETag},
			{PartNumber: 2, ETag: partTwoETag},
		},
	)
	if err != nil || !multipartCreated || multipartDetail.CurrentVersion == nil {
		t.Fatalf(
			"confirm out-of-order MinIO parts: %#v created=%v err=%v",
			multipartDetail, multipartCreated, err,
		)
	}
	if err := service.Trash(
		ctx, owner, projectID, multipartDetail.Artifact.ID,
	); err != nil {
		t.Fatalf("trash multipart MinIO Artifact: %v", err)
	}
	if err := service.Purge(
		ctx, owner, projectID, multipartDetail.Artifact.ID,
	); err != nil {
		t.Fatalf("purge multipart MinIO Artifact: %v", err)
	}

	hashContents := []byte("actual MinIO hash bytes")
	hashUpload, err := service.Initialize(
		ctx, owner, projectID, InitializeUploadInput{
			Filename:       "hash-mismatch.txt",
			SizeBytes:      int64(len(hashContents)),
			SHA256:         digest([]byte("different MinIO hash bytes")),
			MIMEType:       "text/plain",
			Kind:           KindAttachment,
			IdempotencyKey: "minio-hash-mismatch",
		},
	)
	if err != nil {
		t.Fatalf("initialize MinIO hash mismatch: %v", err)
	}
	hashGrants, err := service.SignParts(
		ctx, owner, projectID, hashUpload.UploadID, []int{1},
	)
	if err != nil {
		t.Fatalf("sign MinIO hash mismatch part: %v", err)
	}
	hashETag := putMinIOPart(t, hashGrants.Items[0], hashContents)
	if _, _, err := service.Confirm(
		ctx, owner, projectID, hashUpload.UploadID,
		[]ConfirmPart{{PartNumber: 1, ETag: hashETag}},
	); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("expected MinIO full SHA-256 mismatch, got %v", err)
	}

	cancelContents := []byte("cancel")
	cancelUpload, err := service.Initialize(
		ctx, owner, projectID, InitializeUploadInput{
			Filename: "cancel.txt", SizeBytes: int64(len(cancelContents)),
			SHA256: digest(cancelContents), MIMEType: "text/plain",
			Kind: KindAttachment, IdempotencyKey: "minio-cancel",
		},
	)
	if err != nil {
		t.Fatalf("initialize MinIO cancellation: %v", err)
	}
	if err := service.Abort(
		ctx, owner, projectID, cancelUpload.UploadID,
	); err != nil {
		t.Fatalf("abort real MinIO upload: %v", err)
	}
	if err := service.Abort(
		ctx, owner, projectID, cancelUpload.UploadID,
	); err != nil {
		t.Fatalf("repeat abort real MinIO upload: %v", err)
	}

	expiredUpload, err := service.Initialize(
		ctx, owner, projectID, InitializeUploadInput{
			Filename:       "expired.txt",
			SizeBytes:      7,
			SHA256:         digest([]byte("expired")),
			MIMEType:       "text/plain",
			Kind:           KindAttachment,
			IdempotencyKey: "minio-expired",
		},
	)
	if err != nil {
		t.Fatalf("initialize expiring MinIO upload: %v", err)
	}
	service.Clock = clock.Fixed{Time: now.Add(2 * time.Hour)}
	expiredCount, err := service.ExpireUploads(ctx, 10)
	if err != nil || expiredCount != 1 {
		t.Fatalf("expire real MinIO upload: count=%d err=%v", expiredCount, err)
	}
	expiredState, err := store.GetUpload(
		ctx, projectID, expiredUpload.UploadID,
	)
	if err != nil || expiredState.Status != UploadExpired {
		t.Fatalf("expired MinIO upload state: %#v err=%v", expiredState, err)
	}
}

func putMinIOPart(t *testing.T, grant PartGrant, contents []byte) string {
	t.Helper()
	request, err := http.NewRequest(
		http.MethodPut, grant.Transfer.URL, bytes.NewReader(contents),
	)
	if err != nil {
		t.Fatalf("create MinIO part PUT: %v", err)
	}
	request.ContentLength = int64(len(contents))
	for key, value := range grant.Transfer.Headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("PUT MinIO part %d: %v", grant.PartNumber, err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf(
			"unexpected MinIO part %d PUT status: %d",
			grant.PartNumber, response.StatusCode,
		)
	}
	etag := response.Header.Get("ETag")
	if etag == "" {
		t.Fatalf("MinIO part %d PUT did not return ETag", grant.PartNumber)
	}
	return etag
}
