package artifact

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalBlobStoreMultipartRecoveryCompletionAndPromotion(t *testing.T) {
	store, err := NewLocalBlobStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatalf("initialize local store: %v", err)
	}
	ctx := context.Background()
	upload, err := store.CreateMultipart(ctx, "staging/project/upload", "text/plain")
	if err != nil {
		t.Fatalf("create multipart: %v", err)
	}
	second, err := store.PutPart(ctx, upload, 2, bytes.NewReader([]byte("world")), 5)
	if err != nil {
		t.Fatalf("put second part: %v", err)
	}
	first, err := store.PutPart(ctx, upload, 1, bytes.NewReader([]byte("hello ")), 6)
	if err != nil {
		t.Fatalf("put first part: %v", err)
	}

	parts, err := store.ListParts(ctx, upload)
	if err != nil {
		t.Fatalf("list recovered parts: %v", err)
	}
	if len(parts) != 2 ||
		parts[0].PartNumber != 1 ||
		parts[1].PartNumber != 2 {
		t.Fatalf("parts were not recovered in order: %+v", parts)
	}
	info, err := store.CompleteMultipart(ctx, upload, []CompletedPart{first, second})
	if err != nil {
		t.Fatalf("complete multipart: %v", err)
	}
	if info.SizeBytes != 11 {
		t.Fatalf("unexpected completed size: %d", info.SizeBytes)
	}
	if err := store.Promote(
		ctx,
		upload.ObjectKey,
		"projects/project/blobs/sha256/00/hash",
	); err != nil {
		t.Fatalf("promote object: %v", err)
	}
	reader, err := store.Open(ctx, "projects/project/blobs/sha256/00/hash")
	if err != nil {
		t.Fatalf("open promoted object: %v", err)
	}
	defer reader.Close()
	contents, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read promoted object: %v", err)
	}
	if string(contents) != "hello world" {
		t.Fatalf("unexpected object contents: %q", contents)
	}
}

func TestLocalBlobStoreRejectsMissingConflictingAndOversizedParts(t *testing.T) {
	store, err := NewLocalBlobStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	upload, err := store.CreateMultipart(ctx, "staging/project/upload", "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.PutPart(ctx, upload, 1, bytes.NewReader([]byte("one")), 3)
	if err != nil {
		t.Fatal(err)
	}
	first, err = store.PutPart(ctx, upload, 1, bytes.NewReader([]byte("ONE")), 3)
	if err != nil {
		t.Fatalf("retry first part: %v", err)
	}
	parts, err := store.ListParts(ctx, upload)
	if err != nil || len(parts) != 1 || parts[0].ETag != first.ETag {
		t.Fatalf("retried part was not durable: parts=%+v err=%v", parts, err)
	}
	if _, err := store.PutPart(
		ctx,
		upload,
		2,
		bytes.NewReader([]byte("too many")),
		3,
	); !errors.Is(err, ErrInvalidPart) {
		t.Fatalf("expected oversized part rejection, got %v", err)
	}
	if _, err := store.CompleteMultipart(ctx, upload, []CompletedPart{first, {
		ETag: "missing", PartNumber: 2, SizeBytes: 1,
	}}); !errors.Is(err, ErrInvalidPart) {
		t.Fatalf("expected missing part rejection, got %v", err)
	}
	first.ETag = "forged"
	if _, err := store.CompleteMultipart(
		ctx,
		upload,
		[]CompletedPart{first},
	); !errors.Is(err, ErrInvalidPart) {
		t.Fatalf("expected forged ETag rejection, got %v", err)
	}
}

func TestLocalBlobStoreAbortAndUnsupportedSigningAreIdempotent(t *testing.T) {
	store, err := NewLocalBlobStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	upload, err := store.CreateMultipart(ctx, "staging/project/upload", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AbortMultipart(ctx, upload); err != nil {
		t.Fatalf("abort multipart: %v", err)
	}
	if err := store.AbortMultipart(ctx, upload); err != nil {
		t.Fatalf("repeat abort multipart: %v", err)
	}
	if _, err := store.ListParts(ctx, upload); !errors.Is(err, ErrUploadNotFound) {
		t.Fatalf("expected aborted session to disappear, got %v", err)
	}
	if _, err := store.PresignPart(ctx, upload, 1, time.Minute); !errors.Is(
		err,
		ErrDirectTransferUnsupported,
	) {
		t.Fatalf("unexpected local part signing result: %v", err)
	}
	if _, err := store.PresignGet(
		ctx,
		upload.ObjectKey,
		time.Minute,
		GetObjectOptions{},
	); !errors.Is(
		err,
		ErrDirectTransferUnsupported,
	) {
		t.Fatalf("unexpected local download signing result: %v", err)
	}
}

func TestLocalBlobStoreRejectsTraversalAndOverwrite(t *testing.T) {
	store, err := NewLocalBlobStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := store.CreateMultipart(ctx, "../escape", "text/plain"); !errors.Is(
		err,
		ErrInvalidObjectKey,
	) {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
	upload, err := store.CreateMultipart(ctx, "staging/one", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	part, err := store.PutPart(ctx, upload, 1, bytes.NewReader([]byte("one")), 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteMultipart(ctx, upload, []CompletedPart{part}); err != nil {
		t.Fatal(err)
	}
	if err := store.Promote(ctx, "staging/one", "final/key"); err != nil {
		t.Fatal(err)
	}
	upload, err = store.CreateMultipart(ctx, "staging/two", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	part, err = store.PutPart(ctx, upload, 1, bytes.NewReader([]byte("two")), 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteMultipart(ctx, upload, []CompletedPart{part}); err != nil {
		t.Fatal(err)
	}
	if err := store.Promote(ctx, "staging/two", "final/key"); !errors.Is(
		err,
		ErrObjectExists,
	) {
		t.Fatalf("expected immutable destination, got %v", err)
	}
}
