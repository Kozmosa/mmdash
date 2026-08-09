package model

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

const (
	notionOAuthAuthorizationURL = "https://api.notion.com/v1/oauth/authorize"
	notionOAuthTokenURL         = "https://api.notion.com/v1/oauth/token"
	notionOAuthRevokeURL        = "https://api.notion.com/v1/oauth/revoke"
	notionOAuthStateTTL         = 10 * time.Minute
	maxNotionOAuthResponseBytes = 1024 * 1024
)

// NotionOAuthClient is the public integration OAuth adapter. Endpoint fields
// are injectable only for deterministic tests; production wiring uses Notion's
// fixed official API endpoints.
type NotionOAuthClient struct {
	AuthorizationEndpoint string
	ClientID              string
	ClientSecret          string
	HTTPClient            *http.Client
	RedirectURI           string
	RevokeEndpoint        string
	TokenEndpoint         string
}

func (client *NotionOAuthClient) Available() bool {
	return client != nil && strings.TrimSpace(client.ClientID) != "" &&
		strings.TrimSpace(client.ClientSecret) != "" && strings.TrimSpace(client.RedirectURI) != ""
}

func (client *NotionOAuthClient) AuthorizationURL(state string) (string, error) {
	if !client.Available() || strings.TrimSpace(state) == "" {
		return "", ErrOAuthUnavailable
	}
	endpoint := strings.TrimSpace(client.AuthorizationEndpoint)
	if endpoint == "" {
		endpoint = notionOAuthAuthorizationURL
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", ErrOAuthUnavailable
	}
	query := parsed.Query()
	query.Set("client_id", client.ClientID)
	query.Set("redirect_uri", client.RedirectURI)
	query.Set("response_type", "code")
	query.Set("owner", "user")
	query.Set("state", state)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (client *NotionOAuthClient) Exchange(ctx context.Context, code string) (NotionOAuthTokens, error) {
	return client.tokenRequest(ctx, map[string]string{
		"grant_type":   "authorization_code",
		"code":         strings.TrimSpace(code),
		"redirect_uri": client.RedirectURI,
	}, true)
}

func (client *NotionOAuthClient) Refresh(ctx context.Context, refreshToken string) (NotionOAuthTokens, error) {
	return client.tokenRequest(ctx, map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": strings.TrimSpace(refreshToken),
	}, false)
}

func (client *NotionOAuthClient) Revoke(ctx context.Context, token string) error {
	if !client.Available() || strings.TrimSpace(token) == "" {
		return ErrOAuthUnavailable
	}
	endpoint := strings.TrimSpace(client.RevokeEndpoint)
	if endpoint == "" {
		endpoint = notionOAuthRevokeURL
	}
	return client.request(ctx, endpoint, map[string]string{"token": strings.TrimSpace(token)}, nil)
}

func (client *NotionOAuthClient) tokenRequest(ctx context.Context, body map[string]string, requireWorkspace bool) (NotionOAuthTokens, error) {
	if !client.Available() {
		return NotionOAuthTokens{}, ErrOAuthUnavailable
	}
	for _, value := range body {
		if strings.TrimSpace(value) == "" {
			return NotionOAuthTokens{}, ErrInvalid
		}
	}
	endpoint := strings.TrimSpace(client.TokenEndpoint)
	if endpoint == "" {
		endpoint = notionOAuthTokenURL
	}
	var response struct {
		AccessToken   string `json:"access_token"`
		RefreshToken  string `json:"refresh_token"`
		BotID         string `json:"bot_id"`
		WorkspaceID   string `json:"workspace_id"`
		WorkspaceName string `json:"workspace_name"`
		WorkspaceIcon string `json:"workspace_icon"`
	}
	if err := client.request(ctx, endpoint, body, &response); err != nil {
		return NotionOAuthTokens{}, err
	}
	result := NotionOAuthTokens{
		AccessToken: strings.TrimSpace(response.AccessToken), RefreshToken: strings.TrimSpace(response.RefreshToken),
		BotID: strings.TrimSpace(response.BotID), WorkspaceID: strings.TrimSpace(response.WorkspaceID),
		WorkspaceName: strings.TrimSpace(response.WorkspaceName), WorkspaceIcon: strings.TrimSpace(response.WorkspaceIcon),
	}
	if result.AccessToken == "" || result.RefreshToken == "" || (requireWorkspace && (result.BotID == "" || result.WorkspaceID == "")) {
		return NotionOAuthTokens{}, ErrSyncUnavailable
	}
	return result, nil
}

