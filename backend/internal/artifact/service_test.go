package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/project"
)

func TestNormalizeArtifactInputAndContentKey(t *testing.T) {
	service := Service{
		MaxUploadBytes:     20 * 1024 * 1024,
		MultipartPartBytes: MultipartMinPartBytes,
	}
	description := "  source data  "
	folderID := "550e8400-e29b-41d4-a716-446655440000"
	input := InitializeUploadInput{
		Filename: " input.csv ", SizeBytes: 6 * 1024 * 1024,
		SHA256: strings.Repeat("a", 64), MIMEType: "text/csv; charset=utf-8",
		Kind: KindAttachment, Tags: []string{" data ", "DATA", "raw"},
		Description: &description, FolderID: &folderID, IdempotencyKey: " upload-1 ",
	}
	plan, err := service.normalizeInitialize(&input)
	if err != nil {
		t.Fatalf("normalize input: %v", err)
	}
	if plan.PartCount != 2 || plan.PartBytes != MultipartMinPartBytes {
		t.Fatalf("unexpected multipart plan: %#v", plan)
	}
	if input.Name != "input.csv" || input.MIMEType != "text/csv" {
		t.Fatalf("unexpected normalized display values: %#v", input)
	}
	if input.FolderID == nil || *input.FolderID != folderID {
		t.Fatalf("unexpected normalized folder: %#v", input.FolderID)
	}
	if len(input.Tags) != 2 || input.Tags[0] != "data" || input.Tags[1] != "raw" {
		t.Fatalf("unexpected normalized tags: %#v", input.Tags)
	}
	expected := "projects/project-1/blobs/sha256/aa/" + strings.Repeat("a", 64)
	if actual := ContentObjectKey("project-1", strings.Repeat("a", 64)); actual != expected {
		t.Fatalf("unexpected content key: %s", actual)
	}
}

func TestNormalizeArtifactInputRejectsPublicForgeryAndUnsafeMetadata(t *testing.T) {
	service := Service{
		MaxUploadBytes:     10,
		MultipartPartBytes: MultipartMinPartBytes,
	}
	base := InitializeUploadInput{
		Filename: "input.bin", SizeBytes: 1,
		SHA256: strings.Repeat("a", 64), MIMEType: "application/octet-stream",
		Kind: KindExperimentResult, IdempotencyKey: "upload",
	}
	if _, err := service.normalizeInitialize(&base); !errors.Is(err, ErrKindInvalid) {
		t.Fatalf("expected internal kind rejection, got %v", err)
	}
	base.Kind = KindAgent
	if _, err := service.normalizeInitialize(&base); err != nil {
		t.Fatalf("expected Agent Artifact kind to normalize, got %v", err)
	}
	base.Kind = KindAttachment
	base.Filename = "../input.bin"
	if _, err := service.normalizeInitialize(&base); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected filename rejection, got %v", err)
	}
	base.Filename = "input.bin"
	invalidFolder := "not-a-uuid"
	base.FolderID = &invalidFolder
	if _, err := service.normalizeInitialize(&base); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected folder rejection, got %v", err)
	}
	base.FolderID = nil
	base.Tags = []string{"line\nbreak"}
	if _, err := service.normalizeInitialize(&base); !errors.Is(err, ErrTagInvalid) {
		t.Fatalf("expected tag rejection, got %v", err)
	}
}

func TestTransferSignerRejectsTamperingAndExpiry(t *testing.T) {
	signer, err := NewTransferSigner(
		strings.Repeat("s", 32), "http://localhost:3000",
	)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	grant, err := signer.Sign(TransferClaims{
		Kind: transferUploadPart, ProjectID: "project-1",
		UploadID: "upload-1", PartNumber: 2, SizeBytes: 10,
	}, now, time.Minute)
	if err != nil {
		t.Fatalf("sign transfer: %v", err)
	}
	parsed, _ := url.Parse(grant.URL)
	token := strings.TrimPrefix(parsed.Path, "/v1/artifact-transfers/")
	claims, err := signer.Verify(token, now.Add(30*time.Second))
	if err != nil || claims.PartNumber != 2 || claims.SizeBytes != 10 {
		t.Fatalf("verify transfer: %#v, %v", claims, err)
	}
	if _, err := signer.Verify(token+"x", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected tampered token rejection, got %v", err)
	}
	if _, err := signer.Verify(token, now.Add(time.Minute)); !errors.Is(
		err, ErrTransferExpired,
	) {
		t.Fatalf("expected expired token, got %v", err)
	}
}

