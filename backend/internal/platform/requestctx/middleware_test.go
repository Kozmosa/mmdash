package requestctx

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/platform/identity"
)

func TestMiddlewarePreservesValidRequestContext(t *testing.T) {
	handler := Middleware(
		identity.Generator{Reader: bytes.NewReader(make([]byte, 16))},
		http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if RequestID(request.Context()) != "request-test" {
				t.Fatalf("unexpected request ID: %s", RequestID(request.Context()))
			}
			if ProjectID(request.Context()) != "project-1" {
				t.Fatalf("unexpected project ID: %s", ProjectID(request.Context()))
			}
			response.WriteHeader(http.StatusNoContent)
		}),
	)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "request-test")
	request.Header.Set("X-Mmdash-Project-ID", "project-1")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Header().Get("X-Request-ID") != "request-test" {
		t.Fatalf("missing response request ID")
	}
}

func TestMiddlewareReplacesInvalidRequestID(t *testing.T) {
	handler := Middleware(
		identity.Generator{Reader: bytes.NewReader(make([]byte, 16))},
		http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.WriteHeader(http.StatusNoContent)
		}),
	)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "bad")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Header().Get("X-Request-ID") != "00000000-0000-4000-8000-000000000000" {
		t.Fatalf("unexpected generated request ID: %s", response.Header().Get("X-Request-ID"))
	}
}

func TestAuthenticatedContextReplacesForwardedActor(t *testing.T) {
	handler := Middleware(
		identity.Generator{Reader: bytes.NewReader(make([]byte, 16))},
		http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			SetActor(request.Context(), "verified-user", "session")
			SetProject(request.Context(), "verified-project")
			values := Snapshot(request.Context())
			if values.ActorID != "verified-user" ||
				values.ActorKind != "session" ||
				values.ProjectID != "verified-project" {
				t.Fatalf("unexpected trusted context: %#v", values)
			}
			response.WriteHeader(http.StatusNoContent)
		}),
	)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Mmdash-User-ID", "forwarded-user")
	request.Header.Set("X-Mmdash-Project-ID", "forwarded-project")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
}

func TestForwardedIdentityIsExcludedUntilVerified(t *testing.T) {
	ctx := WithValues(context.Background(), Values{
		ActorID: "forwarded-user", ProjectID: "forwarded-project",
		RequestID: "request-1",
	})
	if values := TrustedSnapshot(ctx); values.ActorID != "" || values.ProjectID != "" {
		t.Fatalf("unverified identity leaked into audit context: %#v", values)
	}
	SetActor(ctx, "verified-user", "session")
	SetProject(ctx, "verified-project")
	if values := TrustedSnapshot(ctx); values.ActorID != "verified-user" ||
		values.ProjectID != "verified-project" {
		t.Fatalf("verified identity missing: %#v", values)
	}
}
