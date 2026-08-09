package project

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
)

// Module exposes project collaboration routes.
type Module struct {
	Artifact     http.Handler
	Model        http.Handler
	Notification http.Handler
	Progress     http.Handler
	Repository   http.Handler
	Service      Service
}

func (Module) Name() string { return "project" }

func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/projects", module.handleCollection)
	mux.HandleFunc("/v1/projects/trash", module.handleTrash)
	mux.HandleFunc("/v1/projects/invitations/", module.handleInvitationAcceptResource)
	mux.HandleFunc("/v1/projects/", module.handleResource)
}

func (module Module) handleInvitationAcceptResource(response http.ResponseWriter, request *http.Request) {
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	segments := strings.Split(strings.Trim(strings.TrimPrefix(request.URL.Path, "/v1/projects/invitations/"), "/"), "/")
	if len(segments) != 2 || segments[1] != "accept" || segments[0] == "" {
		writeProjectError(response, request, ErrNotFound)
		return
	}
	module.handleInvitationAccept(response, request, identity, segments[0])
}

func (module Module) handleCollection(response http.ResponseWriter, request *http.Request) {
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodGet:
		includeArchived := request.URL.Query().Get("include_archived") == "true"
		projects, err := module.Service.List(request.Context(), identity, includeArchived)
		if err != nil {
			writeProjectError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": projects})
	case http.MethodPost:
		var body contract.CreateProjectRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		project, err := module.Service.Create(request.Context(), identity, CreateInput{
			Name:               body.Name,
			ProblemSummary:     stringFrom(body.ProblemSummary),
			ProblemTitle:       stringFrom(body.ProblemTitle),
			ProjectConstraints: sliceFrom(body.ProjectConstraints),
			SourceArtifactIDs:  sliceFrom(body.SourceArtifactIDs),
		})
		if err != nil {
			writeProjectError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusCreated, project)
	default:
		writeProjectError(response, request, apperror.New(
			http.StatusMethodNotAllowed,
			"METHOD_NOT_ALLOWED",
			"Method not allowed",
		))
	}
}

func (module Module) handleResource(response http.ResponseWriter, request *http.Request) {
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	segments := strings.Split(strings.Trim(strings.TrimPrefix(request.URL.Path, "/v1/projects/"), "/"), "/")
	if len(segments) == 0 || segments[0] == "" {
		writeProjectError(response, request, ErrNotFound)
		return
	}
	projectID := segments[0]
	if len(segments) == 1 {
		module.handleProject(response, request, identity, projectID)
		return
	}
	switch segments[1] {
	case "artifacts":
		if module.Artifact != nil {
			module.Artifact.ServeHTTP(response, request)
			return
		}
	case "models":
		if module.Model != nil {
			module.Model.ServeHTTP(response, request)
			return
		}
	case "repository":
		if module.Repository != nil {
			module.Repository.ServeHTTP(response, request)
			return
		}
	case "progress":
		if module.Progress != nil {
			module.Progress.ServeHTTP(response, request)
			return
		}
	case "notification-channels", "notification-rules", "notification-deliveries":
		if module.Notification != nil {
			module.Notification.ServeHTTP(response, request)
			return
		}
	case "restore":
		if len(segments) == 2 {
			module.handleRestore(response, request, identity, projectID)
			return
		}
	case "members":
		if len(segments) == 2 {
			module.handleMembers(response, request, identity, projectID)
			return
		}
		if len(segments) == 3 {
			module.handleMember(response, request, identity, projectID, segments[2])
			return
		}
	case "invitations":
		if len(segments) == 2 {
			module.handleInvitations(response, request, identity, projectID)
			return
		}
		if len(segments) == 3 {
			module.handleInvitation(response, request, identity, projectID, segments[2])
			return
		}
	case "permissions":
		if len(segments) == 2 {
			module.handlePermissions(response, request, identity, projectID)
			return
		}
	}
	writeProjectError(response, request, ErrNotFound)
}

func (module Module) handleInvitationAccept(response http.ResponseWriter, request *http.Request, identity auth.Identity, invitationID string) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	member, err := module.Service.AcceptInvitationByID(request.Context(), identity, invitationID)
	if err != nil {
		writeProjectError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, member)
}