func (client *NotionOAuthClient) request(ctx context.Context, endpoint string, body interface{}, target interface{}) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.SetBasicAuth(client.ClientID, client.ClientSecret)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request Notion OAuth: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxNotionOAuthResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read Notion OAuth response: %w", err)
	}
	if len(responseBody) > maxNotionOAuthResponseBytes {
		return ErrSyncUnavailable
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusBadRequest {
			return ErrNotionUnauthorized
		}
		return ErrSyncUnavailable
	}
	if target == nil || response.StatusCode == http.StatusNoContent || len(bytes.TrimSpace(responseBody)) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return ErrSyncUnavailable
	}
	return nil
}

func (service Service) GetNotionOAuthConnection(ctx context.Context, caller auth.Identity, projectID string) (NotionOAuthConnection, error) {
	if err := service.authorize(ctx, caller, projectID, false); err != nil {
		return NotionOAuthConnection{}, err
	}
	connection := NotionOAuthConnection{Available: service.OAuth != nil && service.OAuth.Available()}
	if service.Settings == nil {
		return connection, nil
	}
	resolved, err := service.Settings.Resolve(ctx, settings.ScopeProject, projectID, SettingTypeNotion)
	if errors.Is(err, settings.ErrNotFound) {
		return connection, nil
	}
	if err != nil {
		return NotionOAuthConnection{}, err
	}
	connection.Connected = strings.TrimSpace(settingString(resolved, "access_token")) != ""
	connection.BotID = settingString(resolved, "oauth_bot_id")
	connection.WorkspaceID = settingString(resolved, "oauth_workspace_id")
	connection.WorkspaceName = settingString(resolved, "oauth_workspace_name")
	connection.WorkspaceIcon = settingString(resolved, "oauth_workspace_icon")
	return connection, nil
}

func (service Service) StartNotionOAuth(ctx context.Context, caller auth.Identity, projectID string, input StartNotionOAuthInput) (NotionOAuthAuthorizationResult, error) {
	if err := service.authorize(ctx, caller, projectID, true); err != nil {
		return NotionOAuthAuthorizationResult{}, err
	}
	if service.OAuth == nil || !service.OAuth.Available() || service.Store == nil {
		return NotionOAuthAuthorizationResult{}, ErrOAuthUnavailable
	}
	pageID, normalizedURL, err := parseNotionPageURL(input.RootPageURL)
	if err != nil || input.Interval < minimumSyncInterval || input.Interval > maximumSyncInterval {
		return NotionOAuthAuthorizationResult{}, ErrInvalid
	}
	state, stateHash, err := newNotionOAuthState()
	if err != nil {
		return NotionOAuthAuthorizationResult{}, err
	}
	id, err := service.Generator.New()
	if err != nil {
		return NotionOAuthAuthorizationResult{}, err
	}
	now := service.now()
	authorization := NotionOAuthAuthorization{
		ID: id, StateHash: stateHash, ProjectID: projectID, UserID: caller.User.ID,
		RootPageID: pageID, RootPageURL: normalizedURL, AutoSyncEnabled: input.AutoSyncEnabled,
		AutoSyncIntervalSeconds: int(input.Interval / time.Second), Status: "pending",
		ExpiresAt: now.Add(notionOAuthStateTTL), CreatedAt: now, UpdatedAt: now,
	}
	authorizationURL, err := service.OAuth.AuthorizationURL(state)
	if err != nil {
		return NotionOAuthAuthorizationResult{}, err
	}
	if err := service.Store.CreateNotionOAuthAuthorization(ctx, authorization); err != nil {
		return NotionOAuthAuthorizationResult{}, err
	}
	service.record(ctx, caller, projectID, "model.notion.oauth.started", "success", authorization.ID, nil)
	return NotionOAuthAuthorizationResult{AuthorizationURL: authorizationURL, ExpiresAt: authorization.ExpiresAt}, nil
}

