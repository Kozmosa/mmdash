package example

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type checkerStub struct {
	checkedAt time.Time
	err       error
}

func (stub checkerStub) Check(context.Context) (time.Time, error) {
	return stub.checkedAt, stub.err
}

func TestExampleRouteReturnsPostgresStatus(t *testing.T) {
	checkedAt := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	module := New(checkerStub{checkedAt: checkedAt})
	mux := http.NewServeMux()
	module.RegisterRoutes(mux)
	request := httptest.NewRequest(http.MethodGet, "/v1/example", nil)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"storage":"postgres"`) {
		t.Fatalf("unexpected response body: %s", response.Body.String())
	}
}

func TestExampleRouteMapsStorageFailure(t *testing.T) {
	module := New(checkerStub{err: errors.New("offline")})
	mux := http.NewServeMux()
	module.RegisterRoutes(mux)
	request := httptest.NewRequest(http.MethodGet, "/v1/example", nil)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected HTTP 503, got %d", response.Code)
	}
}
