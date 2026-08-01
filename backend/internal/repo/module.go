package repo

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/repo/provider"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

// Module exposes Core Repo routes.
type Module struct {
	Service Service
}

func (Module) Name() string { return "repo" }

func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc(
		"/v1/repo/webhooks/github/",
		module.handleGitHubWebhook,
	)
}

// ProjectHandler is mounted by Project so the shared /v1/projects/ subtree
// keeps one explicit dispatcher and one module owner for each child route.
func (module Module) ProjectHandler() http.Handler {
	return http.HandlerFunc(module.handleProjectResource)
}

func (module Module) handleProjectResource(
	response http.ResponseWriter,
	request *http.Request,
) {
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	segments := strings.Split(
		strings.Trim(strings.TrimPrefix(request.URL.Path, "/v1/projects/"), "/"),
		"/",
	)
	if len(segments) < 2 || segments[0] == "" || segments[1] != "repository" {
		writeRepoError(response, request, ErrNotConfigured)
		return
	}
	projectID := segments[0]
	if len(segments) == 2 {
		module.handleRepository(response, request, identity, projectID)
		return
	}
	if len(segments) < 3 || len(segments) > 4 {
		writeRepoError(response, request, ErrNotConfigured)
		return
	}
	switch segments[2] {
	case "branches":
		if len(segments) == 3 {
			module.handleBranches(response, request, identity, projectID)
			return
		}
	case "commits":
		if len(segments) == 3 {
			module.handleCommits(response, request, identity, projectID)
			return
		}
		module.handleCommit(
			response, request, identity, projectID, segments[3],
		)
		return
	case "content":
		if len(segments) == 3 {
			module.handleContent(response, request, identity, projectID)
			return
		}
	case "checkouts":
		if len(segments) == 3 {
			module.handleCheckouts(response, request, identity, projectID)
			return
		}
		module.handleCheckout(
			response, request, identity, projectID, segments[3],
		)
		return
	case "test":
		if len(segments) == 3 {
			module.handleTest(response, request, identity, projectID)
			return
		}
	case "sync":
		if len(segments) == 3 {
			module.handleSync(response, request, identity, projectID)
			return
		}
	case "webhook-secret":
		if len(segments) == 3 {
			module.handleWebhookSecret(
				response, request, identity, projectID,
			)
			return
		}
	case "tree":
		if len(segments) == 3 {
			module.handleTree(response, request, identity, projectID)
			return
		}
	case "workspaces":
		if len(segments) == 3 {
			module.handleWorkspaces(response, request, identity, projectID)
			return
		}
	}
	writeRepoError(response, request, ErrNotConfigured)
}

func (module Module) handleGitHubWebhook(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	hookID := strings.Trim(
		strings.TrimPrefix(
			request.URL.Path, "/v1/repo/webhooks/github/",
		),
		"/",
	)
	if hookID == "" || strings.Contains(hookID, "/") {
		writeRepoError(response, request, ErrNotConfigured)
		return
	}
	body, err := io.ReadAll(io.LimitReader(
		request.Body, maximumWebhookBodyBytes+1,
	))
	if err != nil {
		writeRepoError(response, request, ErrInvalid)
		return
	}
	if int64(len(body)) > maximumWebhookBodyBytes {
		httpx.WriteError(response, request, apperror.New(
			http.StatusRequestEntityTooLarge,
			"REPO_WEBHOOK_TOO_LARGE",
			"Repository webhook payload is too large",
		))
		return
	}
	accepted, err := module.Service.AcceptGitHubWebhook(
		request.Context(), WebhookRequest{
			Body: body, DeliveryID: request.Header.Get("X-GitHub-Delivery"),
			Event: request.Header.Get("X-GitHub-Event"), HookID: hookID,
			Signature: request.Header.Get("X-Hub-Signature-256"),
		},
	)
	if err != nil {
		writeRepoError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusAccepted, accepted)
}

func (module Module) handleWebhookSecret(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	repository, err := module.Service.RotateWebhookSecret(
		request.Context(), identity, projectID,
	)
	if err != nil {
		writeRepoError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, repository)
}

