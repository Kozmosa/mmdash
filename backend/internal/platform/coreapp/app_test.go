package coreapp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/health"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/logging"
	"github.com/mmdash/mmdash/backend/internal/platform/module"
)

type readyChecker string

func (readyChecker) Check(context.Context) error {
	return nil
}

func (checker readyChecker) Name() string {
	return string(checker)
}

func TestApplicationServesHealthAndOpenAPIWithRequestContext(t *testing.T) {
	var logOutput bytes.Buffer
	handler := NewHandler(Options{
		Health: health.Handler{Checkers: []health.Checker{readyChecker("postgres")}},
		IDGenerator: identity.Generator{
			Reader: bytes.NewReader(make([]byte, 32)),
		},
		Logger:  logging.New(&logOutput, clock.Fixed{Time: time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)}),
		Modules: module.NewRegistry(),
		OpenAPI: []byte("openapi: 3.1.0\n"),
	})

	healthResponse := httptest.NewRecorder()
	handler.ServeHTTP(healthResponse, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if healthResponse.Code != http.StatusOK {
		t.Fatalf("expected ready response, got %d", healthResponse.Code)
	}
	if healthResponse.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected request ID response header")
	}

	openAPIResponse := httptest.NewRecorder()
	handler.ServeHTTP(openAPIResponse, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	if !strings.Contains(openAPIResponse.Body.String(), "openapi: 3.1.0") {
		t.Fatalf("unexpected OpenAPI body: %s", openAPIResponse.Body.String())
	}
}