func (service Service) CompleteNotionOAuth(ctx context.Context, caller auth.Identity, input CompleteNotionOAuthInput) (NotionOAuthCallbackResult, error) {
	if service.OAuth == nil || !service.OAuth.Available() || service.OAuthSettings == nil || service.Store == nil || service.Notion == nil {
		return NotionOAuthCallbackResult{}, ErrOAuthUnavailable
	}
	stateHash := hashNotionOAuthState(input.State)
	if stateHash == "" || (strings.TrimSpace(input.Code) == "" && strings.TrimSpace(input.ProviderError) == "") {
		return NotionOAuthCallbackResult{}, ErrInvalid
	}
	now := service.now()
	authorization, err := service.Store.ClaimNotionOAuthAuthorization(ctx, stateHash, caller.User.ID, now)
	if err != nil {
		return NotionOAuthCallbackResult{}, err
	}
	if err := service.authorize(ctx, caller, authorization.ProjectID, true); err != nil {
		_ = service.Store.CompleteNotionOAuthAuthorization(ctx, authorization.ID, "failed", now)
		return NotionOAuthCallbackResult{}, err
	}
	if strings.TrimSpace(input.ProviderError) != "" {
		_ = service.Store.CompleteNotionOAuthAuthorization(ctx, authorization.ID, "denied", now)
		service.record(ctx, caller, authorization.ProjectID, "model.notion.oauth.denied", "success", authorization.ID, nil)
		return NotionOAuthCallbackResult{ProjectID: authorization.ProjectID, Status: "denied"}, nil
	}
	tokens, err := service.OAuth.Exchange(ctx, input.Code)
	if err != nil {
		_ = service.Store.CompleteNotionOAuthAuthorization(ctx, authorization.ID, "failed", now)
		service.record(ctx, caller, authorization.ProjectID, "model.notion.oauth.completed", "error", authorization.ID, nil)
		return NotionOAuthCallbackResult{}, err
	}
	if _, err := service.Notion.Check(ctx, tokens.AccessToken, authorization.RootPageID); err != nil {
		_ = service.OAuth.Revoke(ctx, tokens.AccessToken)
		_ = service.Store.CompleteNotionOAuthAuthorization(ctx, authorization.ID, "failed", now)
		return NotionOAuthCallbackResult{}, ErrNotConfigured
	}
	values := map[string]interface{}{
		"access_token": tokens.AccessToken, "refresh_token": tokens.RefreshToken, "integration_token": nil,
		"root_page_url": authorization.RootPageURL, "auto_sync_enabled": authorization.AutoSyncEnabled,
		"auto_sync_interval_seconds": float64(authorization.AutoSyncIntervalSeconds),
		"oauth_bot_id":               tokens.BotID, "oauth_workspace_id": tokens.WorkspaceID,
	}
	if tokens.WorkspaceName != "" {
		values["oauth_workspace_name"] = tokens.WorkspaceName
	} else {
		values["oauth_workspace_name"] = nil
	}
	if tokens.WorkspaceIcon != "" {
		values["oauth_workspace_icon"] = tokens.WorkspaceIcon
	} else {
		values["oauth_workspace_icon"] = nil
	}
	if _, err := service.OAuthSettings.Update(ctx, caller, settings.ScopeProject, authorization.ProjectID, SettingTypeNotion, values); err != nil {
		_ = service.OAuth.Revoke(ctx, tokens.AccessToken)
		_ = service.Store.CompleteNotionOAuthAuthorization(ctx, authorization.ID, "failed", now)
		return NotionOAuthCallbackResult{}, err
	}
	if err := service.Store.CompleteNotionOAuthAuthorization(ctx, authorization.ID, "succeeded", now); err != nil {
		return NotionOAuthCallbackResult{}, err
	}
	service.record(ctx, caller, authorization.ProjectID, "model.notion.oauth.completed", "success", authorization.ID, map[string]interface{}{"workspace_id": tokens.WorkspaceID})
	return NotionOAuthCallbackResult{ProjectID: authorization.ProjectID, Status: "connected"}, nil
}

func (service Service) DisconnectNotionOAuth(ctx context.Context, caller auth.Identity, projectID string) error {
	if err := service.authorize(ctx, caller, projectID, true); err != nil {
		return err
	}
	if service.Settings == nil || service.OAuthSettings == nil || service.Store == nil {
		return ErrOAuthUnavailable
	}
	resolved, err := service.Settings.Resolve(ctx, settings.ScopeProject, projectID, SettingTypeNotion)
	if err != nil {
		return err
	}
	accessToken := settingString(resolved, "access_token")
	revokeFailed := false
	if accessToken != "" && service.OAuth != nil && service.OAuth.Available() {
		revokeFailed = service.OAuth.Revoke(ctx, accessToken) != nil
	}
	err = service.OAuthSettings.Delete(ctx, caller, settings.ScopeProject, projectID, SettingTypeNotion)
	if err == nil {
		err = service.Store.DisableSource(ctx, projectID, caller.User.ID, service.now())
	}
	service.record(ctx, caller, projectID, "model.notion.oauth.disconnected", outcome(err), projectID, map[string]interface{}{"provider_revoke_failed": revokeFailed})
	return err
}

func newNotionOAuthState() (string, string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", "", err
	}
	state := base64.RawURLEncoding.EncodeToString(value)
	return state, hashNotionOAuthState(state), nil
}

func hashNotionOAuthState(state string) string {
	state = strings.TrimSpace(state)
	if state == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(state))
	return hex.EncodeToString(digest[:])
}
