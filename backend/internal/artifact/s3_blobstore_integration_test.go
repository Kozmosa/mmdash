package artifact

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestMinIOBlobStoreRealMultipart(t *testing.T) {
	endpoint := os.Getenv("MMDASH_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("MMDASH_TEST_MINIO_ENDPOINT is not configured")
	}
	accessKey := envForTest("MMDASH_TEST_MINIO_ACCESS_KEY", "mmdash")
	secretKey := envForTest("MMDASH_TEST_MINIO_SECRET_KEY", "change-me")
	bucket := envForTest("MMDASH_TEST_MINIO_BUCKET", "mmdash-artifact-integration")
	store, err := NewS3BlobStore(S3BlobStoreConfig{
		AccessKey:      accessKey,
		Backend:        "minio",
		Bucket:         bucket,
		Endpoint:       endpoint,
		PublicEndpoint: endpoint,
		Region:         "us-east-1",
		SecretKey:      secretKey,
	})
	if err != nil {
		t.Fatalf("initialize MinIO store: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := store.client.Client.MakeBucket(
		ctx,
		bucket,
		minio.MakeBucketOptions{Region: "us-east-1"},
	); err != nil {
		exists, existsErr := store.client.Client.BucketExists(ctx, bucket)
		if existsErr != nil || !exists {
			t.Fatalf("create MinIO test bucket: %v (exists error: %v)", err, existsErr)
		}
	}
	if err := store.Check(ctx); err != nil {
		t.Fatalf("check MinIO readiness: %v", err)
	}

	prefix := fmt.Sprintf("integration/%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = store.Delete(context.Background(), prefix+"/final")
		_ = store.Delete(context.Background(), prefix+"/direct")
	})

	upload, err := store.CreateMultipart(ctx, prefix+"/staging", "application/octet-stream")
	if err != nil {
		t.Fatalf("create multipart: %v", err)
	}
	secondContents := bytes.Repeat([]byte{0x22}, int(MultipartMinPartBytes))
	second, err := store.PutPart(
		ctx,
		upload,
		2,
		bytes.NewReader(secondContents),
		int64(len(secondContents)),
	)
	if err != nil {
		t.Fatalf("put out-of-order second part: %v", err)
	}
	firstContents := bytes.Repeat([]byte{0x11}, int(MultipartMinPartBytes))
	first, err := store.PutPart(
		ctx,
		upload,
		1,
		bytes.NewReader(firstContents),
		int64(len(firstContents)),
	)
	if err != nil {
		t.Fatalf("put first part: %v", err)
	}
	parts, err := store.ListParts(ctx, upload)
	if err != nil {
		t.Fatalf("recover multipart parts: %v", err)
	}
	if len(parts) != 2 ||
		parts[0].PartNumber != 1 ||
		parts[1].PartNumber != 2 {
		t.Fatalf("unexpected recovered parts: %+v", parts)
	}
	if _, err := store.CompleteMultipart(
		ctx,
		upload,
		[]CompletedPart{first},
	); err == nil {
		t.Fatal("expected missing part completion to fail")
	}
	info, err := store.CompleteMultipart(
		ctx,
		upload,
		[]CompletedPart{first, second},
	)
	if err != nil {
		t.Fatalf("complete multipart: %v", err)
	}
	expectedSize := int64(len(firstContents) + len(secondContents))
	if info.SizeBytes != expectedSize {
		t.Fatalf("unexpected completed size: %d", info.SizeBytes)
	}
	if err := store.Promote(ctx, upload.ObjectKey, prefix+"/final"); err != nil {
		t.Fatalf("promote multipart object: %v", err)
	}
	reader, err := store.Open(ctx, prefix+"/final")
	if err != nil {
		t.Fatalf("open promoted object: %v", err)
	}
	readSize, readErr := io.Copy(io.Discard, reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || readSize != expectedSize {
		t.Fatalf(
			"stream promoted object: size=%d read=%v close=%v",
			readSize,
			readErr,
			closeErr,
		)
	}

	directUpload, err := store.CreateMultipart(ctx, prefix+"/direct", "text/plain")
	if err != nil {
		t.Fatalf("create direct multipart: %v", err)
	}
	signed, err := store.PresignPart(ctx, directUpload, 1, time.Minute)
	if err != nil {
		t.Fatalf("sign direct part: %v", err)
	}
	directContents := bytes.Repeat([]byte("x"), 1024)
	request, err := http.NewRequestWithContext(
		ctx,
		signed.Method,
		signed.URL,
		bytes.NewReader(directContents),
	)
	if err != nil {
		t.Fatalf("create direct part request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("upload signed part: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("signed part returned HTTP %d", response.StatusCode)
	}
	directParts, err := store.ListParts(ctx, directUpload)
	if err != nil || len(directParts) != 1 {
		t.Fatalf("recover direct part: parts=%+v err=%v", directParts, err)
	}
	if _, err := store.CompleteMultipart(ctx, directUpload, directParts); err != nil {
		t.Fatalf("complete direct multipart: %v", err)
	}

	aborted, err := store.CreateMultipart(ctx, prefix+"/aborted", "text/plain")
	if err != nil {
		t.Fatalf("create abort upload: %v", err)
	}
	if _, err := store.PutPart(
		ctx,
		aborted,
		1,
		bytes.NewReader([]byte("partial")),
		7,
	); err != nil {
		t.Fatalf("put abort part: %v", err)
	}
	if err := store.AbortMultipart(ctx, aborted); err != nil {
		t.Fatalf("abort upload: %v", err)
	}
	if err := store.AbortMultipart(ctx, aborted); err != nil {
		t.Fatalf("repeat abort upload: %v", err)
	}
	if _, err := store.ListParts(ctx, aborted); err == nil {
		t.Fatal("expected aborted upload to disappear")
	}
}

func envForTest(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
