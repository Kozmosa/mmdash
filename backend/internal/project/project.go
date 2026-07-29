// Package project owns collaborative projects, membership, roles, and authorization.
package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
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
	PermissionJobsCreate     Permission = "project.jobs.create"
	PermissionJobsRead       Permission = "project.jobs.read"
	PermissionJobsCancel     Permission = "project.jobs.cancel"
	PermissionDataRead       Permission = "project.data.read"
	PermissionContextPropose Permission = "project.context.propose"
	PermissionContextReview  Permission = "project.context.review"
	PermissionAuditRead      Permission = "project.audit.read"
	PermissionAuditWrite     Permission = "project.audit.write"
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
		PermissionJobsCreate,
		PermissionJobsRead,
		PermissionJobsCancel,
		PermissionDataRead,
		PermissionContextPropose,
		PermissionContextReview,
		PermissionAuditRead,
		PermissionAuditWrite,
	},
	RoleMaintainer: {
		PermissionRead,
		PermissionUpdate,
		PermissionMembersManage,
		PermissionSettingsManage,
		PermissionSettingsRead,
		PermissionTokensManage,
		PermissionJobsCreate,
		PermissionJobsRead,
		PermissionJobsCancel,
		PermissionDataRead,
		PermissionContextPropose,
		PermissionContextReview,
		PermissionAuditRead,
		PermissionAuditWrite,
	},
	RoleEditor: {
		PermissionRead,
		PermissionUpdate,
		PermissionSettingsRead,
		PermissionJobsCreate,
		PermissionJobsRead,
		PermissionJobsCancel,
		PermissionDataRead,
		PermissionContextPropose,
		PermissionContextReview,
		PermissionAuditWrite,
	},
	RoleViewer: {
		PermissionRead,
		PermissionSettingsRead,
		PermissionJobsRead,
		PermissionDataRead,
		PermissionAuditWrite,
	},
	RoleAgent: {
		PermissionRead,
		PermissionSettingsRead,
		PermissionJobsCreate,
		PermissionJobsRead,
		PermissionJobsCancel,
		PermissionDataRead,
		PermissionContextPropose,
		PermissionAuditWrite,
	},
	RoleBox: {
		PermissionRead,
		PermissionSettingsRead,
		PermissionJobsRead,
		PermissionDataRead,
		PermissionAuditWrite,
	},
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

// Invitation is a pending or historical membership invitation.
type Invitation struct {
	CreatedAt   time.Time `json:"created_at"`
	Email       string    `json:"email"`
	ExpiresAt   time.Time `json:"expires_at"`
	ID          string    `json:"id"`
	InvitedBy   string    `json:"invited_by"`
	ProjectID   string    `json:"project_id"`
	ProjectName string    `json:"project_name"`
	Role        Role      `json:"role"`
	Status      string    `json:"status"`
}

// IssuedInvitation returns the secret only when a new invitation is created.
type IssuedInvitation struct {
	Invitation Invitation `json:"invitation"`
	Token      string     `json:"token,omitempty"`
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
	AcceptInvitation(context.Context, string, string, string, time.Time) (Member, error)
	CreateInvitation(context.Context, string, string, string, Role, time.Time) (IssuedInvitation, error)
	Create(context.Context, string, CreateInput) (Project, error)
	DeclineInvitation(context.Context, string, time.Time) error
	FindRole(context.Context, string, string) (Role, error)
	Get(context.Context, string, string) (Project, error)
	List(context.Context, string, bool) ([]Project, error)
	ListMembers(context.Context, string) ([]Member, error)
	ListInvitations(context.Context, string, time.Time) ([]Invitation, error)
	PreviewInvitation(context.Context, string, time.Time) (Invitation, error)
	RemoveMember(context.Context, string, string, string) error
	RevokeInvitation(context.Context, string, string, string, time.Time) error
	TransferOwnership(context.Context, string, string, string) (Member, error)
	Update(context.Context, string, string, UpdateInput) (Project, error)
	UpsertMember(context.Context, string, string, string, Role) (Member, error)
}

type transactionalInvitationStore interface {
	AcceptInvitationInTransaction(context.Context, transaction.Tx, string, string, string, time.Time) (Member, error)
}

// Authenticator resolves trusted caller identity.
type Authenticator interface {
	Authenticate(context.Context, string) (auth.Identity, error)
}

// Service applies collaboration and RBAC policy.
type Service struct {
	Auth          Authenticator
	Clock         interface{ Now() time.Time }
	Store         Store
	InvitationTTL time.Duration
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
	created, err := service.Store.Create(ctx, identity.User.ID, input)
	if err == nil {
		requestctx.SetProject(ctx, created.ID)
	}
	return created, err
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
	if !isHumanRole(role) {
		return Member{}, ErrInvalid
	}
	actorRole, err := service.Store.FindRole(ctx, identity.User.ID, projectID)
	if err != nil {
		return Member{}, ErrForbidden
	}
	targetRole, err := service.Store.FindRole(ctx, userID, projectID)
	if err != nil {
		return Member{}, ErrNotFound
	}
	if targetRole == RoleOwner {
		if role != RoleOwner {
			return Member{}, ErrForbidden
		}
		return service.Store.UpsertMember(ctx, identity.User.ID, projectID, userID, role)
	}
	if role == RoleOwner {
		if actorRole != RoleOwner || identity.User.ID == userID || !isHumanRole(targetRole) {
			return Member{}, ErrForbidden
		}
		return service.Store.TransferOwnership(ctx, identity.User.ID, projectID, userID)
	}
	return service.Store.UpsertMember(ctx, identity.User.ID, projectID, userID, role)
}

