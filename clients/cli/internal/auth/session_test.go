package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mmdash/mmdash/clients/cli/internal/api"
	"github.com/mmdash/mmdash/clients/cli/internal/credentials"
)

func TestAccessTokenRefreshesAndRotatesStoredSecrets(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/auth/refresh" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["refresh_token"] != "old-refresh" {
			t.Fatalf("unexpected refresh token")
		}
		_ = json.NewEncoder(response).Encode(api.LoginResult{AccessToken: "new-access", RefreshToken: "new-refresh", SessionID: "session-1", ExpiresAt: now.Add(time.Hour)})
	}))
	defer server.Close()
	store := credentials.NewMemoryStore()
	session := New(api.NewClient(server.URL), store, server.URL)
	session.Now = func() time.Time { return now }
	if err := store.Set(session.Profile, credentials.Credential{AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	token, err := session.AccessToken(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if token != "new-access" {
		t.Fatalf("unexpected token %q", token)
	}
	saved, err := store.Get(session.Profile)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RefreshToken != "new-refresh" {
		t.Fatalf("refresh token was not rotated")
	}
}
