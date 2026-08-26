package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/project"
)

type articleTemplateIDs struct {
	next int
}

func (ids *articleTemplateIDs) New() (string, error) {
	ids.next++
	return "article-template-id-" + string(rune('0'+ids.next)), nil
}

type articleTemplateStore struct {
	Store
	uploads       map[string]UploadSession
	details       map[string]Detail
	createFirst   int
	finalizeCount int
}

func newArticleTemplateStore() *articleTemplateStore {
	return &articleTemplateStore{
		uploads: make(map[string]UploadSession),
		details: make(map[string]Detail),
	}
}

func articleTemplateUploadKey(projectID, idempotencyKey string) string {
	return projectID + "\x00" + idempotencyKey
}

func (store *articleTemplateStore) GetUploadByIdempotency(
	_ context.Context, projectID, idempotencyKey string,
) (UploadSession, error) {
	upload, ok := store.uploads[articleTemplateUploadKey(projectID, idempotencyKey)]
	if !ok {
		return UploadSession{}, ErrNotFound
	}
	return upload, nil
}

func (store *articleTemplateStore) GetDetail(
	_ context.Context, projectID, artifactID string, _ bool,
) (Detail, error) {
	detail, ok := store.details[artifactIDKey(projectID, artifactID)]
	if !ok {
		return Detail{}, ErrNotFound
	}
	return detail, nil
}

func (store *articleTemplateStore) FindBlob(
	_ context.Context, _ string, _ string, _ int64,
) (Blob, error) {
	return Blob{}, ErrNotFound
}

func (store *articleTemplateStore) CreateFirst(
	_ context.Context, artifact Artifact, version Version, upload UploadSession,
) error {
	key := articleTemplateUploadKey(upload.ProjectID, upload.IdempotencyKey)
	if _, exists := store.uploads[key]; exists {
		return ErrUploadConflict
	}
	store.createFirst++
	upload.VersionNo = version.VersionNo
	store.uploads[key] = upload
	store.details[artifactIDKey(artifact.ProjectID, artifact.ID)] = Detail{
		Artifact: artifact, CurrentVersion: &version,
	}
	return nil
}

func (store *articleTemplateStore) UpsertParts(
	_ context.Context, _ string, _ []UploadPart,
) error {
	return nil
}

func (store *articleTemplateStore) MarkUploading(
	_ context.Context, uploadID string, _ time.Time,
) error {
	return store.updateUpload(uploadID, func(upload *UploadSession) {
		upload.Status = UploadUploading
	})
}

func (store *articleTemplateStore) SetUploadStatus(
	_ context.Context, uploadID, status, _ string, _ time.Time,
) error {
	return store.updateUpload(uploadID, func(upload *UploadSession) {
		upload.Status = status
	})
}

func (store *articleTemplateStore) FinalizeUpload(
	_ context.Context, upload UploadSession, blob Blob, now time.Time,
) (Detail, error) {
	store.finalizeCount++
	if err := store.updateUpload(upload.ID, func(item *UploadSession) {
		item.Status = UploadCompleted
		item.CompletedAt = &now
	}); err != nil {
		return Detail{}, err
	}
	key := artifactIDKey(upload.ProjectID, upload.ArtifactID)
	detail, ok := store.details[key]
	if !ok {
		return Detail{}, ErrNotFound
	}
	detail.Artifact.Status = StatusAvailable
	detail.Artifact.CurrentVersionID = &upload.VersionID
	if detail.CurrentVersion == nil {
		return Detail{}, ErrNotFound
	}
	detail.CurrentVersion.Status = StatusAvailable
	detail.CurrentVersion.AvailableAt = &now
	detail.CurrentVersion.BlobID = blob.ID
	detail.CurrentVersion.ObjectKey = blob.ObjectKey
	detail.CurrentVersion.Backend = blob.Backend
	store.details[key] = detail
	return detail, nil
}

func (store *articleTemplateStore) updateUpload(
	uploadID string, update func(*UploadSession),
) error {
	for key, upload := range store.uploads {
		if upload.ID == uploadID {
			update(&upload)
			store.uploads[key] = upload
			return nil
		}
	}
	return ErrNotFound
}

func artifactIDKey(projectID, artifactID string) string {
	return projectID + "\x00" + artifactID
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("retry input must not be read")
}

