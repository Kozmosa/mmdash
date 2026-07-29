package repo

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/repo/provider"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

// Access is implemented by Project without exposing Project persistence.
type Access interface {
	Authenticate(context.Context, string) (auth.Identity, error)
	Authorize(context.Context, auth.Identity, string, project.Permission) error
}

// SettingAccess is the trusted Settings service surface used by Repo.
type SettingAccess interface {
	Resolve(context.Context, settings.Scope, string, string) (settings.ResolvedSetting, error)
	Update(
		context.Context,
		auth.Identity,
		settings.Scope,
		string,
		string,
		map[string]interface{},
	) (settings.Setting, error)
}

// AuditRecorder accepts bounded Repo audit records.
type AuditRecorder interface {
	Record(context.Context, audit.Event) error
}

// Service applies Repo RBAC, settings, provider, and persistence policy.
type Service struct {
	Access          Access
	Audit           AuditRecorder
	Clock           interface{ Now() time.Time }
	DisconnectGrace time.Duration
	Providers       *provider.Registry
	PublicURL       string
	Reads           *Reader
	Settings        SettingAccess
	Store           Store
}

// ConnectionTestResult enriches shared checks with branches required by Repo UI.
type ConnectionTestResult struct {
	Branches      []string                   `json:"branches"`
	CheckedAt     time.Time                  `json:"checked_at"`
	Checks        []settings.ConnectionCheck `json:"checks"`
	DefaultBranch string                     `json:"default_branch"`
	Status        string                     `json:"status"`
}

func (service Service) Authenticate(
	ctx context.Context,
	authorization string,
) (auth.Identity, error) {
	return service.Access.Authenticate(ctx, authorization)
}

