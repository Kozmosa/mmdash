package project

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
)

type authStub struct {
	identity auth.Identity
}

func (stub authStub) Authenticate(context.Context, string) (auth.Identity, error) {
	return stub.identity, nil
}

type agentGrantResolverStub struct {
	role Role
}

func (stub agentGrantResolverStub) ResolveAgentRole(
	context.Context,
	string,
	string,
) (Role, error) {
	return stub.role, nil
}

type storeStub struct {
	invitationCreated  bool
	invitationDeclined bool
	memberRemoved      bool
	memberUpdated      bool
	ownershipMoved     bool
	projectRestored    bool
	projectTrashed     bool
	projectUpdated     bool
	purgeAt            time.Time
	role               Role
}

type artifactValidatorStub struct {
	err       error
	projectID string
	ids       []string
}

func (stub *artifactValidatorStub) ValidateProjectReferences(
	_ context.Context,
	projectID string,
	ids []string,
) error {
	stub.projectID = projectID
	stub.ids = append([]string{}, ids...)
	return stub.err
}

func (stub *storeStub) AcceptInvitation(context.Context, string, string, string, time.Time) (Member, error) {
	return Member{UserID: "user-2", Role: RoleViewer}, nil
}
func (stub *storeStub) AcceptInvitationByID(context.Context, string, string, string, time.Time) (Member, error) {
	return Member{UserID: "user-2", Role: RoleViewer}, nil
}
func (stub *storeStub) CreateInvitation(context.Context, string, string, string, Role, time.Time) (IssuedInvitation, error) {
	stub.invitationCreated = true
	return IssuedInvitation{}, nil
}
func (stub *storeStub) DeclineInvitation(context.Context, string, time.Time) error {
	stub.invitationDeclined = true
	return nil
}
func (stub *storeStub) ListInvitations(context.Context, string, time.Time) ([]Invitation, error) {
	return []Invitation{}, nil
}
func (stub *storeStub) PreviewInvitation(context.Context, string, time.Time) (Invitation, error) {
	return Invitation{}, nil
}
func (stub *storeStub) RevokeInvitation(context.Context, string, string, string, time.Time) error {
	return nil
}

func (stub *storeStub) Create(_ context.Context, userID string, input CreateInput) (Project, error) {
	return Project{ID: "project-1", Name: input.Name, Role: RoleOwner, CreatedBy: userID}, nil
}
func (stub *storeStub) FindRole(context.Context, string, string) (Role, error) {
	if stub.role == "" {
		return "", ErrNotFound
	}
	return stub.role, nil
}
func (stub *storeStub) Get(context.Context, string, string) (Project, error) {
	return Project{ID: "project-1", Role: stub.role}, nil
}
func (stub *storeStub) List(context.Context, string, bool) ([]Project, error) {
	return []Project{{ID: "project-1", Role: stub.role}}, nil
}
func (stub *storeStub) ListTrash(context.Context, string, time.Time) ([]Project, error) {
	return []Project{{ID: "project-trashed", Role: RoleOwner}}, nil
}
func (stub *storeStub) ListMembers(context.Context, string) ([]Member, error) {
	return []Member{{UserID: "user-1", Role: stub.role}}, nil
}
func (stub *storeStub) PurgeExpired(context.Context, time.Time) error {
	return nil
}
func (stub *storeStub) RemoveMember(context.Context, string, string, string) error {
	stub.memberRemoved = true
	return nil
}
func (stub *storeStub) Restore(context.Context, string, string, time.Time) (Project, error) {
	stub.projectRestored = true
	return Project{ID: "project-1", Role: RoleOwner}, nil
}
func (stub *storeStub) Update(context.Context, string, string, UpdateInput) (Project, error) {
	stub.projectUpdated = true
	return Project{ID: "project-1", Role: stub.role}, nil
}
func (stub *storeStub) TransferOwnership(context.Context, string, string, string) (Member, error) {
	stub.ownershipMoved = true
	return Member{UserID: "user-2", Role: RoleOwner}, nil
}
func (stub *storeStub) Trash(_ context.Context, _ string, _ string, _ time.Time, purgeAt time.Time) (Project, error) {
	stub.projectTrashed = true
	stub.purgeAt = purgeAt
	return Project{ID: "project-1", Role: RoleOwner}, nil
}
func (stub *storeStub) UpsertMember(context.Context, string, string, string, Role) (Member, error) {
	stub.memberUpdated = true
	return Member{UserID: "user-2", Role: RoleViewer}, nil
}

