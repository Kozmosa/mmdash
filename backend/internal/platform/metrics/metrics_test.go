package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRegistryExposesBoundedHTTPMetricsAndVersion(t *testing.T) {
	registry := New("core", "0.1.0-test")
	registry.ObserveHTTP("get", http.StatusOK, 1500*time.Millisecond)
	registry.IncrementAuditFailure()
	mux := http.NewServeMux()
	registry.RegisterRoutes(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)
	body := response.Body.String()
	for _, expected := range []string{
		`mmdash_build_info{service="core",version="0.1.0-test"} 1`,
		`mmdash_http_requests_total{method="GET",status="200"} 1`,
		`mmdash_http_request_duration_seconds_sum{method="GET",status="200"} 1.500000`,
		`mmdash_audit_write_failures_total 1`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, body)
		}
	}
}
