// Package project owns collaborative projects, membership, roles, and authorization.
package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
)

// Role is one of the stable collaboration roles.
type Role string

const (
	RoleOwner      Role = "owner"
	RoleMaintainer Role = "maintainer"
	RoleEditor     Role = "editor"
	RoleViewer     Role = "viewer"
	RoleAgent      Role = "agent"
	RoleBox        Role = "box"
)

// Permission is a stable authorization capability.
type Permission string

const (
	PermissionRead           Permission = "project.read"
	PermissionUpdate         Permission = "project.update"
	PermissionArchive        Permission = "project.archive"
	PermissionMembersManage  Permission = "project.members.manage"
	PermissionSettingsManage Permission = "project.settings.manage"
	PermissionSettingsRead   Permission = "project.settings.read"
	PermissionTokensManage   Permission = "project.tokens.manage"
)

var permissionsByRole = map[Role][]Permission{
	RoleOwner: {
		PermissionRead,
		PermissionUpdate,
		PermissionArchive,
		PermissionMembersManage,
		PermissionSettingsManage,
		PermissionSettingsRead,
		PermissionTokensManage,
	},
	RoleMaintainer: {
		PermissionRead,
		PermissionUpdate,
		PermissionMembersManage,
		PermissionSettingsManage,
		PermissionSettingsRead,
		PermissionTokensManage,
	},
	RoleEditor: {PermissionRead, PermissionUpdate, PermissionSettingsRead},
	RoleViewer: {PermissionRead, PermissionSettingsRead},
	RoleAgent:  {PermissionRead, PermissionSettingsRead},
	RoleBox:    {PermissionRead, PermissionSettingsRead},
}