func TestOwnerCanManageTeamAndViewerCannot(t *testing.T) {
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}}
	store := &storeStub{role: RoleOwner}
	service := Service{Auth: authStub{identity: identity}, Store: store}
	if err := service.Authorize(
		context.Background(),
		identity,
		"project-1",
		PermissionMembersManage,
	); err != nil {
		t.Fatalf("owner should manage members: %v", err)
	}
	store.role = RoleViewer
	if err := service.Authorize(
		context.Background(),
		identity,
		"project-1",
		PermissionMembersManage,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer should be forbidden, got %v", err)
	}
}

func TestSettingsPermissionsFollowCollaborationRoles(t *testing.T) {
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}}
	store := &storeStub{role: RoleMaintainer}
	service := Service{Auth: authStub{identity: identity}, Store: store}
	if err := service.AuthorizeSettings(
		context.Background(),
		identity,
		"project-1",
		true,
	); err != nil {
		t.Fatalf("maintainer should manage settings: %v", err)
	}
	store.role = RoleViewer
	if err := service.AuthorizeSettings(
		context.Background(),
		identity,
		"project-1",
		false,
	); err != nil {
		t.Fatalf("viewer should read settings: %v", err)
	}
	if err := service.AuthorizeSettings(
		context.Background(),
		identity,
		"project-1",
		true,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer should not manage settings, got %v", err)
	}
}

func TestProjectSourceArtifactsUseTwoStepValidatedReferences(t *testing.T) {
	identity := auth.Identity{
		Kind: "session", User: auth.User{ID: "user-1"},
	}
	store := &storeStub{role: RoleOwner}
	validator := &artifactValidatorStub{}
	service := Service{
		Artifacts: validator, Auth: authStub{identity: identity}, Store: store,
	}
	if _, err := service.Create(
		context.Background(), identity,
		CreateInput{Name: "Project", SourceArtifactIDs: []string{"artifact-1"}},
	); !errors.Is(err, ErrInvalid) {
		t.Fatalf("project create must remain the first step, got %v", err)
	}
	sourceIDs := []string{
		"00000000-0000-4000-8000-000000000101",
		"00000000-0000-4000-8000-000000000102",
	}
	if _, err := service.Update(
		context.Background(), identity, "project-1",
		UpdateInput{SourceArtifactIDs: &sourceIDs},
	); err != nil {
		t.Fatalf("validated source update: %v", err)
	}
	if !store.projectUpdated ||
		validator.projectID != "project-1" ||
		len(validator.ids) != 2 {
		t.Fatalf("source references did not cross the narrow boundary: %#v", validator)
	}
	validator.err = errors.New("cross-project or unavailable")
	if _, err := service.Update(
		context.Background(), identity, "project-1",
		UpdateInput{SourceArtifactIDs: &sourceIDs},
	); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid Artifact references must be rejected, got %v", err)
	}
}

func TestOnlyOwnerCanTrashAndRestoreProject(t *testing.T) {
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "owner"}}
	store := &storeStub{role: RoleOwner}
	service := Service{
		Auth:           authStub{identity: identity},
		Clock:          fixedProjectClock{now: now},
		Store:          store,
		TrashRetention: 30 * 24 * time.Hour,
	}

	if _, err := service.Trash(context.Background(), identity, "project-1"); err != nil {
		t.Fatalf("trash project: %v", err)
	}
	if !store.projectTrashed || !store.purgeAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("trash window was not persisted: %#v", store)
	}
	if _, err := service.Restore(context.Background(), identity, "project-1"); err != nil {
		t.Fatalf("restore project: %v", err)
	}
	if !store.projectRestored {
		t.Fatal("restore did not reach persistence")
	}

	store.role = RoleMaintainer
	store.projectTrashed = false
	if _, err := service.Trash(context.Background(), identity, "project-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("maintainer trashed project: %v", err)
	}
	if store.projectTrashed {
		t.Fatal("forbidden trash reached persistence")
	}
}

