package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type checkerStub struct {
	err  error
	name string
}

func (checker checkerStub) Check(context.Context) error {
	return checker.err
}

func (checker checkerStub) Name() string {
	return checker.name
}

func TestReadinessReportsDependencyState(t *testing.T) {
	handler := Handler{Checkers: []Checker{
		checkerStub{name: "postgres"},
		checkerStub{name: "object_storage", err: errors.New("offline")},
	}}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected HTTP 503, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"object_storage":"unavailable"`) {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}

func TestLivenessReportsInjectedServiceVersion(t *testing.T) {
	handler := Handler{Version: "0.1.0-test"}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/health/live", nil),
	)
	if !strings.Contains(response.Body.String(), `"version":"0.1.0-test"`) {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}
