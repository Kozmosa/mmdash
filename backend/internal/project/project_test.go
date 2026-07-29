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

type storeStub struct {
	memberRemoved  bool
	memberUpdated  bool
	projectUpdated bool
	role           Role
}

func (stub *storeStub) AcceptInvitation(context.Context, string, string, string, time.Time) (Member, error) {
	return Member{UserID: "user-2", Role: RoleViewer}, nil
}
func (stub *storeStub) CreateInvitation(context.Context, string, string, string, Role, time.Time) (IssuedInvitation, error) {
	return IssuedInvitation{}, nil
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
func (stub *storeStub) ListMembers(context.Context, string) ([]Member, error) {
	return []Member{{UserID: "user-1", Role: stub.role}}, nil
}
func (stub *storeStub) RemoveMember(context.Context, string, string, string) error {
	stub.memberRemoved = true
	return nil
}
func (stub *storeStub) Update(context.Context, string, string, UpdateInput) (Project, error) {
	stub.projectUpdated = true
	return Project{ID: "project-1", Role: stub.role}, nil
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

func TestModuleRoutesProjectAndMemberMutationsToTheirHandlers(t *testing.T) {
	store := &storeStub{role: RoleOwner}
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
	if !store.projectUpdated || !store.memberUpdated || !store.memberRemoved {
		t.Fatalf("expected all mutation handlers to run: %#v", store)
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

func hasPermission(permissions []Permission, expected Permission) bool {
	for _, permission := range permissions {
		if permission == expected {
			return true
		}
	}
	return false
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