func TestProjectScopedTokenCannotCrossProjects(t *testing.T) {
	identity := auth.Identity{
		Kind:      "agent",
		ProjectID: "project-1",
		User:      auth.User{ID: "agent-user"},
	}
	service := Service{
		Auth:  authStub{identity: identity},
		Store: &storeStub{role: RoleAgent},
	}
	if err := service.Authorize(
		context.Background(),
		identity,
		"project-2",
		PermissionRead,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected cross-project token to be forbidden, got %v", err)
	}
}

func TestMaintainerCannotGrantOrManageOwnerRole(t *testing.T) {
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "maintainer"}}
	store := &roleStoreStub{roles: map[string]Role{"maintainer": RoleMaintainer, "owner": RoleOwner, "member": RoleViewer}}
	service := Service{Auth: authStub{identity: identity}, Store: store}
	if _, err := service.UpsertMember(context.Background(), identity, "project-1", "member", RoleOwner); !errors.Is(err, ErrForbidden) {
		t.Fatalf("maintainer granted owner: %v", err)
	}
	if err := service.RemoveMember(context.Background(), identity, "project-1", "owner"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("maintainer removed owner: %v", err)
	}
}

func TestOwnerRoleCanOnlyChangeThroughTransfer(t *testing.T) {
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "creator"}}
	store := &roleStoreStub{roles: map[string]Role{
		"creator": RoleOwner,
		"member":  RoleEditor,
	}}
	service := Service{Auth: authStub{identity: identity}, Store: store}

	if _, err := service.UpsertMember(context.Background(), identity, "project-1", "creator", RoleMaintainer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("owner changed their own role without transfer: %v", err)
	}
	if store.memberUpdated || store.ownershipMoved {
		t.Fatal("forbidden owner role change reached persistence")
	}

	member, err := service.UpsertMember(context.Background(), identity, "project-1", "member", RoleOwner)
	if err != nil {
		t.Fatalf("transfer ownership: %v", err)
	}
	if !store.ownershipMoved || member.Role != RoleOwner {
		t.Fatalf("ownership was not transferred atomically: %#v", member)
	}
}

func TestOwnerCannotRemoveSelf(t *testing.T) {
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "creator"}}
	store := &roleStoreStub{roles: map[string]Role{"creator": RoleOwner}}
	service := Service{Auth: authStub{identity: identity}, Store: store}

	if err := service.RemoveMember(context.Background(), identity, "project-1", "creator"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("owner removed self: %v", err)
	}
	if store.memberRemoved {
		t.Fatal("forbidden owner removal reached persistence")
	}
}

func TestOwnerCannotBeInvitedWithoutTransfer(t *testing.T) {
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "creator"}}
	store := &roleStoreStub{roles: map[string]Role{"creator": RoleOwner}}
	service := Service{Auth: authStub{identity: identity}, Store: store}

	if _, err := service.CreateInvitation(context.Background(), identity, "project-1", "new-owner@example.com", RoleOwner); !errors.Is(err, ErrInvalid) {
		t.Fatalf("owner invitation should be rejected, got %v", err)
	}
}

func TestUserCannotInviteSelf(t *testing.T) {
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "creator", Email: "Owner@Example.com"}}
	store := &roleStoreStub{roles: map[string]Role{"creator": RoleOwner}}
	service := Service{Auth: authStub{identity: identity}, Store: store}

	if _, err := service.CreateInvitation(context.Background(), identity, "project-1", " owner@example.com ", RoleViewer); !errors.Is(err, ErrSelfInvitation) {
		t.Fatalf("self invitation returned %v", err)
	}
	if store.invitationCreated {
		t.Fatal("self invitation reached persistence")
	}
}

func TestHumanMembershipRejectsMachineRoles(t *testing.T) {
	identity := auth.Identity{Kind: "session", User: auth.User{ID: "owner"}}
	store := &roleStoreStub{roles: map[string]Role{
		"owner":  RoleOwner,
		"member": RoleViewer,
	}}
	service := Service{Auth: authStub{identity: identity}, Store: store}

	for _, role := range []Role{RoleAgent, RoleBox} {
		if _, err := service.UpsertMember(context.Background(), identity, "project-1", "member", role); !errors.Is(err, ErrInvalid) {
			t.Errorf("human member accepted machine role %q: %v", role, err)
		}
		if _, err := service.CreateInvitation(context.Background(), identity, "project-1", "human@example.com", role); !errors.Is(err, ErrInvalid) {
			t.Errorf("human invitation accepted machine role %q: %v", role, err)
		}
	}
	if store.memberUpdated || store.ownershipMoved {
		t.Fatal("invalid machine role reached persistence")
	}
}

