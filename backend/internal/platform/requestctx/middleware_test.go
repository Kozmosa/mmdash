package requestctx

import (
	"bytes"
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
