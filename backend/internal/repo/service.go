package repo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/audit"
	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
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
	Checkouts       CheckoutStore
	CheckoutTTL     time.Duration
	Clock           interface{ Now() time.Time }
	Commits         CommitStore
	DisconnectGrace time.Duration
	Generator       identity.Generator
	MaxWriteBytes   int64
	Providers       *provider.Registry
	PublicURL       string
	Reads           *Reader
	Settings        SettingAccess
	Store           Store
	WriteLease      time.Duration
	Writer          *WorkspaceWriter
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

func (service Service) CreateCheckout(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	commitSHA string,
	purpose string,
	ttl time.Duration,
) (Checkout, error) {
	repository, err := service.readRepository(ctx, identity, projectID)
	if err != nil {
		return Checkout{}, err
	}
	if service.Checkouts == nil || service.Writer == nil {
		return Checkout{}, ErrNotReady
	}
	purpose = strings.TrimSpace(purpose)
	if purpose == "" || len(purpose) > 100 {
		return Checkout{}, ErrInvalid
	}
	if ttl <= 0 {
		ttl = service.CheckoutTTL
	}
	if ttl < time.Minute || ttl > 24*time.Hour {
		return Checkout{}, ErrInvalid
	}
	checkoutID, err := service.Generator.New()
	if err != nil {
		return Checkout{}, err
	}
	now := service.Clock.Now().UTC()
	relative, err := service.Writer.Runtime.CreateCheckout(
		ctx, repository, checkoutID, commitSHA,
	)
	if err != nil {
		return Checkout{}, err
	}
	checkout := Checkout{
		CheckoutID: checkoutID, CheckoutRelpath: relative,
		CommitSHA: commitSHA, CreatedAt: now, CreatedBy: actorID(identity),
		ExpiresAt: now.Add(ttl), Purpose: purpose,
		RepositoryID: repository.ID, Status: "active",
	}
	if err := service.Checkouts.CreateCheckout(ctx, checkout); err != nil {
		_ = service.Writer.Runtime.ReleaseCheckout(ctx, repository, relative)
		return Checkout{}, err
	}
	service.record(
		ctx, "repo.checkout.created", projectID, checkoutID, "success", "",
	)
	return checkout, nil
}

func (service Service) GetCheckout(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	checkoutID string,
) (Checkout, error) {
	if err := service.Access.Authorize(
		ctx, identity, projectID, project.PermissionRepoRead,
	); err != nil {
		return Checkout{}, err
	}
	if service.Checkouts == nil {
		return Checkout{}, ErrNotReady
	}
	return service.Checkouts.GetCheckout(ctx, projectID, checkoutID)
}

func (service Service) ReleaseCheckout(
	ctx context.Context,
	identity auth.Identity,
	projectID string,
	checkoutID string,
) error {
	if err := service.Access.Authorize(
		ctx, identity, projectID, project.PermissionRepoRead,
	); err != nil {
		return err
	}
	if service.Checkouts == nil || service.Writer == nil {
		return ErrNotReady
	}
	checkout, err := service.Checkouts.GetCheckout(ctx, projectID, checkoutID)
	if err != nil {
		return err
	}
	if checkout.Status == "active" {
		repository, err := service.Store.GetByProject(ctx, projectID)
		if err != nil {
			return err
		}
		if err := service.Writer.Runtime.ReleaseCheckout(
			ctx, repository, checkout.CheckoutRelpath,
		); err != nil {
			_ = service.Checkouts.MarkCheckoutError(ctx, checkout.CheckoutID)
			return err
		}
	}
	if _, err := service.Checkouts.ReleaseCheckout(
		ctx, projectID, checkoutID, service.Clock.Now().UTC(),
	); err != nil {
		return err
	}
	service.record(
		ctx, "repo.checkout.released", projectID, checkoutID, "success", "",
	)
	return nil
}

func (service Service) Commit(
	ctx context.Context,
	caller auth.Identity,
	request WorkspaceCommitRequest,
) (CommitResult, error) {
	if err := service.Access.Authorize(
		ctx, caller, request.ProjectID, project.PermissionRepoWrite,
	); err != nil {
		return CommitResult{}, err
	}
	request.ActorID = actorID(caller)
	request.ActorName = strings.TrimSpace(caller.User.DisplayName)
	if request.ActorName == "" {
		request.ActorName = "mmdash"
	}
	request.ActorEmail = strings.TrimSpace(caller.User.Email)
	if request.ActorEmail == "" {
		request.ActorEmail = request.ActorID + "@users.mmdash.local"
	}
	if err := service.validateCommitRequest(&request); err != nil {
		return CommitResult{}, err
	}
	return service.commitTrusted(ctx, request)
}