func TestInvitationCanBeDeclinedWithoutAuthentication(t *testing.T) {
	store := &storeStub{}
	service := Service{Store: store}

	if err := service.DeclineInvitation(context.Background(), "invitation-token"); err != nil {
		t.Fatalf("decline invitation: %v", err)
	}
	if !store.invitationDeclined {
		t.Fatal("decline did not reach persistence")
	}
	if err := service.DeclineInvitation(context.Background(), " "); !errors.Is(err, auth.ErrInvalidInvitation) {
		t.Fatalf("blank decline token returned %v", err)
	}
}

func TestModuleRoutesProjectAndMemberMutationsToTheirHandlers(t *testing.T) {
	store := &roleStoreStub{roles: map[string]Role{
		"user-1": RoleOwner,
		"user-2": RoleViewer,
	}}
	module := Module{Service: Service{
		Auth:  authStub{identity: auth.Identity{Kind: "session", User: auth.User{ID: "user-1"}}},
		Store: store,
	}}
	mux := http.NewServeMux()
	module.RegisterRoutes(mux)

	for _, test := range []struct {
		body   string
		method string
		path   string
		status int
	}{
		{body: `{"name":"Renamed"}`, method: http.MethodPatch, path: "/v1/projects/project-1", status: http.StatusOK},
		{method: http.MethodDelete, path: "/v1/projects/project-1", status: http.StatusNoContent},
		{method: http.MethodPost, path: "/v1/projects/project-1/restore", status: http.StatusOK},
		{body: `{"role":"viewer"}`, method: http.MethodPut, path: "/v1/projects/project-1/members/user-2", status: http.StatusOK},
		{method: http.MethodDelete, path: "/v1/projects/project-1/members/user-2", status: http.StatusNoContent},
	} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		request.Header.Set("Authorization", "Bearer test")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("%s %s: got %d, want %d", test.method, test.path, response.Code, test.status)
		}
	}
	if !store.projectUpdated || !store.projectTrashed || !store.projectRestored || !store.memberUpdated || !store.memberRemoved {
		t.Fatalf("expected all mutation handlers to run: %#v", store)
	}
}

func TestModuleRoutesInboxInvitationAcceptByID(t *testing.T) {
	module := Module{Service: Service{
		Auth:  authStub{identity: auth.Identity{Kind: "session", User: auth.User{ID: "user-2", Email: "invitee@example.com"}}},
		Store: &storeStub{},
	}}
	mux := http.NewServeMux()
	module.RegisterRoutes(mux)
	request := httptest.NewRequest(http.MethodPost, "/v1/projects/invitations/invitation-1/accept", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("invitation accept route: got %d, want %d", response.Code, http.StatusOK)
	}
}

func TestRepositoryPermissionsMatchCollaborationRoles(t *testing.T) {
	cases := []struct {
		role   Role
		read   bool
		manage bool
		write  bool
	}{
		{role: RoleOwner, read: true, manage: true, write: true},
		{role: RoleMaintainer, read: true, manage: true, write: true},
		{role: RoleEditor, read: true, write: true},
		{role: RoleViewer, read: true},
		{role: RoleAgent, read: true},
		{role: RoleBox, read: true},
	}
	for _, testCase := range cases {
		permissions := permissionsByRole[testCase.role]
		if hasPermission(permissions, PermissionRepoRead) != testCase.read ||
			hasPermission(permissions, PermissionRepoManage) != testCase.manage ||
			hasPermission(permissions, PermissionRepoWrite) != testCase.write {
			t.Fatalf(
				"unexpected Repo permissions for %s: %#v",
				testCase.role, permissions,
			)
		}
	}
}