func (module Module) handleRepository(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	switch request.Method {
	case http.MethodGet:
		repository, err := module.Service.Get(request.Context(), identity, projectID)
		if err != nil {
			writeRepoError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, repository)
	case http.MethodPut:
		var body contract.RepoConnectRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeRepoError(response, request, ErrInvalid)
			return
		}
		repository, err := module.Service.Connect(
			request.Context(), identity, projectID, ConnectRequest{
				ReplaceDisconnected: body.ReplaceDisconnected != nil &&
					*body.ReplaceDisconnected,
				SettingsVersion: body.SettingsVersion,
			},
		)
		if err != nil {
			writeRepoError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusAccepted, repository)
	case http.MethodDelete:
		if err := module.Service.Disconnect(
			request.Context(), identity, projectID,
		); err != nil {
			writeRepoError(response, request, err)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	default:
		httpx.WriteError(response, request, apperror.New(
			http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed",
		))
	}
}

func (module Module) handleTest(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	result, err := module.Service.TestConnection(
		request.Context(), identity, projectID,
	)
	if err != nil {
		writeRepoError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, result)
}

func (module Module) handleSync(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	repository, err := module.Service.RequestSync(
		request.Context(), identity, projectID,
	)
	if err != nil {
		writeRepoError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusAccepted, repository)
}

func (module Module) handleWorkspaces(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodPatch) {
		return
	}
	var body contract.RepoUpdateWorkspacesRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	if err := body.Validate(); err != nil {
		writeRepoError(response, request, ErrInvalid)
		return
	}
	repository, err := module.Service.UpdateMappings(
		request.Context(),
		identity,
		projectID,
		WorkspaceMappings{
			CodeBranch: body.CodeBranch, ArticleBranch: body.ArticleBranch,
			ResultBranch: body.ResultBranch,
		},
	)
	if err != nil {
		writeRepoError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusAccepted, repository)
}

func (module Module) handleBranches(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	branches, err := module.Service.ListBranches(
		request.Context(), identity, projectID,
	)
	if err != nil {
		writeRepoError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, branches)
}

func (module Module) handleCommits(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if request.Method == http.MethodPost {
		module.handleCreateCommit(response, request, identity, projectID)
		return
	}
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	workspace, ok := queryWorkspace(response, request)
	if !ok {
		return
	}
	limit, ok := queryLimit(response, request, maxCommitLimit)
	if !ok {
		return
	}
	page, err := module.Service.ListCommits(
		request.Context(), identity, projectID, workspace,
		request.URL.Query().Get("cursor"), limit,
	)
	if err != nil {
		writeRepoError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, page)
}

func (module Module) handleCreateCommit(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	var body contract.RepoCreateCommitRequest
	if !httpx.DecodeJSONLimit(
		response, request, &body, 2*1024*1024,
	) {
		return
	}
	changes := make([]FileChange, 0, len(body.Changes))
	for _, raw := range body.Changes {
		if len(raw) < 2 || len(raw) > 3 {
			writeRepoError(response, request, ErrInvalid)
			return
		}
		for key := range raw {
			if key != "operation" && key != "path" && key != "content_base64" {
				writeRepoError(response, request, ErrInvalid)
				return
			}
		}
		operation, operationOK := raw["operation"].(string)
		repositoryPath, pathOK := raw["path"].(string)
		if !operationOK || !pathOK {
			writeRepoError(response, request, ErrInvalid)
			return
		}
		change := FileChange{Operation: operation, Path: repositoryPath}
		encoded, hasContent := raw["content_base64"]
		switch operation {
		case "put":
			value, ok := encoded.(string)
			if !hasContent || !ok {
				writeRepoError(response, request, ErrInvalid)
				return
			}
			decoded, err := base64.StdEncoding.Strict().DecodeString(value)
			if err != nil {
				writeRepoError(response, request, ErrInvalid)
				return
			}
			change.Content = decoded
		case "delete":
			if hasContent {
				writeRepoError(response, request, ErrInvalid)
				return
			}
		default:
			writeRepoError(response, request, ErrInvalid)
			return
		}
		changes = append(changes, change)
	}
	result, err := module.Service.Commit(
		request.Context(), identity, WorkspaceCommitRequest{
			Changes: changes, ExpectedHeadSHA: body.ExpectedHeadSha,
			IdempotencyKey: body.IdempotencyKey, Message: body.Message,
			ProjectID: projectID, Workspace: WorkspaceKind(body.Workspace),
		},
	)
	if err != nil {
		writeRepoError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusCreated, result)
}

