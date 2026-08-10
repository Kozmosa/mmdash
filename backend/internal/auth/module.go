package auth

import (
	"errors"
	"net/http"
	"strings"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
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
	mux.HandleFunc("/v1/auth/refresh", module.handleRefresh)
	mux.HandleFunc("/v1/auth/device/authorize", module.handleDeviceAuthorize)
	mux.HandleFunc("/v1/auth/device/verify", module.handleDeviceVerify)
	mux.HandleFunc("/v1/auth/device/token", module.handleDeviceToken)
	mux.HandleFunc("/v1/auth/register", module.handleRegister)
	mux.HandleFunc("/v1/auth/logout", module.handleLogout)
	mux.HandleFunc("/v1/auth/me", module.handleMe)
	mux.HandleFunc("/v1/auth/me/password", module.handlePassword)
	mux.HandleFunc("/v1/auth/invitations/preview", module.handleInvitationPreview)
	mux.HandleFunc("/v1/auth/invitations/accept", module.handleInvitationAccept)
	mux.HandleFunc("/v1/auth/invitations/reject", module.handleInvitationReject)
	mux.HandleFunc("/v1/auth/tokens", module.handleTokens)
	mux.HandleFunc("/v1/auth/tokens/", module.handleToken)
	mux.HandleFunc("/v1/auth/agent-tokens/", module.handleAgentTokenVerification)
}

func (module Module) handleRefresh(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body refreshTokenRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	result, err := module.Service.Refresh(request.Context(), body.RefreshToken)
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, result)
}

func (module Module) handleDeviceAuthorize(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	result, err := module.Service.StartDeviceAuthorization(request.Context())
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusCreated, result)
}

func (module Module) handleDeviceVerify(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	identity, err := module.Service.Authenticate(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	var body deviceVerificationRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	if err := module.Service.DecideDeviceAuthorization(request.Context(), identity, body.UserCode, body.Approve); err != nil {
		writeDomainError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (module Module) handleDeviceToken(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body deviceTokenRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	result, err := module.Service.ExchangeDeviceAuthorization(request.Context(), body.DeviceCode)
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, result)
}

func (module Module) handleRegister(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body registerRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	result, err := module.Service.Register(request.Context(), RegisterInput{DisplayName: body.DisplayName, Email: body.Email, InvitationToken: stringValue(body.InvitationToken), Password: body.Password})
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusCreated, result)
}

func (module Module) handleLogin(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.LoginRequest
	if !httpx.DecodeJSON(response, request, &body) {
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
	identity, err := module.Service.Authenticate(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	switch request.Method {
	case http.MethodGet:
		httpx.WriteJSON(response, http.StatusOK, identity)
	case http.MethodPatch:
		var body updateProfileRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		user, err := module.Service.UpdateProfile(request.Context(), identity, UpdateProfileInput{CurrentPassword: stringValue(body.CurrentPassword), DisplayName: body.DisplayName, Email: body.Email})
		if err != nil {
			writeDomainError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, user)
	default:
		response.Header().Set("Allow", "GET, PATCH")
		writeDomainError(response, request, apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed"))
	}
}

func (module Module) handlePassword(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	identity, err := module.Service.Authenticate(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	var body changePasswordRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	if err := module.Service.ChangePassword(request.Context(), identity, body.CurrentPassword, body.NewPassword); err != nil {
		writeDomainError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (module Module) handleInvitationPreview(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body invitationTokenRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	invitation, err := module.Service.PreviewInvitation(request.Context(), body.Token)
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, invitation)
}

func (module Module) handleInvitationAccept(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	identity, err := module.Service.Authenticate(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	var body invitationTokenRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	member, err := module.Service.AcceptInvitation(request.Context(), identity, body.Token)
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, member)
}

func (module Module) handleInvitationReject(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body invitationTokenRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	if err := module.Service.DeclineInvitation(request.Context(), body.Token); err != nil {
		writeDomainError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
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
		var body contract.CreateTokenRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeDomainError(response, request, ErrInvalid)
			return
		}
		issued, err := module.Service.IssueToken(
			request.Context(),
			identity,
			body.Kind,
			body.Name,
			stringValue(body.ProjectID),
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

func (module Module) handleAgentTokenVerification(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, "/v1/auth/agent-tokens/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "verification" {
		writeDomainError(response, request, ErrNotFound)
		return
	}
	identity, err := module.Service.Authenticate(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	var body contract.RecordAgentTokenVerificationRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	if err := body.Validate(); err != nil {
		writeDomainError(response, request, ErrInvalid)
		return
	}
	evidence, err := module.Service.RecordAgentTokenVerification(
		request.Context(), identity, parts[0], RecordAgentTokenVerificationInput{
			AgentInstanceID: body.AgentInstanceID,
			MCPMethod:       body.McpMethod,
			MCPSessionID:    body.McpSessionID,
			ProjectID:       body.ProjectID,
			RequestID:       body.RequestID,
		},
	)
	if err != nil {
		writeDomainError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, evidence)
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
	case errors.Is(err, ErrConflict):
		httpx.WriteError(response, request, apperror.New(http.StatusConflict, "AUTH_CONFLICT", "The account already exists or conflicts with another account"))
	case errors.Is(err, ErrRegistrationClosed):
		httpx.WriteError(response, request, apperror.New(http.StatusForbidden, "REGISTRATION_CLOSED", "Open registration is disabled"))
	case errors.Is(err, ErrInvalidInvitation):
		httpx.WriteError(response, request, apperror.New(http.StatusBadRequest, "INVALID_INVITATION", "The invitation is invalid, expired, revoked, or already used"))
	case errors.Is(err, ErrAuthorizationPending):
		httpx.WriteError(response, request, apperror.New(http.StatusBadRequest, "AUTHORIZATION_PENDING", "Device authorization is still pending"))
	case errors.Is(err, ErrAuthorizationDenied):
		httpx.WriteError(response, request, apperror.New(http.StatusUnauthorized, "AUTHORIZATION_DENIED", "Device authorization was denied"))
	case errors.Is(err, ErrAuthorizationExpired):
		httpx.WriteError(response, request, apperror.New(http.StatusBadRequest, "AUTHORIZATION_EXPIRED", "Device authorization expired"))
	default:
		httpx.WriteError(response, request, err)
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

type registerRequest struct {
	Email           string  `json:"email"`
	DisplayName     string  `json:"display_name"`
	Password        string  `json:"password"`
	InvitationToken *string `json:"invitation_token,omitempty"`
}
type updateProfileRequest struct {
	DisplayName     *string `json:"display_name,omitempty"`
	Email           *string `json:"email,omitempty"`
	CurrentPassword *string `json:"current_password,omitempty"`
}
type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}
type invitationTokenRequest struct {
	Token string `json:"token"`
}
type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}
type deviceVerificationRequest struct {
	Approve  bool   `json:"approve"`
	UserCode string `json:"user_code"`
}
type deviceTokenRequest struct {
	DeviceCode string `json:"device_code"`
}
