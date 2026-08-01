package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientRetriesOnlySafeRequests(t *testing.T) {
	t.Run("identity read retries a transient failure", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			requests++
			if requests == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"kind":"session","user":{"id":"user-1"}}`))
		}))
		defer server.Close()

		if _, err := NewClient(server.URL).WhoAmI(context.Background(), "token"); err != nil {
			t.Fatal(err)
		}
		if requests != 2 {
			t.Fatalf("expected one bounded retry, got %d requests", requests)
		}
	})

	t.Run("device authorization is never blindly replayed", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			requests++
			response.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		if _, err := NewClient(server.URL).StartDeviceAuthorization(context.Background()); err == nil {
			t.Fatal("expected device authorization failure")
		}
		if requests != 1 {
			t.Fatalf("device authorization was replayed %d times", requests)
		}
	})
}
