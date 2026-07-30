package coreapp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
	"github.com/mmdash/mmdash/backend/internal/platform/health"
	"github.com/mmdash/mmdash/backend/internal/platform/identity"
	"github.com/mmdash/mmdash/backend/internal/platform/logging"
	"github.com/mmdash/mmdash/backend/internal/platform/metrics"
	"github.com/mmdash/mmdash/backend/internal/platform/module"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
)

type readyChecker string

type observedModule struct{}

func (observedModule) Name() string { return "observed" }

func (observedModule) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/observed", func(response http.ResponseWriter, request *http.Request) {
		requestctx.SetActor(request.Context(), "verified-user", "session")
		requestctx.SetProject(request.Context(), "verified-project")
		response.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/v1/artifact-transfers/", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	})
}

func (readyChecker) Check(context.Context) error {
	return nil
}

func TestApplicationRecordsMetricsLogsAndAuditContext(t *testing.T) {
	var logOutput bytes.Buffer
	modules := module.NewRegistry()
	if err := modules.Register(observedModule{}); err != nil {
		t.Fatal(err)
	}
	registry := metrics.New("core", "test")
	var observation HTTPObservation
	handler := NewHandler(Options{
		Audit: func(_ context.Context, value HTTPObservation) error {
			observation = value
			return nil
		},
		Health: health.Handler{},
		IDGenerator: identity.Generator{
			Reader: bytes.NewReader(make([]byte, 32)),
		},
		Logger: logging.New(
			&logOutput,
			clock.Fixed{Time: time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)},
		),
		Metrics: registry,
		Modules: modules,
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPost, "/v1/observed", nil),
	)
	if observation.Path != "/v1/observed" ||
		observation.Status != http.StatusCreated {
		t.Fatalf("unexpected audit observation: %#v", observation)
	}
	if !strings.Contains(logOutput.String(), `"user_id":"verified-user"`) ||
		!strings.Contains(logOutput.String(), `"project_id":"verified-project"`) {
		t.Fatalf("trusted context missing from log: %s", logOutput.String())
	}
	metricsResponse := httptest.NewRecorder()
	handler.ServeHTTP(
		metricsResponse,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)
	if !strings.Contains(
		metricsResponse.Body.String(),
		`mmdash_http_requests_total{method="POST",status="201"} 1`,
	) {
		t.Fatalf("request metric missing: %s", metricsResponse.Body.String())
	}
}

func TestApplicationRedactsArtifactTransferTokensFromLogsAndAudit(t *testing.T) {
	var logOutput bytes.Buffer
	modules := module.NewRegistry()
	if err := modules.Register(observedModule{}); err != nil {
		t.Fatal(err)
	}
	var observation HTTPObservation
	handler := NewHandler(Options{
		Audit: func(_ context.Context, value HTTPObservation) error {
			observation = value
			return nil
		},
		Health: health.Handler{},
		IDGenerator: identity.Generator{
			Reader: bytes.NewReader(make([]byte, 32)),
		},
		Logger: logging.New(
			&logOutput,
			clock.Fixed{Time: time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)},
		),
		Metrics: metrics.New("core", "test"),
		Modules: modules,
	})

	const secretToken = "signed-transfer-token"
	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPut, "/v1/artifact-transfers/"+secretToken, nil),
	)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected transfer response, got %d", response.Code)
	}
	if strings.Contains(logOutput.String(), secretToken) {
		t.Fatalf("transfer token leaked to log: %s", logOutput.String())
	}
	if observation.Path != "/v1/artifact-transfers/{redacted}" {
		t.Fatalf("unexpected redacted audit path: %#v", observation)
	}
}

func TestApplicationAuditFinishesAfterRequestCancellation(t *testing.T) {
	modules := module.NewRegistry()
	if err := modules.Register(observedModule{}); err != nil {
		t.Fatal(err)
	}
	var auditValues requestctx.Values
	handler := NewHandler(Options{
		Audit: func(ctx context.Context, _ HTTPObservation) error {
			if err := ctx.Err(); err != nil {
				t.Fatalf("audit context inherited request cancellation: %v", err)
			}
			auditValues = requestctx.TrustedSnapshot(ctx)
			return nil
		},
		Health: health.Handler{},
		IDGenerator: identity.Generator{
			Reader: bytes.NewReader(make([]byte, 32)),
		},
		Logger: logging.New(
			io.Discard,
			clock.Fixed{Time: time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)},
		),
		Metrics: metrics.New("core", "test"),
		Modules: modules,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/v1/observed", nil).WithContext(ctx)
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if auditValues.ActorID != "verified-user" ||
		auditValues.ProjectID != "verified-project" ||
		auditValues.RequestID == "" {
		t.Fatalf("detached audit lost trusted request values: %#v", auditValues)
	}
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
