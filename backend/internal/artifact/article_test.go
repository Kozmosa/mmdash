package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strconv"
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
	blobs         map[string]Blob
	createFirst   int
	finalizeCount int
}

func newArticleTemplateStore() *articleTemplateStore {
	return &articleTemplateStore{
		uploads: make(map[string]UploadSession),
		details: make(map[string]Detail),
		blobs:   make(map[string]Blob),
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

func (store *articleTemplateStore) GetVersion(
	_ context.Context, projectID, artifactID, versionID string,
) (Version, error) {
	detail, ok := store.details[artifactIDKey(projectID, artifactID)]
	if !ok || detail.CurrentVersion == nil || detail.CurrentVersion.ID != versionID {
		return Version{}, ErrNotFound
	}
	return *detail.CurrentVersion, nil
}

func (store *articleTemplateStore) FindBlob(
	_ context.Context, projectID string, sha string, size int64,
) (Blob, error) {
	blob, ok := store.blobs[articleBlobKey(projectID, sha, size)]
	if !ok {
		return Blob{}, ErrNotFound
	}
	return blob, nil
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
	store.blobs[articleBlobKey(blob.ProjectID, blob.SHA256, blob.SizeBytes)] = blob
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

func articleBlobKey(projectID, sha string, size int64) string {
	return projectID + "\x00" + sha + "\x00" + strconv.FormatInt(size, 10)
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

type articleGrantStorage struct {
	BlobStore
	presignCalls int
}

func (*articleGrantStorage) Backend() string { return "minio" }

func (storage *articleGrantStorage) PresignGet(
	context.Context,
	string,
	time.Duration,
	GetObjectOptions,
) (SignedRequest, error) {
	storage.presignCalls++
	return SignedRequest{
		Method: "GET",
		URL:    "https://prod.mmdash.moe/mmdash/template.zip?signed=provider",
	}, nil
}

func TestArticleResourceGrantUsesInternalWorkerTransferForObjectStorage(t *testing.T) {
	projectID := "project-template"
	artifactID := "artifact-template"
	versionID := "version-template"
	store := newArticleTemplateStore()
	store.details[artifactIDKey(projectID, artifactID)] = Detail{
		Artifact: Artifact{ID: artifactID, ProjectID: projectID, Status: StatusAvailable},
		CurrentVersion: &Version{
			ID: versionID, ArtifactID: artifactID, ProjectID: projectID,
			StorageClass: "object", Status: StatusAvailable, SizeBytes: 123,
			Filename: "template.zip", MIMEType: articleTemplateMIME,
		},
	}
	publicSigner, err := NewTransferSigner(strings.Repeat("s", 32), "https://prod.mmdash.moe")
	if err != nil {
		t.Fatalf("create public signer: %v", err)
	}
	workerSigner, err := NewTransferSigner(strings.Repeat("s", 32), "http://core:8080")
	if err != nil {
		t.Fatalf("create Worker signer: %v", err)
	}
	storage := &articleGrantStorage{}
	service := Service{
		Signer: publicSigner, Storage: storage, Store: store,
		TransferTTL: time.Minute, WorkerSigner: workerSigner,
	}

	grant, err := service.ArticleResourceGrant(
		context.Background(), projectID, artifactID, versionID,
	)
	if err != nil {
		t.Fatalf("create Article resource grant: %v", err)
	}
	if storage.presignCalls != 0 {
		t.Fatalf("Article Worker grant used provider presigning %d times", storage.presignCalls)
	}
	rawURL, ok := grant["url"].(string)
	if !ok || !strings.HasPrefix(rawURL, "http://core:8080/v1/artifact-transfers/") {
		t.Fatalf("unexpected Worker transfer URL: %#v", grant["url"])
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse Worker transfer URL: %v", err)
	}
	token, err := transferToken(strings.TrimPrefix(parsed.Path, "/v1/artifact-transfers/"))
	if err != nil {
		t.Fatalf("extract Worker transfer token: %v", err)
	}
	claims, err := workerSigner.Verify(token, time.Now())
	if err != nil {
		t.Fatalf("verify Worker transfer token: %v", err)
	}
	if claims.Kind != transferDownload || claims.ProjectID != projectID ||
		claims.ArtifactID != artifactID || claims.VersionID != versionID ||
		claims.SizeBytes != 123 {
		t.Fatalf("unexpected Worker transfer claims: %#v", claims)
	}
}

func TestArticleResourceGrantRejectsMissingInternalWorkerSigner(t *testing.T) {
	projectID := "project-template"
	artifactID := "artifact-template"
	versionID := "version-template"
	store := newArticleTemplateStore()
	store.details[artifactIDKey(projectID, artifactID)] = Detail{
		Artifact: Artifact{ID: artifactID, ProjectID: projectID, Status: StatusAvailable},
		CurrentVersion: &Version{
			ID: versionID, ArtifactID: artifactID, ProjectID: projectID,
			StorageClass: "object", Status: StatusAvailable, SizeBytes: 123,
		},
	}
	storage := &articleGrantStorage{}
	service := Service{Storage: storage, Store: store}

	if _, err := service.ArticleResourceGrant(
		context.Background(), projectID, artifactID, versionID,
	); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("expected missing Worker signer rejection, got %v", err)
	}
	if storage.presignCalls != 0 {
		t.Fatalf("missing Worker signer fell back to provider presigning")
	}
}

func TestArchiveArticleBuildOutputSupportsBlobDeduplicationAndIdempotentRetry(t *testing.T) {
	contents := []byte("shared article build output")
	projectID := "project-build"
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
	firstArtifactID, firstVersionID, err := service.ArchiveArticleBuildOutput(
		context.Background(), projectID, "build-one", "system", "pdf",
		"main.pdf", "application/pdf", sha, int64(len(contents)), bytes.NewReader(contents),
	)
	if err != nil {
		t.Fatalf("archive first build output: %v", err)
	}
	secondArtifactID, secondVersionID, err := service.ArchiveArticleBuildOutput(
		context.Background(), projectID, "build-one", "system", "tex_source",
		"main.tex", "application/x-tex", sha, int64(len(contents)), bytes.NewReader(contents),
	)
	if err != nil {
		t.Fatalf("archive deduplicated build output: %v", err)
	}
	if secondArtifactID == firstArtifactID || secondVersionID == firstVersionID {
		t.Fatalf("deduplicated bytes must still produce distinct output identities")
	}
	if store.createFirst != 2 || store.finalizeCount != 1 {
		t.Fatalf("unexpected persistence counts: create=%d finalize=%d", store.createFirst, store.finalizeCount)
	}
	retryArtifactID, retryVersionID, err := service.ArchiveArticleBuildOutput(
		context.Background(), projectID, "build-one", "system", "tex_source",
		"main.tex", "application/x-tex", sha, int64(len(contents)), failingReader{},
	)
	if err != nil {
		t.Fatalf("retry deduplicated build output: %v", err)
	}
	if retryArtifactID != secondArtifactID || retryVersionID != secondVersionID ||
		store.createFirst != 2 || store.finalizeCount != 1 || ids.next != 7 {
		t.Fatalf("retry created a duplicate: first=(%s,%s) retry=(%s,%s) create=%d finalize=%d ids=%d",
			secondArtifactID, secondVersionID, retryArtifactID, retryVersionID,
			store.createFirst, store.finalizeCount, ids.next)
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
