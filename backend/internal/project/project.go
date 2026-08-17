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
	PermissionRead              Permission = "project.read"
	PermissionUpdate            Permission = "project.update"
	PermissionArchive           Permission = "project.archive"
	PermissionMembersManage     Permission = "project.members.manage"
	PermissionSettingsManage    Permission = "project.settings.manage"
	PermissionSettingsRead      Permission = "project.settings.read"
	PermissionTokensManage      Permission = "project.tokens.manage"
	PermissionJobsCreate        Permission = "project.jobs.create"
	PermissionJobsRead          Permission = "project.jobs.read"
	PermissionJobsCancel        Permission = "project.jobs.cancel"
	PermissionDataRead          Permission = "project.data.read"
	PermissionContextPropose    Permission = "project.context.propose"
	PermissionContextReview     Permission = "project.context.review"
	PermissionAuditRead         Permission = "project.audit.read"
	PermissionAuditWrite        Permission = "project.audit.write"
	PermissionRepoRead          Permission = "project.repo.read"
	PermissionRepoManage        Permission = "project.repo.manage"
	PermissionRepoWrite         Permission = "project.repo.write"
	PermissionArtifactRead      Permission = "project.artifact.read"
	PermissionArtifactUpload    Permission = "project.artifact.upload"
	PermissionArtifactDownload  Permission = "project.artifact.download"
	PermissionArtifactDelete    Permission = "project.artifact.delete"
	PermissionProgressRead      Permission = "project.progress.read"
	PermissionProgressManage    Permission = "project.progress.manage"
	PermissionProgressPropose   Permission = "project.progress.propose"
	PermissionProgressEvaluate  Permission = "project.progress.evaluate"
	PermissionModelRead         Permission = "project.model.read"
	PermissionModelSync         Permission = "project.model.sync"
	PermissionModelManage       Permission = "project.model.manage"
	PermissionAgentRead         Permission = "project.agent.read"
	PermissionAgentUse          Permission = "project.agent.use"
	PermissionAgentManage       Permission = "project.agent.manage"
	PermissionAgentTokensManage Permission = "project.agent.tokens.manage"
	PermissionExperimentRead    Permission = "project.experiment.read"
	PermissionExperimentManage  Permission = "project.experiment.manage"
	PermissionBoxRead           Permission = "project.box.read"
	PermissionBoxManage         Permission = "project.box.manage"
	PermissionArticleRead       Permission = "project.article.read"
	PermissionArticleEdit       Permission = "project.article.edit"
	PermissionArticlePropose    Permission = "project.article.propose"
	PermissionArticleBuild      Permission = "project.article.build"
	PermissionArticleRelease    Permission = "project.article.release"
	PermissionArticleTemplate   Permission = "project.article.template.manage"
	PermissionArticleZotero     Permission = "project.article.zotero.manage"
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
		PermissionRepoRead,
		PermissionRepoManage,
		PermissionRepoWrite,
		PermissionArtifactRead,
		PermissionArtifactUpload,
		PermissionArtifactDownload,
		PermissionArtifactDelete,
		PermissionProgressRead,
		PermissionProgressManage,
		PermissionProgressPropose,
		PermissionProgressEvaluate,
		PermissionModelRead,
		PermissionModelSync,
		PermissionModelManage,
		PermissionAgentRead,
		PermissionAgentUse,
		PermissionAgentManage,
		PermissionAgentTokensManage,
		PermissionExperimentRead,
		PermissionExperimentManage,
		PermissionBoxRead,
		PermissionBoxManage,
		PermissionArticleRead,
		PermissionArticleEdit,
		PermissionArticlePropose,
		PermissionArticleBuild,
		PermissionArticleRelease,
		PermissionArticleTemplate,
		PermissionArticleZotero,
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
		PermissionRepoRead,
		PermissionRepoManage,
		PermissionRepoWrite,
		PermissionArtifactRead,
		PermissionArtifactUpload,
		PermissionArtifactDownload,
		PermissionArtifactDelete,
		PermissionProgressRead,
		PermissionProgressManage,
		PermissionProgressPropose,
		PermissionProgressEvaluate,
		PermissionModelRead,
		PermissionModelSync,
		PermissionModelManage,
		PermissionAgentRead,
		PermissionAgentUse,
		PermissionAgentManage,
		PermissionAgentTokensManage,
		PermissionExperimentRead,
		PermissionExperimentManage,
		PermissionBoxRead,
		PermissionBoxManage,
		PermissionArticleRead,
		PermissionArticleEdit,
		PermissionArticlePropose,
		PermissionArticleBuild,
		PermissionArticleRelease,
		PermissionArticleTemplate,
		PermissionArticleZotero,
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
		PermissionRepoRead,
		PermissionRepoWrite,
		PermissionArtifactRead,
		PermissionArtifactUpload,
		PermissionArtifactDownload,
		PermissionProgressRead,
		PermissionProgressManage,
		PermissionProgressPropose,
		PermissionProgressEvaluate,
		PermissionModelRead,
		PermissionModelSync,
		PermissionModelManage,
		PermissionAgentRead,
		PermissionAgentUse,
		PermissionExperimentRead,
		PermissionExperimentManage,
		PermissionBoxRead,
		PermissionArticleRead,
		PermissionArticleEdit,
		PermissionArticlePropose,
		PermissionArticleBuild,
	},
	RoleViewer: {
		PermissionRead,
		PermissionSettingsRead,
		PermissionJobsRead,
		PermissionDataRead,
		PermissionAuditWrite,
		PermissionRepoRead,
		PermissionArtifactRead,
		PermissionArtifactDownload,
		PermissionProgressRead,
		PermissionProgressEvaluate,
		PermissionModelRead,
		PermissionModelSync,
		PermissionAgentRead,
		PermissionExperimentRead,
		PermissionBoxRead,
		PermissionArticleRead,
	},
	RoleAgent: {
		PermissionRead,
		PermissionDataRead,
		PermissionModelRead,
		PermissionContextPropose,
		PermissionRepoRead,
		PermissionArtifactRead,
		PermissionArtifactUpload,
		PermissionArtifactDownload,
		PermissionProgressRead,
		PermissionProgressEvaluate,
		PermissionExperimentRead,
		PermissionExperimentManage,
		PermissionBoxRead,
		PermissionArticleRead,
		PermissionArticlePropose,
	},
	RoleBox: {
		PermissionRead,
		PermissionSettingsRead,
		PermissionJobsRead,
		PermissionDataRead,
		PermissionAuditWrite,
		PermissionRepoRead,
		PermissionProgressRead,
		PermissionModelRead,
	},
}

