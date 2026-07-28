package settings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
)

// Authenticator resolves the trusted Core identity.
type Authenticator interface {
	Authenticate(context.Context, string) (auth.Identity, error)
}

// Module exposes the typed Settings HTTP contract.
type Module struct {
	Auth    Authenticator
	Service Service
}

func (Module) Name() string { return "settings" }

func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/settings/types", module.handleTypes)
	mux.HandleFunc("/v1/settings/system/", module.handleSystem)
	mux.HandleFunc("/v1/settings/projects/", module.handleProject)
}

func (module Module) handleTypes(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	scope := Scope(request.URL.Query().Get("scope"))
	scopeID := request.URL.Query().Get("project_id")
	if scope != ScopeSystem && scope != ScopeProject {
		writeSettingsError(response, request, ErrInvalid)
		return
	}
	if scope == ScopeProject && scopeID == "" {
		writeSettingsError(response, request, ErrInvalid)
		return
	}
	items, err := module.Service.ListTypes(
		request.Context(),
		identity,
		scope,
		scopeID,
	)
	if err != nil {
		writeSettingsError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": items})
}

func (module Module) handleSystem(response http.ResponseWriter, request *http.Request) {
	segments, ok := settingsSegments(request.URL.Path, "/v1/settings/system/")
	if !ok {
		writeSettingsError(response, request, ErrNotFound)
		return
	}
	module.handleSetting(response, request, ScopeSystem, "", segments)
}

func (module Module) handleProject(response http.ResponseWriter, request *http.Request) {
	segments, ok := settingsSegments(request.URL.Path, "/v1/settings/projects/")
	if !ok || len(segments) < 2 {
		writeSettingsError(response, request, ErrNotFound)
		return
	}
	module.handleSetting(response, request, ScopeProject, segments[0], segments[1:])
}

func (module Module) handleSetting(
	response http.ResponseWriter,
	request *http.Request,
	scope Scope,
	scopeID string,
	segments []string,
) {
	if len(segments) < 1 || len(segments) > 2 ||
		(len(segments) == 2 && segments[1] != "test") {
		writeSettingsError(response, request, ErrNotFound)
		return
	}
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	typeKey := segments[0]
	if len(segments) == 2 {
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		result, err := module.Service.TestConnection(
			request.Context(),
			identity,
			scope,
			scopeID,
			typeKey,
		)
		if err != nil {
			writeSettingsError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, result)
		return
	}
	switch request.Method {
	case http.MethodGet:
		setting, err := module.Service.Get(
			request.Context(),
			identity,
			scope,
			scopeID,
			typeKey,
		)
		if err != nil {
			writeSettingsError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, setting)
	case http.MethodPatch:
		var body struct {
			Values map[string]interface{} `json:"values"`
		}
		if !decodeSettingsBody(response, request, &body) {
			return
		}
		if body.Values == nil {
			writeSettingsError(response, request, ErrInvalid)
			return
		}
		setting, err := module.Service.Update(
			request.Context(),
			identity,
			scope,
			scopeID,
			typeKey,
			body.Values,
		)
		if err != nil {
			writeSettingsError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, setting)
	case http.MethodDelete:
		if err := module.Service.Delete(
			request.Context(),
			identity,
			scope,
			scopeID,
			typeKey,
		); err != nil {
			writeSettingsError(response, request, err)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	default:
		response.Header().Set("Allow", "GET, PATCH, DELETE")
		writeSettingsError(response, request, apperror.New(
			http.StatusMethodNotAllowed,
			"METHOD_NOT_ALLOWED",
			"Method not allowed",
		))
	}
}

func (module Module) identity(
	response http.ResponseWriter,
	request *http.Request,
) (auth.Identity, bool) {
	identity, err := module.Auth.Authenticate(
		request.Context(),
		request.Header.Get("Authorization"),
	)
	if err != nil {
		writeSettingsError(response, request, err)
		return auth.Identity{}, false
	}
	return identity, true
}

func settingsSegments(path string, prefix string) ([]string, bool) {
	raw := strings.Split(strings.Trim(strings.TrimPrefix(path, prefix), "/"), "/")
	if len(raw) == 0 || raw[0] == "" {
		return nil, false
	}
	segments := make([]string, len(raw))
	for index, segment := range raw {
		decoded, err := url.PathUnescape(segment)
		if err != nil || decoded == "" {
			return nil, false
		}
		segments[index] = decoded
	}
	return segments, true
}

func decodeSettingsBody(
	response http.ResponseWriter,
	request *http.Request,
	target interface{},
) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeSettingsError(response, request, ErrInvalid)
		return false
	}
	return true
}

func writeSettingsError(response http.ResponseWriter, request *http.Request, err error) {
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
			"Settings permission denied",
		))
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"Settings input is invalid",
		))
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrTypeNotFound):
		httpx.WriteError(response, request, apperror.New(
			http.StatusNotFound,
			"SETTING_NOT_FOUND",
			"Setting or configuration type not found",
		))
	default:
		httpx.WriteError(response, request, err)
	}
}