func articleTemplateSHA(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

func TestArchiveArticleTemplatePersistsImmutableSourceArticleAttachment(t *testing.T) {
	contents := []byte("default template zip bytes")
	projectID := "project-template"
	store := newArticleTemplateStore()
	storage, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	service := Service{
		Generator:          &articleTemplateIDs{},
		MaxUploadBytes:     20 * 1024 * 1024,
		MultipartPartBytes: MultipartMinPartBytes,
		Storage:            storage,
		Store:              store,
	}

	artifactID, versionID, err := service.ArchiveArticleTemplate(
		context.Background(), projectID, "system", "mmdash-default.zip", "default-v1",
		articleTemplateSHA(contents), int64(len(contents)), bytes.NewReader(contents),
	)
	if err != nil {
		t.Fatalf("archive article template: %v", err)
	}
	if artifactID == "" || versionID == "" || store.createFirst != 1 || store.finalizeCount != 1 {
		t.Fatalf("unexpected archive result: artifact=%q version=%q create=%d finalize=%d", artifactID, versionID, store.createFirst, store.finalizeCount)
	}
	detail := store.details[artifactIDKey(projectID, artifactID)]
	if detail.Artifact.Source != SourceArticle || detail.Artifact.Kind != KindAttachment ||
		detail.Artifact.Status != StatusAvailable || detail.CurrentVersion == nil ||
		detail.CurrentVersion.ID != versionID || detail.CurrentVersion.Status != StatusAvailable ||
		detail.CurrentVersion.MIMEType != articleTemplateMIME {
		t.Fatalf("unexpected archived Article attachment: %#v", detail)
	}
	if _, err := storage.Stat(context.Background(), ContentObjectKey(projectID, articleTemplateSHA(contents))); err != nil {
		t.Fatalf("promoted template object is unavailable: %v", err)
	}
}

func TestArchiveArticleTemplateIsIdempotentAndRejectsKeyReuseMismatch(t *testing.T) {
	contents := []byte("default template zip bytes")
	projectID := "project-template"
	store := newArticleTemplateStore()
	storage, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	ids := &articleTemplateIDs{}
	service := Service{
		Generator:          ids,
		MaxUploadBytes:     20 * 1024 * 1024,
		MultipartPartBytes: MultipartMinPartBytes,
		Storage:            storage,
		Store:              store,
	}
	sha := articleTemplateSHA(contents)
	firstArtifactID, firstVersionID, err := service.ArchiveArticleTemplate(
		context.Background(), projectID, "system", "mmdash-default.zip", "default-v1",
		sha, int64(len(contents)), bytes.NewReader(contents),
	)
	if err != nil {
		t.Fatalf("archive first template: %v", err)
	}
	secondArtifactID, secondVersionID, err := service.ArchiveArticleTemplate(
		context.Background(), projectID, "system", "mmdash-default.zip", "default-v1",
		sha, int64(len(contents)), failingReader{},
	)
	if err != nil {
		t.Fatalf("retry archive template: %v", err)
	}
	if secondArtifactID != firstArtifactID || secondVersionID != firstVersionID ||
		store.createFirst != 1 || store.finalizeCount != 1 || ids.next != 4 {
		t.Fatalf("retry created a duplicate: first=(%s,%s) second=(%s,%s) create=%d finalize=%d ids=%d", firstArtifactID, firstVersionID, secondArtifactID, secondVersionID, store.createFirst, store.finalizeCount, ids.next)
	}
	if _, _, err := service.ArchiveArticleTemplate(
		context.Background(), projectID, "system", "other.zip", "default-v1",
		sha, int64(len(contents)), failingReader{},
	); !errors.Is(err, ErrUploadConflict) {
		t.Fatalf("expected idempotency mismatch conflict, got %v", err)
	}
}

func TestArchiveArticleTemplatePreservesSizeAndSHAValidation(t *testing.T) {
	contents := []byte("default template zip bytes")
	store := newArticleTemplateStore()
	storage, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	service := Service{
		Generator:          &articleTemplateIDs{},
		MaxUploadBytes:     int64(len(contents) - 1),
		MultipartPartBytes: MultipartMinPartBytes,
		Storage:            storage,
		Store:              store,
	}
	if _, _, err := service.ArchiveArticleTemplate(
		context.Background(), "project-template", "system", "default.zip", "default-v1",
		articleTemplateSHA(contents), int64(len(contents)), bytes.NewReader(contents),
	); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected maximum-size rejection, got %v", err)
	}
	service.MaxUploadBytes = 20 * 1024 * 1024
	if _, _, err := service.ArchiveArticleTemplate(
		context.Background(), "project-template", "system", "default.zip", "default-v1",
		strings.Repeat("0", 64), int64(len(contents)), bytes.NewReader(contents),
	); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("expected SHA rejection, got %v", err)
	}
}

func TestBuiltInArticleTemplateRejectsOrdinaryMutationAndDeletion(t *testing.T) {
	projectID := "project-template"
	artifactID := "built-in-template"
	store := newArticleTemplateStore()
	store.details[artifactIDKey(projectID, artifactID)] = Detail{Artifact: Artifact{
		ID: artifactID, ProjectID: projectID, Source: SourceArticle,
		Tags: []string{"article-template"}, Status: StatusAvailable,
	}}
	service := Service{
		Access: roleAccess{role: project.RoleOwner}, Store: store,
		Generator: &articleTemplateIDs{},
	}
	caller := auth.Identity{Kind: "session", User: auth.User{ID: "owner"}}
	ctx := context.Background()
	changedName := "changed"

	if _, err := service.Update(ctx, caller, projectID, artifactID, UpdateInput{Name: &changedName}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected built-in template update rejection, got %v", err)
	}
	if _, err := service.InitializeVersion(ctx, caller, projectID, artifactID, InitializeVersionInput{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected built-in template version rejection, got %v", err)
	}
	if _, err := service.RestoreVersion(ctx, caller, projectID, artifactID, "version-1", "restore-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected built-in template restore-version rejection, got %v", err)
	}
	if err := service.Trash(ctx, caller, projectID, artifactID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected built-in template trash rejection, got %v", err)
	}
	if err := service.Purge(ctx, caller, projectID, artifactID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected built-in template purge rejection, got %v", err)
	}
}