func (module Module) handleCheckouts(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.RepoCreateCheckoutRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	var ttl time.Duration
	if body.TtlSeconds != nil {
		ttl = time.Duration(*body.TtlSeconds) * time.Second
	}
	checkout, err := module.Service.CreateCheckout(
		request.Context(), identity, projectID,
		body.CommitSha, body.Purpose, ttl,
	)
	if err != nil {
		writeRepoError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusCreated, checkout)
}

func (module Module) handleCheckout(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	checkoutID string,
) {
	switch request.Method {
	case http.MethodGet:
		checkout, err := module.Service.GetCheckout(
			request.Context(), identity, projectID, checkoutID,
		)
		if err != nil {
			writeRepoError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, checkout)
	case http.MethodDelete:
		if err := module.Service.ReleaseCheckout(
			request.Context(), identity, projectID, checkoutID,
		); err != nil {
			writeRepoError(response, request, err)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	default:
		httpx.WriteError(response, request, apperror.New(
			http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed",
		))
	}
}

func (module Module) handleCommit(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	commitSHA string,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	commit, err := module.Service.GetCommit(
		request.Context(), identity, projectID, commitSHA,
	)
	if err != nil {
		writeRepoError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, commit)
}

func (module Module) handleTree(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	workspace, ok := queryWorkspace(response, request)
	if !ok {
		return
	}
	limit, ok := queryLimit(response, request, maxTreeLimit)
	if !ok {
		return
	}
	revision := request.URL.Query().Get("revision")
	if revision == "" {
		writeRepoError(response, request, ErrInvalid)
		return
	}
	page, err := module.Service.ListTree(
		request.Context(), identity, projectID, workspace, revision,
		request.URL.Query().Get("path"),
		request.URL.Query().Get("cursor"), limit,
	)
	if err != nil {
		writeRepoError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, page)
}

func (module Module) handleContent(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	workspace, ok := queryWorkspace(response, request)
	if !ok {
		return
	}
	revision := request.URL.Query().Get("revision")
	repositoryPath := request.URL.Query().Get("path")
	if revision == "" || repositoryPath == "" {
		writeRepoError(response, request, ErrInvalid)
		return
	}
	content, err := module.Service.ReadFile(
		request.Context(), identity, projectID, workspace,
		revision, repositoryPath,
	)
	if err != nil {
		writeRepoError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, content)
}

func queryWorkspace(
	response http.ResponseWriter,
	request *http.Request,
) (WorkspaceKind, bool) {
	workspace := WorkspaceKind(request.URL.Query().Get("workspace"))
	if workspace != WorkspaceCode &&
		workspace != WorkspaceArticle &&
		workspace != WorkspaceResult {
		writeRepoError(response, request, ErrInvalid)
		return "", false
	}
	return workspace, true
}

func queryLimit(
	response http.ResponseWriter,
	request *http.Request,
	maximum int,
) (int, bool) {
	raw := request.URL.Query().Get("limit")
	if raw == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maximum {
		writeRepoError(response, request, ErrInvalid)
		return 0, false
	}
	return limit, true
}

func (module Module) identity(
	response http.ResponseWriter,
	request *http.Request,
) (auth.Identity, bool) {
	identity, err := module.Service.Authenticate(
		request.Context(), request.Header.Get("Authorization"),
	)
	if err != nil {
		writeRepoError(response, request, err)
		return auth.Identity{}, false
	}
	return identity, true
}

func writeRepoError(response http.ResponseWriter, request *http.Request, err error) {
	var applicationError *apperror.Error
	if errors.As(err, &applicationError) {
		httpx.WriteError(response, request, applicationError)
		return
	}
	var safeError *SafeError
	if errors.As(err, &safeError) {
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest, safeError.Code, safeError.Message,
		))
		return
	}
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		httpx.WriteError(response, request, apperror.New(
			http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required",
		))
	case errors.Is(err, project.ErrForbidden), errors.Is(err, settings.ErrForbidden):
		httpx.WriteError(response, request, apperror.New(
			http.StatusForbidden, "FORBIDDEN", "Repository permission denied",
		))
	case errors.Is(err, ErrNotConfigured):
		httpx.WriteError(response, request, apperror.New(
			http.StatusNotFound, "REPOSITORY_NOT_CONFIGURED", "Repository is not configured",
		))
	case errors.Is(err, ErrObjectNotFound):
		httpx.WriteError(response, request, apperror.New(
			http.StatusNotFound, "REPO_OBJECT_NOT_FOUND", "Repository object was not found",
		))
	case errors.Is(err, ErrCheckoutNotFound):
		httpx.WriteError(response, request, apperror.New(
			http.StatusNotFound, "REPO_CHECKOUT_NOT_FOUND", "Repository checkout was not found",
		))
	case errors.Is(err, ErrAlreadyConnected):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "REPOSITORY_ALREADY_CONNECTED", "Repository is already connected",
		))
	case errors.Is(err, ErrReconnectMismatch):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict,
			"REPOSITORY_RECONNECT_REMOTE_MISMATCH",
			"Disconnected repository can only recover the same remote",
		))
	case errors.Is(err, ErrReconnectExpired):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict,
			"REPOSITORY_RECONNECT_EXPIRED",
			"Repository cleanup has started; wait for cleanup to finish",
		))
	case errors.Is(err, ErrReplacementCleanup):
		httpx.WriteError(response, request, apperror.New(
			http.StatusServiceUnavailable,
			"REPOSITORY_REPLACEMENT_CLEANUP_FAILED",
			"Disconnected repository cleanup failed; the old binding was preserved",
		))
	case errors.Is(err, ErrConflict):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "REPO_SETTINGS_CHANGED", "Repository settings changed; test them again",
		))
	case errors.Is(err, ErrLocked):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "REPO_SYNC_IN_PROGRESS", "Repository synchronization is in progress",
		))
	case errors.Is(err, ErrNotReady):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "REPOSITORY_NOT_READY", "Repository is not ready",
		))
	case errors.Is(err, ErrHeadChanged):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "REPO_HEAD_CHANGED", "Repository branch head changed",
		))
	case errors.Is(err, ErrNoChanges):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "REPO_NO_CHANGES", "Repository commit contains no changes",
		))
	case errors.Is(err, ErrWebhookSignature):
		httpx.WriteError(response, request, apperror.New(
			http.StatusUnauthorized,
			"REPO_WEBHOOK_SIGNATURE_INVALID",
			"Repository webhook signature is invalid",
		))
	case errors.Is(err, ErrWebhookConflict):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict,
			"REPO_WEBHOOK_DELIVERY_CONFLICT",
			"Repository webhook delivery conflicts with a previous payload",
		))
	case errors.Is(err, ErrBranchMapping), errors.Is(err, provider.ErrInvalidConfig):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest, "REPO_BRANCH_MAPPING_INVALID", "Repository branch mapping is invalid",
		))
	case errors.Is(err, provider.ErrAuthentication):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest, "REPO_AUTH_FAILED", "Repository authentication failed",
		))
	case errors.Is(err, provider.ErrRemoteNotFound):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest, "REPO_REMOTE_NOT_FOUND", "Repository was not found",
		))
	case errors.Is(err, provider.ErrBranchMissing):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest, "REPO_BRANCH_NOT_FOUND", "A mapped branch was not found",
		))
	case errors.Is(err, provider.ErrUnsupported):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest, "REPO_PROVIDER_UNSUPPORTED", "Repository provider is unsupported",
		))
	case errors.Is(err, settings.ErrNotFound), errors.Is(err, settings.ErrTypeNotFound):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest, "REPOSITORY_NOT_CONFIGURED", "Repository settings are incomplete",
		))
	case errors.Is(err, ErrInvalid), errors.Is(err, settings.ErrInvalid):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest, "INVALID_REQUEST", "Repository input is invalid",
		))
	default:
		httpx.WriteError(response, request, err)
	}
}