func TestAgentUploadOwnershipIsInstanceScoped(t *testing.T) {
	service := Service{}
	upload := UploadSession{AgentInstanceID: "agent-1", CreatedBy: "user-1"}
	if !service.uploadOwnedBy(auth.Identity{
		AgentInstanceID: "agent-1", Kind: "agent",
	}, upload) {
		t.Fatal("owning Agent instance cannot continue its upload")
	}
	if service.uploadOwnedBy(auth.Identity{
		AgentInstanceID: "agent-2", Kind: "agent",
	}, upload) {
		t.Fatal("another Agent instance can continue the upload")
	}
	if !service.uploadOwnedBy(auth.Identity{
		Kind: "session", User: auth.User{ID: "maintainer-1"},
	}, upload) {
		t.Fatal("authorized human operators must retain upload recovery access")
	}
}

type recordingAgentRunValidator struct {
	agentInstanceID string
	projectID       string
	sessionID       string
	runID           string
}

func (validator *recordingAgentRunValidator) ValidateProvenance(
	_ context.Context,
	agentInstanceID string,
	projectID string,
	sessionID string,
	runID string,
) error {
	validator.agentInstanceID = agentInstanceID
	validator.projectID = projectID
	validator.sessionID = sessionID
	validator.runID = runID
	return nil
}

func TestAgentRunProvenanceUsesAgentThenProjectArgumentOrder(t *testing.T) {
	validator := &recordingAgentRunValidator{}
	service := Service{AgentRuns: validator}
	err := service.validateAgentRunProvenance(
		context.Background(),
		auth.Identity{AgentInstanceID: "agent-1", Kind: "agent"},
		"project-1", "session-1", "run-1",
	)
	if err != nil {
		t.Fatalf("validate provenance: %v", err)
	}
	if validator.agentInstanceID != "agent-1" || validator.projectID != "project-1" ||
		validator.sessionID != "session-1" || validator.runID != "run-1" {
		t.Fatalf("provenance arguments were reordered: %#v", validator)
	}
}

func TestVerifyObjectChecksDeclaredSizeBeforeHashing(t *testing.T) {
	storage, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	ctx := context.Background()
	upload, err := storage.CreateMultipart(ctx, "staging/size-check", "text/plain")
	if err != nil {
		t.Fatalf("create multipart: %v", err)
	}
	contents := []byte("artifact")
	part, err := storage.PutPart(
		ctx, upload, 1, bytes.NewReader(contents), int64(len(contents)),
	)
	if err != nil {
		t.Fatalf("write multipart part: %v", err)
	}
	if _, err := storage.CompleteMultipart(ctx, upload, []CompletedPart{part}); err != nil {
		t.Fatalf("complete multipart: %v", err)
	}
	service := Service{Storage: storage}
	if err := service.verifyObject(
		ctx, upload.ObjectKey, int64(len(contents))+1, digest(contents),
	); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("expected full-object size mismatch, got %v", err)
	}
}

func TestArtifactRBACMatchesFrozenRoles(t *testing.T) {
	cases := []struct {
		role     project.Role
		read     bool
		upload   bool
		download bool
		delete   bool
	}{
		{project.RoleOwner, true, true, true, true},
		{project.RoleMaintainer, true, true, true, true},
		{project.RoleEditor, true, true, true, false},
		{project.RoleViewer, true, false, true, false},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.role), func(t *testing.T) {
			access := roleAccess{role: testCase.role}
			service := Service{Access: access}
			caller := auth.Identity{User: auth.User{ID: "user-1"}}
			checks := []struct {
				permission project.Permission
				allowed    bool
			}{
				{project.PermissionArtifactRead, testCase.read},
				{project.PermissionArtifactUpload, testCase.upload},
				{project.PermissionArtifactDownload, testCase.download},
				{project.PermissionArtifactDelete, testCase.delete},
			}
			for _, check := range checks {
				err := service.authorize(
					context.Background(), caller, "project-1", check.permission,
				)
				if (err == nil) != check.allowed {
					t.Fatalf(
						"permission %s allowed=%v, got %v",
						check.permission, check.allowed, err,
					)
				}
			}
		})
	}
}