func TestArtifactPermissionsMatchCollaborationRoles(t *testing.T) {
	cases := []struct {
		role     Role
		read     bool
		upload   bool
		download bool
		delete   bool
	}{
		{RoleOwner, true, true, true, true},
		{RoleMaintainer, true, true, true, true},
		{RoleEditor, true, true, true, false},
		{RoleViewer, true, false, true, false},
		{RoleAgent, true, false, true, false},
		{RoleBox, false, false, false, false},
	}
	for _, testCase := range cases {
		permissions := permissionsByRole[testCase.role]
		if hasPermission(permissions, PermissionArtifactRead) != testCase.read ||
			hasPermission(permissions, PermissionArtifactUpload) != testCase.upload ||
			hasPermission(permissions, PermissionArtifactDownload) != testCase.download ||
			hasPermission(permissions, PermissionArtifactDelete) != testCase.delete {
			t.Fatalf(
				"unexpected Artifact permissions for %s: %#v",
				testCase.role, permissions,
			)
		}
	}
}

func TestAgentRoleIsReadOnlyOutsideExplicitContextPromotion(t *testing.T) {
	permissions := permissionsByRole[RoleAgent]
	for _, permission := range []Permission{
		PermissionRead,
		PermissionDataRead,
		PermissionContextPropose,
		PermissionRepoRead,
		PermissionArtifactRead,
		PermissionArtifactDownload,
		PermissionProgressRead,
	} {
		if !hasPermission(permissions, permission) {
			t.Fatalf("Agent role is missing Stage 5 permission %q", permission)
		}
	}
	for _, permission := range []Permission{
		PermissionSettingsRead,
		PermissionJobsCreate,
		PermissionJobsRead,
		PermissionJobsCancel,
		PermissionRepoManage,
		PermissionRepoWrite,
		PermissionArtifactUpload,
		PermissionArtifactDelete,
		PermissionProgressManage,
		PermissionProgressPropose,
		PermissionAgentUse,
		PermissionAgentManage,
		PermissionAgentTokensManage,
	} {
		if hasPermission(permissions, permission) {
			t.Fatalf("Agent role unexpectedly grants mutation permission %q", permission)
		}
	}
}

func TestAgentAuthorizationRequiresMatchingProductToolGrant(t *testing.T) {
	service := Service{AgentGrants: agentGrantResolverStub{role: RoleAgent}}
	identity := auth.Identity{
		AgentInstanceID: "agent-1", CredentialStatus: "active", Kind: "agent",
		ProjectID: "project-1",
	}
	tests := []struct {
		name       string
		tools      []string
		permission Permission
		allowed    bool
	}{
		{name: "project read", tools: []string{"project.get"}, permission: PermissionRead, allowed: true},
		{name: "artifact through data read", tools: []string{"data.read"}, permission: PermissionArtifactRead, allowed: true},
		{name: "repo through data read", tools: []string{"data.read"}, permission: PermissionRepoRead, allowed: true},
		{name: "data list", tools: []string{"data.list"}, permission: PermissionDataRead, allowed: true},
		{name: "context proposal", tools: []string{"context.promote"}, permission: PermissionContextPropose, allowed: true},
		{name: "list cannot download", tools: []string{"data.list"}, permission: PermissionArtifactDownload},
		{name: "project tool cannot read artifact", tools: []string{"project.get"}, permission: PermissionArtifactRead},
		{name: "no tool grants management", tools: []string{"data.read"}, permission: PermissionProgressManage},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			caller := identity
			caller.AllowedTools = testCase.tools
			err := service.Authorize(context.Background(), caller, "project-1", testCase.permission)
			if (err == nil) != testCase.allowed {
				t.Fatalf("authorization result: allowed=%v err=%v", testCase.allowed, err)
			}
		})
	}
}

func hasPermission(permissions []Permission, expected Permission) bool {
	for _, permission := range permissions {
		if permission == expected {
			return true
		}
	}
	return false
}

type fixedProjectClock struct {
	now time.Time
}

func (clock fixedProjectClock) Now() time.Time {
	return clock.now
}

type roleStoreStub struct {
	storeStub
	roles map[string]Role
}

func (stub *roleStoreStub) FindRole(_ context.Context, userID string, _ string) (Role, error) {
	role, ok := stub.roles[userID]
	if !ok {
		return "", ErrNotFound
	}
	return role, nil
}
