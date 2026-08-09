package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmdash/mmdash/clients/cli/internal/apperror"
)

type Client struct {
	BaseURL string
	HTTP    HTTPTransport
}

type HTTPTransport interface {
	Do(*http.Request) (*http.Response, error)
}

type User struct {
	CreatedAt   time.Time `json:"created_at"`
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email"`
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	SystemRole  string    `json:"system_role"`
}

type Identity struct {
	Kind      string `json:"kind"`
	ProjectID string `json:"project_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	User      User   `json:"user"`
}

type LoginResult struct {
	AccessToken  string    `json:"access_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	RefreshToken string    `json:"refresh_token"`
	SessionID    string    `json:"session_id"`
	User         User      `json:"user"`
}

type DeviceAuthorization struct {
	DeviceCode              string    `json:"device_code"`
	ExpiresAt               time.Time `json:"expires_at"`
	Interval                int       `json:"interval"`
	UserCode                string    `json:"user_code"`
	VerificationURI         string    `json:"verification_uri"`
	VerificationURIComplete string    `json:"verification_uri_complete"`
}

type Project struct {
	CreatedAt    time.Time `json:"created_at"`
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	ProblemTitle string    `json:"problem_title"`
	Role         string    `json:"role"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ProjectList struct {
	Items []Project `json:"items"`
}

type ModelQuestion struct {
	ID               string     `json:"question_id"`
	ProjectID        string     `json:"project_id"`
	Code             string     `json:"code"`
	Title            string     `json:"title"`
	NotionPageID     string     `json:"notion_page_id"`
	NotionPageURL    string     `json:"notion_page_url"`
	LatestSnapshotID string     `json:"latest_snapshot_id,omitempty"`
	SnapshotCount    int        `json:"snapshot_count"`
	SyncStatus       string     `json:"sync_status"`
	LastSyncedAt     *time.Time `json:"last_synced_at,omitempty"`
}

type ModelSource struct {
	ID                      string     `json:"source_id"`
	NotionRootTitle         string     `json:"notion_root_title"`
	NotionRootPageURL       string     `json:"notion_root_page_url"`
	AutoSyncEnabled         bool       `json:"auto_sync_enabled"`
	AutoSyncIntervalSeconds int        `json:"auto_sync_interval_seconds"`
	NextSyncAt              *time.Time `json:"next_sync_at,omitempty"`
	SyncStatus              string     `json:"sync_status"`
	DiscoveredPageCount     int        `json:"discovered_page_count"`
}

type ModelOverview struct {
	ProjectID  string          `json:"project_id"`
	Configured bool            `json:"configured"`
	Source     *ModelSource    `json:"source,omitempty"`
	Questions  []ModelQuestion `json:"questions"`
}

type ModelQuestionDetail struct {
	Question       ModelQuestion            `json:"question"`
	LatestSnapshot map[string]interface{}   `json:"latest_snapshot,omitempty"`
	Snapshots      []map[string]interface{} `json:"snapshots"`
}

type ModelSync struct {
	ID         string `json:"sync_id"`
	ProjectID  string `json:"project_id"`
	QuestionID string `json:"question_id,omitempty"`
	Scope      string `json:"scope"`
	Status     string `json:"status"`
	JobID      string `json:"job_id"`
}

type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Status    int    `json:"-"`
}

func (err *Error) Error() string { return err.Message }

func NewClient(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (client *Client) StartDeviceAuthorization(ctx context.Context) (DeviceAuthorization, error) {
	var result DeviceAuthorization
	err := client.do(ctx, http.MethodPost, "/v1/auth/device/authorize", nil, "", &result, false)
	return result, err
}

func (client *Client) ExchangeDeviceAuthorization(ctx context.Context, code string) (LoginResult, error) {
	var result LoginResult
	err := client.do(ctx, http.MethodPost, "/v1/auth/device/token", map[string]string{"device_code": code}, "", &result, false)
	return result, err
}

func (client *Client) Refresh(ctx context.Context, refreshToken string) (LoginResult, error) {
	var result LoginResult
	err := client.do(ctx, http.MethodPost, "/v1/auth/refresh", map[string]string{"refresh_token": refreshToken}, "", &result, false)
	return result, err
}

func (client *Client) Logout(ctx context.Context, token string) error {
	return client.do(ctx, http.MethodPost, "/v1/auth/logout", nil, token, nil, false)
}

func (client *Client) WhoAmI(ctx context.Context, token string) (Identity, error) {
	var result Identity
	err := client.do(ctx, http.MethodGet, "/v1/auth/me", nil, token, &result, true)
	return result, err
}

func (client *Client) ListProjects(ctx context.Context, token string) (ProjectList, error) {
	var result ProjectList
	err := client.do(ctx, http.MethodGet, "/v1/projects", nil, token, &result, true)
	return result, err
}

func (client *Client) GetProject(ctx context.Context, token string, projectID string) (Project, error) {
	var result Project
	path := "/v1/projects/" + url.PathEscape(projectID)
	err := client.do(ctx, http.MethodGet, path, nil, token, &result, true)
	return result, err
}

func (client *Client) GetModels(ctx context.Context, token, projectID string) (ModelOverview, error) {
	var result ModelOverview
	err := client.do(ctx, http.MethodGet, "/v1/projects/"+url.PathEscape(projectID)+"/models", nil, token, &result, true)
	return result, err
}

func (client *Client) GetModelQuestion(ctx context.Context, token, projectID, questionID string) (ModelQuestionDetail, error) {
	var result ModelQuestionDetail
	path := "/v1/projects/" + url.PathEscape(projectID) + "/models/questions/" + url.PathEscape(questionID)
	err := client.do(ctx, http.MethodGet, path, nil, token, &result, true)
	return result, err
}

func (client *Client) SyncModels(ctx context.Context, token, projectID, questionID string) (ModelSync, error) {
	var result ModelSync
	path := "/v1/projects/" + url.PathEscape(projectID) + "/models/source/sync"
	if questionID != "" {
		path = "/v1/projects/" + url.PathEscape(projectID) + "/models/questions/" + url.PathEscape(questionID) + "/sync"
	}
	err := client.do(ctx, http.MethodPost, path, nil, token, &result, false)
	return result, err
}

func (client *Client) do(ctx context.Context, method string, path string, body interface{}, token string, result interface{}, retry bool) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	attempts := 1
	if retry {
		attempts = 3
	}
	for attempt := 0; attempt < attempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, client.BaseURL+path, bytes.NewReader(encoded))
		if err != nil {
			return err
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("X-Request-Id", requestID())
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := client.HTTP.Do(request)
		if err != nil {
			if attempt+1 < attempts && !errors.Is(err, context.Canceled) {
				time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
				continue
			}
			return &apperror.Error{Code: "NETWORK_UNAVAILABLE", ExitCode: 4, Message: "Cannot reach mmdash", Retryable: true, Cause: err}
		}
		err = decodeResponse(response, result)
		if apiErr, ok := err.(*Error); ok && attempt+1 < attempts && apiErr.Status >= 500 {
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
			continue
		}
		return err
	}
	return nil
}

func decodeResponse(response *http.Response, result interface{}) error {
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var value Error
		_ = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&value)
		value.Status = response.StatusCode
		if value.Code == "" {
			value.Code = "HTTP_ERROR"
		}
		if value.Message == "" {
			value.Message = fmt.Sprintf("mmdash returned HTTP %d", response.StatusCode)
		}
		return &value
	}
	if result == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(result)
}

func requestID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("cli-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}
