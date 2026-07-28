package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
)

// Module exposes the Auth HTTP contract.
type Module struct {
	Service *Service
}

func (Module) Name() string { return "auth" }

func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/auth/login", module.handleLogin)
	mux.HandleFunc("/v1/auth/logout", module.handleLogout)
	mux.HandleFunc("/v1/auth/me", module.handleMe)
	mux.HandleFunc("/v1/auth/tokens", module.handleTokens)
	mux.HandleFunc("/v1/auth/tokens/", module.handleToken)
}

func (module Module) handleLogin(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeBody(response, request, &body) {
		return
	}
	result, err := module.Service.Login(request.Context(), body.Email, body.Password)
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, result)
}

func (module Module) handleLogout(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	if err := module.Service.Logout(request.Context(), request.Header.Get("Authorization")); err != nil {
		writeDomainError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (module Module) handleMe(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	identity, err := module.Service.Authenticate(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, identity)
}

func (module Module) handleTokens(response http.ResponseWriter, request *http.Request) {
	identity, err := module.Service.Authenticate(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	switch request.Method {
	case http.MethodGet:
		tokens, err := module.Service.ListTokens(request.Context(), identity)
		if err != nil {
			writeDomainError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": tokens})
	case http.MethodPost:
		var body struct {
			ExpiresAt *time.Time `json:"expires_at"`
			Kind      string     `json:"kind"`
			Name      string     `json:"name"`
			ProjectID string     `json:"project_id"`
		}
		if !decodeBody(response, request, &body) {
			return
		}
		issued, err := module.Service.IssueToken(
			request.Context(),
			identity,
			body.Kind,
			body.Name,
			body.ProjectID,
			body.ExpiresAt,
		)
		if err != nil {
			writeDomainError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusCreated, issued)
	default:
		response.Header().Set("Allow", "GET, POST")
		writeDomainError(response, request, apperror.New(
			http.StatusMethodNotAllowed,
			"METHOD_NOT_ALLOWED",
			"Method not allowed",
		))
	}
}

func (module Module) handleToken(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodDelete) {
		return
	}
	identity, err := module.Service.Authenticate(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	tokenID := strings.TrimPrefix(request.URL.Path, "/v1/auth/tokens/")
	if tokenID == "" || strings.Contains(tokenID, "/") {
		writeDomainError(response, request, ErrNotFound)
		return
	}
	if err := module.Service.RevokeToken(request.Context(), identity, tokenID); err != nil {
		writeDomainError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func decodeBody(response http.ResponseWriter, request *http.Request, target interface{}) bool {
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

func writeDomainError(response http.ResponseWriter, request *http.Request, err error) {
	var applicationError *apperror.Error
	if errors.As(err, &applicationError) {
		httpx.WriteError(response, request, applicationError)
		return
	}
	switch {
	case errors.Is(err, ErrInvalidCredentials), errors.Is(err, ErrUnauthenticated):
		httpx.WriteError(response, request, apperror.New(
			http.StatusUnauthorized,
			"UNAUTHENTICATED",
			"Authentication is required",
		))
	case errors.Is(err, ErrForbidden):
		httpx.WriteError(response, request, apperror.New(
			http.StatusForbidden,
			"FORBIDDEN",
			"Permission denied",
		))
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"Authentication input is invalid",
		))
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(response, request, apperror.New(
			http.StatusNotFound,
			"NOT_FOUND",
			"Resource not found",
		))
	default:
		httpx.WriteError(response, request, err)
	}
}