func (module Module) handleProject(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	switch request.Method {
	case http.MethodGet:
		project, err := module.Service.Get(request.Context(), identity, projectID)
		if err != nil {
			writeProjectError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, project)
	case http.MethodPatch:
		var body contract.UpdateProjectRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		project, err := module.Service.Update(request.Context(), identity, projectID, UpdateInput{
			Archived:           body.Archived,
			Name:               body.Name,
			ProblemSummary:     body.ProblemSummary,
			ProblemTitle:       body.ProblemTitle,
			ProjectConstraints: body.ProjectConstraints,
			SourceArtifactIDs:  body.SourceArtifactIDs,
		})
		if err != nil {
			writeProjectError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, project)
	case http.MethodDelete:
		if _, err := module.Service.Trash(request.Context(), identity, projectID); err != nil {
			writeProjectError(response, request, err)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	default:
		writeProjectError(response, request, apperror.New(
			http.StatusMethodNotAllowed,
			"METHOD_NOT_ALLOWED",
			"Method not allowed",
		))
	}
}

func (module Module) handleTrash(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	projects, err := module.Service.ListTrash(request.Context(), identity)
	if err != nil {
		writeProjectError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": projects})
}

func (module Module) handleRestore(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	project, err := module.Service.Restore(request.Context(), identity, projectID)
	if err != nil {
		writeProjectError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, project)
}

func (module Module) handleMembers(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	members, err := module.Service.ListMembers(request.Context(), identity, projectID)
	if err != nil {
		writeProjectError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": members})
}

func (module Module) handleMember(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	userID string,
) {
	switch request.Method {
	case http.MethodPut:
		var body contract.UpdateProjectMemberRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		member, err := module.Service.UpsertMember(
			request.Context(),
			identity,
			projectID,
			userID,
			Role(body.Role),
		)
		if err != nil {
			writeProjectError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, member)
	case http.MethodDelete:
		if err := module.Service.RemoveMember(
			request.Context(),
			identity,
			projectID,
			userID,
		); err != nil {
			writeProjectError(response, request, err)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	default:
		writeProjectError(response, request, apperror.New(
			http.StatusMethodNotAllowed,
			"METHOD_NOT_ALLOWED",
			"Method not allowed",
		))
	}
}

func (module Module) handleInvitations(response http.ResponseWriter, request *http.Request, identity auth.Identity, projectID string) {
	switch request.Method {
	case http.MethodGet:
		items, err := module.Service.ListInvitations(request.Context(), identity, projectID)
		if err != nil {
			writeProjectError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": items})
	case http.MethodPost:
		var body createInvitationRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		issued, err := module.Service.CreateInvitation(request.Context(), identity, projectID, body.Email, Role(body.Role))
		if err != nil {
			writeProjectError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusCreated, issued)
	default:
		writeProjectError(response, request, apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed"))
	}
}

func (module Module) handleInvitation(response http.ResponseWriter, request *http.Request, identity auth.Identity, projectID string, invitationID string) {
	if !httpx.RequireMethod(response, request, http.MethodDelete) {
		return
	}
	if err := module.Service.RevokeInvitation(request.Context(), identity, projectID, invitationID); err != nil {
		writeProjectError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (module Module) handlePermissions(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	role, permissions, err := module.Service.Permissions(request.Context(), identity, projectID)
	if err != nil {
		writeProjectError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{
		"permissions": permissions,
		"role":        role,
	})
}

func (module Module) identity(
	response http.ResponseWriter,
	request *http.Request,
) (auth.Identity, bool) {
	identity, err := module.Service.Authenticate(
		request.Context(),
		request.Header.Get("Authorization"),
	)
	if err != nil {
		writeProjectError(response, request, err)
		return auth.Identity{}, false
	}
	return identity, true
}

func writeProjectError(response http.ResponseWriter, request *http.Request, err error) {
	var applicationError *apperror.Error
	if errors.As(err, &applicationError) {
		httpx.WriteError(response, request, applicationError)
		return
	}
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		httpx.WriteError(response, request, apperror.New(
			http.StatusUnauthorized,
			"UNAUTHENTICATED",
			"Authentication is required",
		))
	case errors.Is(err, ErrForbidden):
		httpx.WriteError(response, request, apperror.New(
			http.StatusForbidden,
			"FORBIDDEN",
			"Project permission denied",
		))
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"Project input is invalid",
		))
	case errors.Is(err, ErrSelfInvitation):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest,
			"SELF_INVITATION_NOT_ALLOWED",
			"You cannot invite yourself to a project",
		))
	case errors.Is(err, ErrMemberExists):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict,
			"PROJECT_MEMBER_EXISTS",
			"This user is already a project member",
		))
	case errors.Is(err, ErrConflict):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict,
			"PROJECT_CONFLICT",
			"The project change would violate collaboration constraints",
		))
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(response, request, apperror.New(
			http.StatusNotFound,
			"PROJECT_NOT_FOUND",
			"Project not found",
		))
	case errors.Is(err, auth.ErrInvalidInvitation):
		httpx.WriteError(response, request, apperror.New(http.StatusBadRequest, "INVALID_INVITATION", "Invitation is invalid, expired, revoked, or does not match the current user"))
	default:
		httpx.WriteError(response, request, err)
	}
}

func stringFrom(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func sliceFrom(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}

type createInvitationRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}
