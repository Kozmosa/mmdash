package project

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
)

// Module exposes project collaboration routes.
type Module struct {
	Service Service
}

func (Module) Name() string { return "project" }

func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/projects", module.handleCollection)
	mux.HandleFunc("/v1/projects/", module.handleResource)
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
		var body CreateInput
		if !decodeProjectBody(response, request, &body) {
			return
		}
		project, err := module.Service.Create(request.Context(), identity, body)
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
	case "members":
		if len(segments) == 2 {
			module.handleMembers(response, request, identity, projectID)
			return
		}
		if len(segments) == 3 {
			module.handleMember(response, request, identity, projectID, segments[2])
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
		var body UpdateInput
		if !decodeProjectBody(response, request, &body) {
			return
		}
		project, err := module.Service.Update(request.Context(), identity, projectID, body)
		if err != nil {
			writeProjectError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, project)
	default:
		writeProjectError(response, request, apperror.New(
			http.StatusMethodNotAllowed,
			"METHOD_NOT_ALLOWED",
			"Method not allowed",
		))
	}
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
		var body struct {
			Role Role `json:"role"`
		}
		if !decodeProjectBody(response, request, &body) {
			return
		}
		member, err := module.Service.UpsertMember(
			request.Context(),
			identity,
			projectID,
			userID,
			body.Role,
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

func decodeProjectBody(response http.ResponseWriter, request *http.Request, target interface{}) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"Request body is invalid",
		))
		return false
	}
	return true
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
	default:
		httpx.WriteError(response, request, err)
	}
}
