package artifact

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestS3BlobStoreSignsPublicMultipartAndDownloadURLs(t *testing.T) {
	internal := httptest.NewServer(http.NotFoundHandler())
	defer internal.Close()
	public := httptest.NewServer(http.NotFoundHandler())
	defer public.Close()

	store, err := NewS3BlobStore(S3BlobStoreConfig{
		AccessKey:      "access",
		Backend:        "minio",
		Bucket:         "mmdash-artifacts",
		Endpoint:       internal.URL,
		PublicEndpoint: public.URL,
		Region:         "us-east-1",
		SecretKey:      "secret",
	})
	if err != nil {
		t.Fatalf("initialize S3-compatible store: %v", err)
	}
	upload := MultipartUpload{
		ObjectKey:        "staging/project/upload",
		ProviderUploadID: "provider-upload",
	}
	part, err := store.PresignPart(context.Background(), upload, 7, 5*time.Minute)
	if err != nil {
		t.Fatalf("sign part: %v", err)
	}
	partURL, err := url.Parse(part.URL)
	if err != nil {
		t.Fatalf("parse part URL: %v", err)
	}
	publicURL, _ := url.Parse(public.URL)
	if part.Method != http.MethodPut ||
		partURL.Host != publicURL.Host ||
		partURL.Query().Get("partNumber") != "7" ||
		partURL.Query().Get("uploadId") != upload.ProviderUploadID ||
		partURL.Query().Get("X-Amz-Signature") == "" {
		t.Fatalf("unexpected signed part URL: %s", part.URL)
	}
	if strings.Contains(part.URL, "secret") {
		t.Fatal("signed URL exposed the secret key")
	}

	download, err := store.PresignGet(
		context.Background(),
		"projects/project/blobs/sha256/00/hash",
		time.Minute,
	)
	if err != nil {
		t.Fatalf("sign download: %v", err)
	}
	downloadURL, _ := url.Parse(download.URL)
	if download.Method != http.MethodGet || downloadURL.Host != publicURL.Host {
		t.Fatalf("unexpected signed download URL: %s", download.URL)
	}
}

func TestS3BlobStoreRejectsInvalidBackendKeyPartAndTTL(t *testing.T) {
	if _, err := NewS3BlobStore(S3BlobStoreConfig{
		AccessKey: "access",
		Backend:   "other",
		Bucket:    "mmdash-artifacts",
		Endpoint:  "http://localhost:9000",
		SecretKey: "secret",
	}); err == nil {
		t.Fatal("expected unsupported backend rejection")
	}
	store, err := NewS3BlobStore(S3BlobStoreConfig{
		AccessKey: "access",
		Backend:   "s3",
		Bucket:    "mmdash-artifacts",
		Endpoint:  "https://s3.example.test",
		SecretKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	upload := MultipartUpload{
		ObjectKey:        "staging/project/upload",
		ProviderUploadID: "provider-upload",
	}
	if _, err := store.PresignPart(
		context.Background(),
		upload,
		0,
		time.Minute,
	); err == nil {
		t.Fatal("expected invalid part rejection")
	}
	if _, err := store.PresignPart(
		context.Background(),
		upload,
		1,
		8*24*time.Hour,
	); err == nil {
		t.Fatal("expected invalid TTL rejection")
	}
	if _, err := store.PresignGet(
		context.Background(),
		"../escape",
		time.Minute,
	); err == nil {
		t.Fatal("expected invalid object key rejection")
	}
}
