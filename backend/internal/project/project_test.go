package project

import (
	"context"
	"errors"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/auth"
)

type authStub struct {
	identity auth.Identity
}

func (stub authStub) Authenticate(context.Context, string) (auth.Identity, error) {
	return stub.identity, nil
}

type storeStub struct {
	role Role
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
	return nil
}
func (stub *storeStub) Update(context.Context, string, string, UpdateInput) (Project, error) {
	return Project{ID: "project-1", Role: stub.role}, nil
}
func (stub *storeStub) UpsertMember(context.Context, string, string, string, Role) (Member, error) {
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