// Project is the authoritative project record.
type Project struct {
	ArchivedAt         *time.Time `json:"archived_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	CreatedBy          string     `json:"created_by"`
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	ProblemSummary     string     `json:"problem_summary"`
	ProblemTitle       string     `json:"problem_title"`
	ProjectConstraints []string   `json:"project_constraints"`
	Role               Role       `json:"role"`
	SourceArtifactIDs  []string   `json:"source_artifact_ids"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// Member is a collaborative project member.
type Member struct {
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email"`
	JoinedAt    time.Time `json:"joined_at"`
	Role        Role      `json:"role"`
	UserID      string    `json:"user_id"`
}

// CreateInput contains project fields accepted at creation.
type CreateInput struct {
	Name               string   `json:"name"`
	ProblemSummary     string   `json:"problem_summary"`
	ProblemTitle       string   `json:"problem_title"`
	ProjectConstraints []string `json:"project_constraints"`
	SourceArtifactIDs  []string `json:"source_artifact_ids"`
}

// UpdateInput contains partial project edits.
type UpdateInput struct {
	Archived           *bool     `json:"archived,omitempty"`
	Name               *string   `json:"name,omitempty"`
	ProblemSummary     *string   `json:"problem_summary,omitempty"`
	ProblemTitle       *string   `json:"problem_title,omitempty"`
	ProjectConstraints *[]string `json:"project_constraints,omitempty"`
	SourceArtifactIDs  *[]string `json:"source_artifact_ids,omitempty"`
}

// Store is the project persistence boundary.
type Store interface {
	Create(context.Context, string, CreateInput) (Project, error)
	FindRole(context.Context, string, string) (Role, error)
	Get(context.Context, string, string) (Project, error)
	List(context.Context, string, bool) ([]Project, error)
	ListMembers(context.Context, string) ([]Member, error)
	RemoveMember(context.Context, string, string, string) error
	Update(context.Context, string, string, UpdateInput) (Project, error)
	UpsertMember(context.Context, string, string, string, Role) (Member, error)
}

// Authenticator resolves trusted caller identity.
type Authenticator interface {
	Authenticate(context.Context, string) (auth.Identity, error)
}

// Service applies collaboration and RBAC policy.
type Service struct {
	Auth  Authenticator
	Store Store
}

// Authenticate resolves an identity through the Auth module.
func (service Service) Authenticate(ctx context.Context, authorization string) (auth.Identity, error) {
	return service.Auth.Authenticate(ctx, authorization)
}

// Create creates a project and assigns the caller as owner.
func (service Service) Create(
	ctx context.Context,
	identity auth.Identity,
	input CreateInput,
) (Project, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.ProblemTitle = strings.TrimSpace(input.ProblemTitle)
	input.ProblemSummary = strings.TrimSpace(input.ProblemSummary)
	if input.Name == "" {
		return Project{}, ErrInvalid
	}
	if identity.Kind != "session" && identity.Kind != "api" {
		return Project{}, ErrForbidden
	}
	return service.Store.Create(ctx, identity.User.ID, input)
}

// List returns projects visible to the caller.
func (service Service) List(
	ctx context.Context,
	identity auth.Identity,
	includeArchived bool,
) ([]Project, error) {
	return service.Store.List(ctx, identity.User.ID, includeArchived)
}

// Get returns one project after permission checks.
func (service Service) Get(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (Project, error) {
	if err := service.Authorize(ctx, identity, projectID, PermissionRead); err != nil {
		return Project{}, err
	}
	return service.Store.Get(ctx, identity.User.ID, projectID)
}

// Update edits or archives a project under RBAC.
func (service Service) Update(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	input UpdateInput,
) (Project, error) {
	permission := PermissionUpdate
	if input.Archived != nil {
		permission = PermissionArchive
	}
	if err := service.Authorize(ctx, identity, projectID, permission); err != nil {
		return Project{}, err
	}
	if input.Name != nil {
		trimmed := strings.TrimSpace(*input.Name)
		if trimmed == "" {
			return Project{}, ErrInvalid
		}
		input.Name = &trimmed
	}
	return service.Store.Update(ctx, identity.User.ID, projectID, input)
}

// ListMembers returns the project team.
func (service Service) ListMembers(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) ([]Member, error) {
	if err := service.Authorize(ctx, identity, projectID, PermissionRead); err != nil {
		return nil, err
	}
	return service.Store.ListMembers(ctx, projectID)
}

// UpsertMember adds or changes a team member role.
func (service Service) UpsertMember(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	userID string,
	role Role,
) (Member, error) {
	if err := service.Authorize(ctx, identity, projectID, PermissionMembersManage); err != nil {
		return Member{}, err
	}
	if _, ok := permissionsByRole[role]; !ok {
		return Member{}, ErrInvalid
	}
	return service.Store.UpsertMember(ctx, identity.User.ID, projectID, userID, role)
}

// RemoveMember removes a collaborator while preserving at least one owner.
func (service Service) RemoveMember(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	userID string,
) error {
	if err := service.Authorize(ctx, identity, projectID, PermissionMembersManage); err != nil {
		return err
	}
	return service.Store.RemoveMember(ctx, identity.User.ID, projectID, userID)
}

// Permissions returns the current role and effective permissions.
func (service Service) Permissions(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (Role, []Permission, error) {
	role, err := service.Store.FindRole(ctx, identity.User.ID, projectID)
	if err != nil {
		return "", nil, ErrForbidden
	}
	if identity.ProjectID != "" && identity.ProjectID != projectID {
		return "", nil, ErrForbidden
	}
	return role, append([]Permission(nil), permissionsByRole[role]...), nil
}

// Authorize verifies membership, token scope, and role capability.
func (service Service) Authorize(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	required Permission,
) error {
	role, permissions, err := service.Permissions(ctx, identity, projectID)
	if err != nil {
		return err
	}
	_ = role
	for _, permission := range permissions {
		if permission == required {
			return nil
		}
	}
	return ErrForbidden
}

// AuthorizeTokenManagement lets Auth enforce project scope without owning RBAC data.
func (service Service) AuthorizeTokenManagement(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) error {
	return service.Authorize(ctx, identity, projectID, PermissionTokensManage)
}

// AuthorizeSettings lets Settings enforce project scope without owning RBAC.
func (service Service) AuthorizeSettings(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	manage bool,
) error {
	permission := PermissionSettingsRead
	if manage {
		permission = PermissionSettingsManage
	}
	return service.Authorize(ctx, identity, projectID, permission)
}

var (
	ErrConflict  = errors.New("project conflict")
	ErrForbidden = errors.New("project permission denied")
	ErrInvalid   = errors.New("invalid project input")
	ErrNotFound  = errors.New("project not found")
)

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