func (service Service) commitTrusted(
	ctx context.Context,
	request WorkspaceCommitRequest,
) (result CommitResult, err error) {
	if service.Commits == nil || service.Writer == nil {
		return CommitResult{}, ErrNotReady
	}
	ownerID, err := service.Generator.New()
	if err != nil {
		return CommitResult{}, err
	}
	lease := service.WriteLease
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	claim, err := service.Commits.BeginCommit(
		ctx, request, "write-"+ownerID, lease, service.Clock.Now().UTC(),
	)
	if err != nil {
		return CommitResult{}, err
	}
	result = CommitResult{
		Branch:            claim.Workspace.RemoteBranch,
		CommitSHA:         claim.PreparedCommitSHA,
		PreviousCommitSHA: request.ExpectedHeadSHA,
		RepositoryID:      claim.Repository.ID, Workspace: request.Workspace,
	}
	if claim.AlreadySucceeded {
		return result, nil
	}
	fail := func(operationErr error, action string) {
		code, _ := safeCommitFailure(operationErr)
		_ = service.Commits.FailCommit(
			ctx, claim, code, service.Clock.Now().UTC(),
		)
		service.record(
			ctx, action, request.ProjectID,
			claim.Repository.ID, "error", code,
		)
	}
	resolved, err := service.Settings.Resolve(
		ctx, settings.ScopeProject, request.ProjectID, SettingType,
	)
	if err != nil {
		fail(err, "repo.commit.failed")
		return CommitResult{}, err
	}
	config, err := providerConfig(resolved)
	if err != nil {
		fail(err, "repo.commit.failed")
		return CommitResult{}, err
	}
	connection, err := service.Providers.Test(ctx, config)
	if err != nil {
		fail(err, "repo.commit.failed")
		return CommitResult{}, err
	}
	prepared := claim.PreparedCommitSHA
	if prepared == "" {
		commit, prepareErr := service.Writer.Prepare(
			ctx, claim, connection, request,
		)
		if prepareErr != nil {
			fail(prepareErr, "repo.commit.failed")
			return CommitResult{}, prepareErr
		}
		prepared = commit.CommitSHA
		if err := service.Commits.SavePreparedCommit(
			ctx, claim, prepared, service.Clock.Now().UTC(),
		); err != nil {
			fail(err, "repo.commit.failed")
			return CommitResult{}, err
		}
		claim.PreparedCommitSHA = prepared
	}
	commit, err := service.Writer.PushPrepared(
		ctx, claim, connection, prepared,
	)
	if err != nil {
		fail(err, "repo.push.failed")
		return CommitResult{}, err
	}
	result.CommitSHA = commit.CommitSHA
	if err := service.Commits.CompleteCommit(
		ctx, claim, commit, result, service.Clock.Now().UTC(),
	); err != nil {
		fail(err, "repo.commit.failed")
		return CommitResult{}, err
	}
	service.record(
		ctx, "repo.commit.created", request.ProjectID,
		commit.CommitSHA, "success", "",
	)
	return result, nil
}

func (service Service) validateCommitRequest(
	request *WorkspaceCommitRequest,
) error {
	if request.ProjectID == "" ||
		(request.Workspace != WorkspaceCode &&
			request.Workspace != WorkspaceArticle &&
			request.Workspace != WorkspaceResult) ||
		gitcli.ValidateFullSHA(request.ExpectedHeadSHA) != nil ||
		strings.TrimSpace(request.Message) == "" ||
		len(request.Message) > 10000 ||
		strings.TrimSpace(request.IdempotencyKey) == "" ||
		len(request.IdempotencyKey) > 200 ||
		len(request.Changes) < 1 ||
		len(request.Changes) > 100 {
		return ErrInvalid
	}
	paths := map[string]bool{}
	var total int64
	for _, change := range request.Changes {
		if gitcli.ValidateRepoPath(change.Path, false) != nil ||
			paths[change.Path] ||
			(change.Operation != "put" && change.Operation != "delete") {
			return ErrInvalid
		}
		paths[change.Path] = true
		if change.Operation == "delete" && len(change.Content) != 0 {
			return ErrInvalid
		}
		total += int64(len(change.Content))
	}
	maxBytes := service.MaxWriteBytes
	if maxBytes <= 0 {
		maxBytes = 1024 * 1024
	}
	if total > maxBytes {
		return ErrInvalid
	}
	hashInput := struct {
		ActorID         string
		Changes         []FileChange
		ExpectedHeadSHA string
		IdempotencyKey  string
		Message         string
		ProjectID       string
		Workspace       WorkspaceKind
	}{
		ActorID: request.ActorID, Changes: request.Changes,
		ExpectedHeadSHA: request.ExpectedHeadSHA,
		IdempotencyKey:  request.IdempotencyKey, Message: request.Message,
		ProjectID: request.ProjectID, Workspace: request.Workspace,
	}
	contents, err := json.Marshal(hashInput)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(contents)
	request.RequestSHA256 = hex.EncodeToString(sum[:])
	return nil
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

func safeCommitFailure(err error) (string, string) {
	var safeError *SafeError
	switch {
	case errors.As(err, &safeError):
		return safeError.Code, safeError.Message
	case errors.Is(err, ErrHeadChanged):
		return "REPO_HEAD_CHANGED", "Repository branch head changed"
	case errors.Is(err, ErrNoChanges):
		return "REPO_NO_CHANGES", "Repository commit contains no changes"
	case errors.Is(err, ErrWorktreeDirty):
		return "REPO_WORKTREE_DIRTY", "Managed repository worktree contains changes"
	case errors.Is(err, ErrLocked):
		return "REPO_WRITE_IN_PROGRESS", "Repository write is in progress"
	case errors.Is(err, provider.ErrAuthentication):
		return "REPO_AUTH_FAILED", "Repository authentication failed"
	default:
		return "REPO_COMMIT_FAILED", "Repository commit failed"
	}
}

func actorID(identity auth.Identity) string {
	for _, candidate := range []string{
		identity.User.ID, identity.TokenID, identity.SessionID, identity.Kind,
	} {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return "system"
}