func TestFolderInputValidationAndPermissions(t *testing.T) {
	if !validFolderUUID("550e8400-e29b-41d4-a716-446655440000") {
		t.Fatal("expected UUID-shaped folder ID to be accepted")
	}
	for _, name := range []string{"", " ", "a\nb", strings.Repeat("x", 256)} {
		if validFolderName(name) {
			t.Fatalf("expected folder name %q to be rejected", name)
		}
	}
	if !validFolderName("Research Data") {
		t.Fatal("expected ordinary folder name to be accepted")
	}

	service := Service{Access: roleAccess{role: project.RoleEditor}}
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}}
	if err := service.DeleteFolder(
		context.Background(), identity,
		"550e8400-e29b-41d4-a716-446655440000",
		"550e8400-e29b-41d4-a716-446655440001",
		false,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor should not delete folders: %v", err)
	}
	if _, err := service.MoveFolder(
		context.Background(), auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}},
		"550e8400-e29b-41d4-a716-446655440000",
		"550e8400-e29b-41d4-a716-446655440001",
		MoveFolderInput{ParentFolderID: stringPointer("not-a-uuid")},
	); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid parent folder ID should be rejected: %v", err)
	}
}

func stringPointer(value string) *string { return &value }

type roleAccess struct {
	role project.Role
}

type staticGitReader struct {
	contents  string
	projectID string
	reference GitReference
}

func (reader staticGitReader) Open(
	_ context.Context,
	projectID string,
	reference GitReference,
) (io.ReadCloser, int64, error) {
	if projectID != reader.projectID || reference != reader.reference {
		return nil, 0, ErrNotFound
	}
	return io.NopCloser(strings.NewReader(reader.contents)),
		int64(len(reader.contents)), nil
}

func (access roleAccess) Authenticate(
	context.Context,
	string,
) (auth.Identity, error) {
	return auth.Identity{}, nil
}

func (access roleAccess) Authorize(
	_ context.Context,
	_ auth.Identity,
	_ string,
	permission project.Permission,
) error {
	switch access.role {
	case project.RoleOwner, project.RoleMaintainer:
		return nil
	case project.RoleEditor:
		if permission != project.PermissionArtifactDelete {
			return nil
		}
	case project.RoleViewer:
		if permission == project.PermissionArtifactRead ||
			permission == project.PermissionArtifactDownload {
			return nil
		}
	}
	return project.ErrForbidden
}

func newLocalTestService(
	t *testing.T,
	store Store,
	access Access,
	now time.Time,
) Service {
	t.Helper()
	local, err := NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	signer, err := NewTransferSigner(
		strings.Repeat("s", 32), "http://localhost:3000",
	)
	if err != nil {
		t.Fatalf("create transfer signer: %v", err)
	}
	return Service{
		Access: access, Clock: clock.Fixed{Time: now},
		Generator:          identity.Generator{},
		MaxUploadBytes:     20 * 1024 * 1024,
		MultipartPartBytes: MultipartMinPartBytes,
		Signer:             signer, Storage: local, Store: store,
		TransferTTL: time.Minute, UploadSessionTTL: time.Hour,
	}
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func grantToken(t *testing.T, grant TransferGrant) string {
	t.Helper()
	parsed, err := url.Parse(grant.URL)
	if err != nil {
		t.Fatalf("parse grant URL: %v", err)
	}
	return strings.TrimPrefix(parsed.Path, "/v1/artifact-transfers/")
}

func uploadGrantedPart(
	t *testing.T,
	service Service,
	grant PartGrant,
	contents []byte,
) CompletedPart {
	t.Helper()
	part, err := service.PutSignedPart(
		context.Background(), grantToken(t, grant.Transfer),
		bytes.NewReader(contents), int64(len(contents)),
	)
	if err != nil {
		t.Fatalf("put signed part %d: %v", grant.PartNumber, err)
	}
	return part
}
