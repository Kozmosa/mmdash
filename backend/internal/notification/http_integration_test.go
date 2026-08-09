package notification

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/mmdash/mmdash/backend/internal/platform/identity"
)

func TestCoreHTTPNotificationRuleRoundTrip(t *testing.T) {
	coreURL := strings.TrimRight(os.Getenv("MMDASH_TEST_CORE_URL"), "/")
	databaseURL := os.Getenv("MMDASH_TEST_DATABASE_URL")
	password := os.Getenv("MMDASH_TEST_ADMIN_PASSWORD")
	if coreURL == "" || databaseURL == "" || password == "" {
		t.Skip("Core HTTP integration settings are not configured")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := &http.Client{Timeout: 15 * time.Second}

	var login struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	status, err := notificationHTTPJSON(ctx, client, http.MethodPost, coreURL+"/v1/auth/login", "", map[string]string{
		"email": "admin@mmdash.local", "password": password,
	}, &login)
	if err != nil || status != http.StatusOK || login.AccessToken == "" || login.User.ID == "" {
		t.Fatalf("login: status=%d err=%v", status, err)
	}
	token := login.AccessToken

	generator := identity.Generator{}
	projectID := generator.MustNew()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects(project_id,name,created_by,created_at,updated_at)
		VALUES($1,'Notification HTTP Test',$2,$3,$3)
	`, projectID, login.User.ID, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_members(project_id,user_id,role,created_at,updated_at)
		VALUES($1,$2,'owner',$3,$3)
	`, projectID, login.User.ID, now); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM projects WHERE project_id=$1`, projectID)
		logoutRequest, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, coreURL+"/v1/auth/logout", nil)
		logoutRequest.Header.Set("Authorization", "Bearer "+token)
		if response, logoutErr := client.Do(logoutRequest); logoutErr == nil {
			_ = response.Body.Close()
		}
	})

	path := coreURL + "/v1/projects/" + projectID + "/notification-rules/" + TypeReminderDue
	channelPath := coreURL + "/v1/projects/" + projectID + "/notification-channels/notification.generic_webhook"
	status, err = notificationHTTPJSON(ctx, client, http.MethodPatch, channelPath, token, map[string]interface{}{
		"values": map[string]interface{}{
			"enabled":        true,
			"endpoint":       "https://example.test/notification",
			"signing_secret": "integration-secret",
		},
	}, nil)
	if err != nil || status != http.StatusOK {
		t.Fatalf("configure notification channel: status=%d err=%v", status, err)
	}
	body := map[string]interface{}{
		"inbox_enabled":    true,
		"external_enabled": true,
		"channel_keys":     []string{"notification.generic_webhook"},
		"minimum_priority": "high",
	}
	var updated Rule
	status, err = notificationHTTPJSON(ctx, client, http.MethodPut, path, token, body, &updated)
	if err != nil || status != http.StatusOK {
		t.Fatalf("put rule: status=%d err=%v", status, err)
	}
	if len(updated.ChannelKeys) != 1 || updated.ChannelKeys[0] != "notification.generic_webhook" {
		t.Fatalf("put rule channel keys: %#v", updated.ChannelKeys)
	}
	var loaded Rule
	status, err = notificationHTTPJSON(ctx, client, http.MethodGet, path, token, nil, &loaded)
	if err != nil || status != http.StatusOK {
		t.Fatalf("get rule: status=%d err=%v", status, err)
	}
	if loaded.Version != updated.Version || len(loaded.ChannelKeys) != 1 || loaded.ChannelKeys[0] != updated.ChannelKeys[0] {
		t.Fatalf("get rule mismatch: updated=%#v loaded=%#v", updated, loaded)
	}
}

func notificationHTTPJSON(ctx context.Context, client *http.Client, method, url, token string, input interface{}, output interface{}) (int, error) {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return 0, err
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return response.StatusCode, err
	}
	if output != nil && len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, output); err != nil {
			return response.StatusCode, err
		}
	}
	return response.StatusCode, nil
}
