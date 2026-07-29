package repo

import (
	"errors"
	"net/http"
	"strings"

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

func (module Module) RegisterRoutes(_ *http.ServeMux) {}

// ProjectHandler is mounted by Project because net/http ServeMux cannot register
// two independent handlers for the same /v1/projects/ subtree on Go 1.17.
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
	if len(segments) != 3 {
		writeRepoError(response, request, ErrNotConfigured)
		return
	}
	switch segments[2] {
	case "test":
		module.handleTest(response, request, identity, projectID)
	case "sync":
		module.handleSync(response, request, identity, projectID)
	case "workspaces":
		module.handleWorkspaces(response, request, identity, projectID)
	default:
		writeRepoError(response, request, ErrNotConfigured)
	}
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
			request.Context(), identity, projectID, body.SettingsVersion,
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
	case errors.Is(err, ErrAlreadyConnected):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "REPOSITORY_ALREADY_CONNECTED", "Repository is already connected",
		))
	case errors.Is(err, ErrConflict):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "REPO_SETTINGS_CHANGED", "Repository settings changed; test them again",
		))
	case errors.Is(err, ErrLocked):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "REPO_SYNC_IN_PROGRESS", "Repository synchronization is in progress",
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
