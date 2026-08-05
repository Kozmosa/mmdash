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
	registry.ObserveRepoOperation("sync", "success", "local", 125*time.Millisecond)
	registry.ObserveRepoOperation("unbounded-value", "other", "repository-id", time.Millisecond)
	registry.ObserveArtifactOperation("confirm", "success", "minio", 250*time.Millisecond)
	registry.ObserveArtifactOperation(
		"artifact-id", "denied", "bucket-name", time.Millisecond,
	)
	registry.ObserveAgentOperation("hermes", "stream", "success")
	registry.ObserveAgentOperation("secret-runtime-url", "secret-operation", "secret-outcome")
	registry.ObserveAgentConnectionCheck("project_access", "passed")
	registry.ObserveAgentRun("waiting_for_approval")
	registry.AddAgentStream(1)
	registry.ObserveAgentToken("rotate", "error")
	registry.SetRepoGauges(3, 2, 4096)
	registry.ObserveNotificationCreated()
	registry.ObserveNotificationDelivery("retrying", 300*time.Millisecond)
	registry.ObserveNotificationDelivery("failed", 200*time.Millisecond)
	registry.SetNotificationGauges(4, 5)
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
		`mmdash_repo_operations_total{operation="sync",outcome="success",provider="local"} 1`,
		`mmdash_repo_operations_total{operation="other",outcome="error",provider="unknown"} 1`,
		`mmdash_repo_operation_duration_seconds{operation="sync",provider="local"} 0.125000`,
		`mmdash_repo_sync_queue_depth 3`,
		`mmdash_repo_checkouts_active 2`,
		`mmdash_repo_storage_bytes 4096`,
		`mmdash_artifact_operations_total{backend="minio",operation="confirm",outcome="success"} 1`,
		`mmdash_artifact_operations_total{backend="unknown",operation="other",outcome="error"} 1`,
		`mmdash_artifact_operation_duration_seconds{backend="minio",operation="confirm"} 0.250000`,
		`mmdash_agent_adapter_requests_total{adapter="hermes",operation="stream",outcome="success"} 1`,
		`mmdash_agent_adapter_requests_total{adapter="unknown",operation="other",outcome="error"} 1`,
		`mmdash_agent_connection_checks_total{kind="project_access",outcome="passed"} 1`,
		`mmdash_agent_runs_total{status="waiting_for_approval"} 1`,
		`mmdash_agent_sse_streams_active 1`,
		`mmdash_agent_token_lifecycle_total{action="rotate",outcome="error"} 1`,
		`mmdash_notification_created_total 1`,
		`mmdash_notification_inbox_unread 4`,
		`mmdash_notification_delivery_pending 5`,
		`mmdash_notification_delivery_retries_total 1`,
		`mmdash_notification_delivery_failures_total 1`,
		`mmdash_notification_delivery_duration_seconds_count 2`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, body)
		}
	}
	for _, secret := range []string{"secret-runtime-url", "secret-operation", "secret-outcome"} {
		if strings.Contains(body, secret) {
			t.Fatalf("metrics leaked unbounded Agent label %q:\n%s", secret, body)
		}
	}
}