func (service Service) Get(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (Repository, error) {
	if err := service.Access.Authorize(
		ctx, identity, projectID, project.PermissionRepoRead,
	); err != nil {
		return Repository{}, err
	}
	repository, err := service.Store.GetByProject(ctx, projectID)
	if err != nil {
		return Repository{}, err
	}
	return service.decorate(ctx, repository, ""), nil
}

func (service Service) ListBranches(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (BranchList, error) {
	repository, err := service.readRepository(ctx, identity, projectID)
	if err != nil {
		return BranchList{}, err
	}
	return service.Reads.ListBranches(ctx, repository)
}

func (service Service) ListCommits(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	workspace WorkspaceKind,
	cursor string,
	limit int,
) (CommitPage, error) {
	repository, err := service.readRepository(ctx, identity, projectID)
	if err != nil {
		return CommitPage{}, err
	}
	return service.Reads.ListCommits(
		ctx, repository, workspace, cursor, limit,
	)
}

func (service Service) GetCommit(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	commitSHA string,
) (Commit, error) {
	repository, err := service.readRepository(ctx, identity, projectID)
	if err != nil {
		return Commit{}, err
	}
	return service.Reads.GetCommit(ctx, repository, commitSHA)
}

func (service Service) ListTree(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	workspace WorkspaceKind,
	revision string,
	path string,
	cursor string,
	limit int,
) (TreePage, error) {
	repository, err := service.readRepository(ctx, identity, projectID)
	if err != nil {
		return TreePage{}, err
	}
	return service.Reads.ListTree(
		ctx, repository, workspace, revision, path, cursor, limit,
	)
}

func (service Service) ReadFile(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	workspace WorkspaceKind,
	revision string,
	path string,
) (FileContent, error) {
	repository, err := service.readRepository(ctx, identity, projectID)
	if err != nil {
		return FileContent{}, err
	}
	return service.Reads.ReadFile(
		ctx, repository, workspace, revision, path,
	)
}

func (service Service) TestConnection(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (ConnectionTestResult, error) {
	if err := service.Access.Authorize(
		ctx, identity, projectID, project.PermissionRepoManage,
	); err != nil {
		return ConnectionTestResult{}, err
	}
	checkedAt := service.Clock.Now().UTC()
	resolved, err := service.Settings.Resolve(
		ctx, settings.ScopeProject, projectID, SettingType,
	)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	config, err := providerConfig(resolved)
	if err != nil {
		return ConnectionTestResult{}, err
	}
	connection, err := service.Providers.Test(ctx, config)
	checks := []settings.ConnectionCheck{}
	status := "failed"
	if err != nil {
		checks = append(checks, settings.ConnectionCheck{
			Message: safeProviderMessage(err), Name: "provider", Status: "failed",
		})
	} else {
		status = "passed"
		checks = append(checks,
			settings.ConnectionCheck{Name: "provider", Status: "passed"},
			settings.ConnectionCheck{Name: "authentication", Status: "passed"},
			settings.ConnectionCheck{
				Message: connection.DefaultBranch,
				Name:    "default_branch",
				Status:  "passed",
			},
			settings.ConnectionCheck{
				Message: strings.Join(connection.BranchNames(), ", "),
				Name:    "workspace_branches",
				Status:  "passed",
			},
		)
	}
	result := ConnectionTestResult{
		Branches: connection.BranchNames(), CheckedAt: checkedAt,
		Checks: checks, DefaultBranch: connection.DefaultBranch, Status: status,
	}
	service.record(ctx, "repo.connection.tested", projectID, "", status, safeProviderCode(err))
	return result, nil
}

func (service Service) Connect(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	settingsVersion int64,
) (Repository, error) {
	if err := service.Access.Authorize(
		ctx, identity, projectID, project.PermissionRepoManage,
	); err != nil {
		return Repository{}, err
	}
	resolved, err := service.Settings.Resolve(
		ctx, settings.ScopeProject, projectID, SettingType,
	)
	if err != nil {
		return Repository{}, err
	}
	if settingsVersion < 1 || resolved.Version != settingsVersion {
		return Repository{}, ErrConflict
	}
	config, err := providerConfig(resolved)
	if err != nil {
		return Repository{}, err
	}
	connection, err := service.Providers.Test(ctx, config)
	if err != nil {
		return Repository{}, err
	}
	plainSecret := ""
	if current, _ := resolved.Values["webhook_secret"].(string); connection.Provider == "github" && current == "" {
		plainSecret, err = newWebhookSecret()
		if err != nil {
			return Repository{}, err
		}
		if _, err := service.Settings.Update(
			ctx, identity, settings.ScopeProject, projectID, SettingType,
			map[string]interface{}{"webhook_secret": plainSecret},
		); err != nil {
			return Repository{}, err
		}
		resolved, err = service.Settings.Resolve(
			ctx, settings.ScopeProject, projectID, SettingType,
		)
		if err != nil {
			return Repository{}, err
		}
	}
	repository, err := service.Store.CreatePending(ctx, identity.User.ID, ConnectionSnapshot{
		CanonicalRemoteURL: connection.CanonicalRemoteURL,
		DefaultBranch:      connection.DefaultBranch,
		DisplayName:        connection.DisplayName,
		ProjectID:          projectID,
		Provider:           Provider(connection.Provider),
		SettingsVersion:    resolved.Version,
		Workspaces: WorkspaceMappings{
			CodeBranch: config.CodeBranch, ArticleBranch: config.ArticleBranch,
			ResultBranch: config.ResultBranch,
		},
	})
	if err != nil {
		return Repository{}, err
	}
	service.record(ctx, "repo.sync.requested", projectID, repository.ID, "success", "")
	return service.decorate(ctx, repository, plainSecret), nil
}

func (service Service) RequestSync(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (Repository, error) {
	if err := service.Access.Authorize(
		ctx, identity, projectID, project.PermissionRepoManage,
	); err != nil {
		return Repository{}, err
	}
	repository, err := service.Store.RequestSync(ctx, projectID, service.Clock.Now().UTC())
	if err == nil {
		service.record(ctx, "repo.sync.requested", projectID, repository.ID, "success", "")
	}
	return service.decorate(ctx, repository, ""), err
}

func (service Service) UpdateMappings(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	mappings WorkspaceMappings,
) (Repository, error) {
	if err := service.Access.Authorize(
		ctx, identity, projectID, project.PermissionRepoManage,
	); err != nil {
		return Repository{}, err
	}
	current, err := service.Store.GetByProject(ctx, projectID)
	if err != nil {
		return Repository{}, err
	}
	if current.SyncLockedBy != nil {
		return Repository{}, ErrLocked
	}
	resolved, err := service.Settings.Resolve(
		ctx, settings.ScopeProject, projectID, SettingType,
	)
	if err != nil {
		return Repository{}, err
	}
	config, err := providerConfig(resolved)
	if err != nil {
		return Repository{}, err
	}
	config.CodeBranch = strings.TrimSpace(mappings.CodeBranch)
	config.ArticleBranch = strings.TrimSpace(mappings.ArticleBranch)
	config.ResultBranch = strings.TrimSpace(mappings.ResultBranch)
	if _, err := service.Providers.Test(ctx, config); err != nil {
		return Repository{}, err
	}
	updatedSetting, err := service.Settings.Update(
		ctx, identity, settings.ScopeProject, projectID, SettingType,
		map[string]interface{}{
			"code_branch": config.CodeBranch, "article_branch": config.ArticleBranch,
			"result_branch": config.ResultBranch,
		},
	)
	if err != nil {
		return Repository{}, err
	}
	repository, err := service.Store.UpdateMappings(ctx, projectID, WorkspaceMappings{
		CodeBranch: config.CodeBranch, ArticleBranch: config.ArticleBranch,
		ResultBranch: config.ResultBranch,
	}, updatedSetting.Version, service.Clock.Now().UTC())
	if err == nil {
		service.record(
			ctx, "repo.workspace.mapping.updated", projectID,
			repository.ID, "success", "",
		)
	}
	return service.decorate(ctx, repository, ""), err
}

func (service Service) Disconnect(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) error {
	if err := service.Access.Authorize(
		ctx, identity, projectID, project.PermissionRepoManage,
	); err != nil {
		return err
	}
	repository, err := service.Store.GetByProject(ctx, projectID)
	if err != nil {
		return err
	}
	now := service.Clock.Now().UTC()
	grace := service.DisconnectGrace
	if grace <= 0 {
		grace = 24 * time.Hour
	}
	if err := service.Store.Disconnect(ctx, projectID, now.Add(grace), now); err != nil {
		return err
	}
	service.record(ctx, "repo.disconnected", projectID, repository.ID, "success", "")
	return nil
}

func (service Service) decorate(
	ctx context.Context,
	repository Repository,
	plainSecret string,
) Repository {
	if repository.ID == "" {
		return repository
	}
	if repository.Provider == ProviderGitHub {
		repository.Webhook.PublicURL = strings.TrimSuffix(service.PublicURL, "/") +
			"/api/webhooks/github/" + repository.Webhook.HookID
	}
	resolved, err := service.Settings.Resolve(
		ctx, settings.ScopeProject, repository.ProjectID, SettingType,
	)
	if err == nil {
		secret, _ := resolved.Values["webhook_secret"].(string)
		repository.Webhook.SecretConfigured =
			repository.Provider == ProviderGitHub && secret != ""
	}
	repository.Webhook.Secret = plainSecret
	return repository
}

func (service Service) readRepository(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
) (Repository, error) {
	if service.Reads == nil {
		return Repository{}, ErrNotReady
	}
	if err := service.Access.Authorize(
		ctx, identity, projectID, project.PermissionRepoRead,
	); err != nil {
		return Repository{}, err
	}
	return service.Store.GetByProject(ctx, projectID)
}

func (service Service) record(
	ctx context.Context,
	action string,
	projectID string,
	resourceID string,
	outcome string,
	errorCode string,
) {
	if service.Audit == nil {
		return
	}
	values := requestctx.TrustedSnapshot(ctx)
	_ = service.Audit.Record(ctx, audit.Event{
		Action: action, ActorID: values.ActorID, ActorKind: values.ActorKind,
		Category: "repo", ErrorCode: errorCode, Metadata: map[string]interface{}{},
		Outcome: outcome, ProjectID: projectID, ResourceID: resourceID,
		ResourceType: "repository", Source: "core",
	})
}

func newWebhookSecret() (string, error) {
	contents := make([]byte, 32)
	if _, err := rand.Read(contents); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(contents), nil
}

func safeProviderCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, provider.ErrAuthentication):
		return "REPO_AUTH_FAILED"
	case errors.Is(err, provider.ErrBranchMissing):
		return "REPO_BRANCH_NOT_FOUND"
	case errors.Is(err, provider.ErrRemoteNotFound):
		return "REPO_REMOTE_NOT_FOUND"
	case errors.Is(err, provider.ErrUnsupported):
		return "REPO_PROVIDER_UNSUPPORTED"
	default:
		return "REPO_CONNECTION_FAILED"
	}
}