// Project is the authoritative project record.
type Project struct {
	ArchivedAt         *time.Time `json:"archived_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	CreatedBy          string     `json:"created_by"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty"`
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	ProblemSummary     string     `json:"problem_summary"`
	ProblemTitle       string     `json:"problem_title"`
	ProjectConstraints []string   `json:"project_constraints"`
	PurgeAt            *time.Time `json:"purge_at,omitempty"`
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
	ListTrash(context.Context, string, time.Time) ([]Project, error)
	ListMembers(context.Context, string) ([]Member, error)
	ListInvitations(context.Context, string, time.Time) ([]Invitation, error)
	PreviewInvitation(context.Context, string, time.Time) (Invitation, error)
	PurgeExpired(context.Context, time.Time) error
	RemoveMember(context.Context, string, string, string) error
	Restore(context.Context, string, string, time.Time) (Project, error)
	RevokeInvitation(context.Context, string, string, string, time.Time) error
	Trash(context.Context, string, string, time.Time, time.Time) (Project, error)
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

// ArtifactReferenceValidator is the narrow boundary used to validate Project
// source references without reading Artifact persistence directly.
type ArtifactReferenceValidator interface {
	ValidateProjectReferences(context.Context, string, []string) error
}

// Service applies collaboration and RBAC policy.
type Service struct {
	AgentGrants    AgentGrantResolver
	Auth           Authenticator
	Artifacts      ArtifactReferenceValidator
	Clock          interface{ Now() time.Time }
	MemberRemoval  MemberRemovalHook
	Store          Store
	InvitationTTL  time.Duration
	TrashRetention time.Duration
}

// MemberRemovalHook lets an owning module close Project-scoped resources
// before membership is removed. Box Control uses it to force-unassign every
// account-owned Box of the departing member without revoking the global Box.
type MemberRemovalHook interface {
	BeforeProjectMemberRemoval(context.Context, string, string) error
}

// AgentGrantResolver is implemented by the Agent module without importing it
// into Project. It prevents product Agent tokens from inheriting the issuing
// user's membership and privileges.
type AgentGrantResolver interface {
	ResolveAgentRole(context.Context, string, string) (Role, error)
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
	// Stage 2 uses an explicit two-step flow: create the Project, upload its
	// source files inside that Project, then PATCH source_artifact_ids.
	if len(input.SourceArtifactIDs) != 0 {
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
	if err := service.Store.PurgeExpired(ctx, service.now()); err != nil {
		return nil, err
	}
	return service.Store.List(ctx, identity.User.ID, includeArchived)
}

// ListTrash returns recoverable projects owned by the caller.
func (service Service) ListTrash(
	ctx context.Context,
	identity auth.Identity,
) ([]Project, error) {
	now := service.now()
	if err := service.Store.PurgeExpired(ctx, now); err != nil {
		return nil, err
	}
	return service.Store.ListTrash(ctx, identity.User.ID, now)
}

// Get returns one project after permission checks.
func (service Service) Get(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (Project, error) {
	if err := auth.RequireAgentTool(identity, "project.get"); err != nil {
		return Project{}, ErrForbidden
	}
	if err := service.Authorize(ctx, identity, projectID, PermissionRead); err != nil {
		return Project{}, err
	}
	if identity.Kind == "agent" {
		store, ok := service.Store.(interface {
			GetForAgent(context.Context, string) (Project, error)
		})
		if !ok {
			return Project{}, ErrForbidden
		}
		return store.GetForAgent(ctx, projectID)
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
	if input.Archived != nil {
		if *input.Archived {
			return service.Trash(ctx, identity, projectID)
		}
		restored, err := service.Restore(ctx, identity, projectID)
		if err == nil {
			return restored, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Project{}, err
		}
		input.Archived = nil
	}
	if err := service.Authorize(ctx, identity, projectID, PermissionUpdate); err != nil {
		return Project{}, err
	}
	if input.Name != nil {
		trimmed := strings.TrimSpace(*input.Name)
		if trimmed == "" {
			return Project{}, ErrInvalid
		}
		input.Name = &trimmed
	}
	if input.SourceArtifactIDs != nil {
		if service.Artifacts == nil {
			if len(*input.SourceArtifactIDs) != 0 {
				return Project{}, ErrInvalid
			}
		} else if err := service.Artifacts.ValidateProjectReferences(
			ctx, projectID, *input.SourceArtifactIDs,
		); err != nil {
			return Project{}, ErrInvalid
		}
	}
	return service.Store.Update(ctx, identity.User.ID, projectID, input)
}

// Trash moves an active project into the owner's recoverable recycle bin.
func (service Service) Trash(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (Project, error) {
	if err := service.Authorize(ctx, identity, projectID, PermissionArchive); err != nil {
		return Project{}, err
	}
	now := service.now()
	retention := service.TrashRetention
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	return service.Store.Trash(
		ctx,
		identity.User.ID,
		projectID,
		now,
		now.Add(retention),
	)
}

// Restore recovers a trashed project while its retention window is active.
func (service Service) Restore(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (Project, error) {
	return service.Store.Restore(ctx, identity.User.ID, projectID, service.now())
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
	if identity.User.ID == "" {
		return ErrForbidden
	}
	selfRemoval := identity.User.ID == userID
	if !selfRemoval {
		if err := service.Authorize(ctx, identity, projectID, PermissionMembersManage); err != nil {
			return err
		}
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
	if service.MemberRemoval != nil {
		if err := service.MemberRemoval.BeforeProjectMemberRemoval(ctx, projectID, userID); err != nil {
			return err
		}
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
	if strings.EqualFold(email, strings.TrimSpace(identity.User.Email)) {
		return IssuedInvitation{}, ErrSelfInvitation
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

// AcceptInvitationByID is the authenticated Inbox action. It deliberately
// accepts an invitation ID, not a token; the Project store rechecks the
// invitation email, lifecycle, and one-time semantics in the same transaction.
func (service Service) AcceptInvitationByID(ctx context.Context, identity auth.Identity, invitationID string) (auth.AcceptedMember, error) {
	if identity.Kind == "agent" || identity.Kind == "box" {
		return auth.AcceptedMember{}, auth.ErrInvalidInvitation
	}
	store, ok := service.Store.(interface {
		AcceptInvitationByID(context.Context, string, string, string, time.Time) (Member, error)
	})
	if !ok || strings.TrimSpace(invitationID) == "" {
		return auth.AcceptedMember{}, auth.ErrInvalidInvitation
	}
	member, err := store.AcceptInvitationByID(ctx, invitationID, identity.User.ID, identity.User.Email, service.now())
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
	if identity.Kind == "agent" {
		if identity.CredentialStatus != "active" ||
			identity.AgentInstanceID == "" || identity.ProjectID != projectID ||
			service.AgentGrants == nil {
			return "", nil, ErrForbidden
		}
		role, err := service.AgentGrants.ResolveAgentRole(
			ctx, identity.AgentInstanceID, projectID,
		)
		if err != nil || role != RoleAgent {
			return "", nil, ErrForbidden
		}
		return role, append([]Permission(nil), permissionsByRole[role]...), nil
	}
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
	if identity.Kind == "agent" && !agentToolAllowsPermission(identity, required) {
		return ErrForbidden
	}
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

func agentToolAllowsPermission(identity auth.Identity, permission Permission) bool {
	requiredTools := []string{}
	switch permission {
	case PermissionRead:
		requiredTools = []string{"project.get"}
	case PermissionDataRead:
		requiredTools = []string{"data.list", "data.read"}
	case PermissionModelRead:
		// Model objects are exposed through the Data Hub's data.read adapter;
		// there is no separate MCP model-read tool to grant.
		requiredTools = []string{"data.read"}
	case PermissionContextPropose:
		requiredTools = []string{"context.promote"}
	case PermissionArtifactUpload:
		requiredTools = []string{"artifact.upload"}
	case PermissionRepoRead, PermissionArtifactRead,
		PermissionArtifactDownload:
		requiredTools = []string{"data.read", "artifact.read"}
	case PermissionProgressRead:
		requiredTools = []string{"data.read", "progress.get"}
	case PermissionProgressEvaluate:
		requiredTools = []string{"progress.recalculate"}
	case PermissionArticleRead:
		// Article objects are exposed read-only through Data Hub data.read.
		requiredTools = []string{"data.read"}
	case PermissionArticlePropose:
		// A reviewed patch proposal is the sole Agent-side Article mutation.
		requiredTools = []string{"data.read"}
	default:
		return false
	}
	for _, allowed := range identity.AllowedTools {
		for _, required := range requiredTools {
			if allowed == required {
				return true
			}
		}
	}
	return false
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
	ErrConflict       = errors.New("project conflict")
	ErrForbidden      = errors.New("project permission denied")
	ErrInvalid        = errors.New("invalid project input")
	ErrMemberExists   = errors.New("project member already exists")
	ErrNotFound       = errors.New("project not found")
	ErrSelfInvitation = errors.New("users cannot invite themselves")
)

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