// RemoveMember removes a collaborator. An owner must transfer ownership first
// and can never remove their own active ownership through this operation.
func (service Service) RemoveMember(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	userID string,
) error {
	if err := service.Authorize(ctx, identity, projectID, PermissionMembersManage); err != nil {
		return err
	}
	if _, err := service.Store.FindRole(ctx, identity.User.ID, projectID); err != nil {
		return ErrForbidden
	}
	targetRole, err := service.Store.FindRole(ctx, userID, projectID)
	if err != nil {
		return ErrNotFound
	}
	if targetRole == RoleOwner {
		return ErrForbidden
	}
	return service.Store.RemoveMember(ctx, identity.User.ID, projectID, userID)
}

// CreateInvitation creates an email-bound, single-use membership invitation.
func (service Service) CreateInvitation(ctx context.Context, identity auth.Identity, projectID string, email string, role Role) (IssuedInvitation, error) {
	if err := service.Authorize(ctx, identity, projectID, PermissionMembersManage); err != nil {
		return IssuedInvitation{}, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return IssuedInvitation{}, ErrInvalid
	}
	if !isInvitableHumanRole(role) {
		return IssuedInvitation{}, ErrInvalid
	}
	if _, err := service.Store.FindRole(ctx, identity.User.ID, projectID); err != nil {
		return IssuedInvitation{}, ErrForbidden
	}
	ttl := service.InvitationTTL
	if ttl <= 0 {
		ttl = 72 * time.Hour
	}
	return service.Store.CreateInvitation(ctx, identity.User.ID, projectID, email, role, service.now().Add(ttl))
}

func (service Service) ListInvitations(ctx context.Context, identity auth.Identity, projectID string) ([]Invitation, error) {
	if err := service.Authorize(ctx, identity, projectID, PermissionMembersManage); err != nil {
		return nil, err
	}
	return service.Store.ListInvitations(ctx, projectID, service.now())
}

func (service Service) RevokeInvitation(ctx context.Context, identity auth.Identity, projectID string, invitationID string) error {
	if err := service.Authorize(ctx, identity, projectID, PermissionMembersManage); err != nil {
		return err
	}
	return service.Store.RevokeInvitation(ctx, identity.User.ID, projectID, invitationID, service.now())
}

func (service Service) PreviewInvitation(ctx context.Context, token string) (auth.Invitation, error) {
	invitation, err := service.Store.PreviewInvitation(ctx, hashInvitationToken(token), service.now())
	if err != nil {
		return auth.Invitation{}, auth.ErrInvalidInvitation
	}
	return authInvitation(invitation), nil
}

// DeclineInvitation permanently invalidates a pending invitation using its
// unguessable invitation token. Authentication is intentionally not required.
func (service Service) DeclineInvitation(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return auth.ErrInvalidInvitation
	}
	if err := service.Store.DeclineInvitation(ctx, hashInvitationToken(token), service.now()); err != nil {
		return auth.ErrInvalidInvitation
	}
	return nil
}

func (service Service) AcceptInvitation(ctx context.Context, identity auth.Identity, token string) (auth.AcceptedMember, error) {
	member, err := service.Store.AcceptInvitation(ctx, hashInvitationToken(token), identity.User.ID, identity.User.Email, service.now())
	if err != nil {
		return auth.AcceptedMember{}, auth.ErrInvalidInvitation
	}
	return acceptedMember(member), nil
}

func (service Service) AcceptRegistration(ctx context.Context, token string, user auth.User) (auth.AcceptedMember, error) {
	member, err := service.Store.AcceptInvitation(ctx, hashInvitationToken(token), user.ID, user.Email, service.now())
	if err != nil {
		return auth.AcceptedMember{}, auth.ErrInvalidInvitation
	}
	return acceptedMember(member), nil
}

// AcceptRegistrationInTransaction lets Auth atomically create an invited user
// and consume the invitation in one database transaction.
func (service Service) AcceptRegistrationInTransaction(ctx context.Context, tx transaction.Tx, token string, user auth.User) (auth.AcceptedMember, error) {
	store, ok := service.Store.(transactionalInvitationStore)
	if !ok {
		return auth.AcceptedMember{}, auth.ErrInvalidInvitation
	}
	member, err := store.AcceptInvitationInTransaction(ctx, tx, hashInvitationToken(token), user.ID, user.Email, service.now())
	if err != nil {
		return auth.AcceptedMember{}, auth.ErrInvalidInvitation
	}
	return acceptedMember(member), nil
}

func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now().UTC()
	}
	return service.Clock.Now().UTC()
}

func authInvitation(invitation Invitation) auth.Invitation {
	return auth.Invitation{CreatedAt: invitation.CreatedAt, Email: invitation.Email, ExpiresAt: invitation.ExpiresAt, ID: invitation.ID, InvitedBy: invitation.InvitedBy, ProjectID: invitation.ProjectID, ProjectName: invitation.ProjectName, Role: string(invitation.Role), Status: invitation.Status}
}

func acceptedMember(member Member) auth.AcceptedMember {
	return auth.AcceptedMember{DisplayName: member.DisplayName, Email: member.Email, JoinedAt: member.JoinedAt, Role: string(member.Role), UserID: member.UserID}
}

func isHumanRole(role Role) bool {
	return role == RoleOwner ||
		role == RoleMaintainer ||
		role == RoleEditor ||
		role == RoleViewer
}

func isInvitableHumanRole(role Role) bool {
	return role == RoleMaintainer || role == RoleEditor || role == RoleViewer
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
	requestctx.SetProject(ctx, projectID)
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
