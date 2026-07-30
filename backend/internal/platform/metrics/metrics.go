// Package metrics provides a dependency-free Prometheus exposition registry.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Registry records bounded-label Core platform metrics.
type Registry struct {
	http              map[httpKey]*httpMetric
	mu                sync.RWMutex
	repo              map[repoKey]*httpMetric
	repoDurations     map[repoDurationKey]*httpMetric
	repoCheckouts     int64
	repoQueue         int64
	repoStorage       int64
	artifact          map[artifactKey]*httpMetric
	artifactDurations map[artifactDurationKey]*httpMetric
	service           string
	version           string
	auditFailures     uint64
}

type httpKey struct {
	Method string
	Status int
}

type httpMetric struct {
	Count       uint64
	DurationSec float64
}

type repoKey struct {
	Operation string
	Outcome   string
	Provider  string
}

type repoDurationKey struct {
	Operation string
	Provider  string
}

type artifactKey struct {
	Backend   string
	Operation string
	Outcome   string
}

type artifactDurationKey struct {
	Backend   string
	Operation string
}

func New(service, version string) *Registry {
	return &Registry{
		http:              map[httpKey]*httpMetric{},
		repo:              map[repoKey]*httpMetric{},
		repoDurations:     map[repoDurationKey]*httpMetric{},
		artifact:          map[artifactKey]*httpMetric{},
		artifactDurations: map[artifactDurationKey]*httpMetric{},
		service:           strings.TrimSpace(service),
		version:           strings.TrimSpace(version),
	}
}

// ObserveArtifactOperation records one bounded Artifact control or storage
// operation. Identifiers, hashes, MIME types, filenames, and URLs are excluded.
func (registry *Registry) ObserveArtifactOperation(
	operation string,
	outcome string,
	backend string,
	duration time.Duration,
) {
	if registry == nil {
		return
	}
	key := artifactKey{
		Operation: boundedLabel(operation, map[string]bool{
			"abort": true, "confirm": true, "download_grant": true,
			"expire": true, "initialize": true,
			"initialize_version": true, "purge": true,
			"sign_parts": true,
		}, "other"),
		Outcome: boundedLabel(
			outcome, map[string]bool{"success": true, "error": true}, "error",
		),
		Backend: boundedLabel(
			backend,
			map[string]bool{"local": true, "minio": true, "s3": true},
			"unknown",
		),
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	metric := registry.artifact[key]
	if metric == nil {
		metric = &httpMetric{}
		registry.artifact[key] = metric
	}
	metric.Count++
	durationKey := artifactDurationKey{
		Backend: key.Backend, Operation: key.Operation,
	}
	durationMetric := registry.artifactDurations[durationKey]
	if durationMetric == nil {
		durationMetric = &httpMetric{}
		registry.artifactDurations[durationKey] = durationMetric
	}
	durationMetric.Count++
	durationMetric.DurationSec += duration.Seconds()
}

// ObserveHTTP records one completed request without unbounded path labels.
func (registry *Registry) ObserveHTTP(method string, status int, duration time.Duration) {
	if registry == nil {
		return
	}
	key := httpKey{Method: strings.ToUpper(method), Status: status}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	metric := registry.http[key]
	if metric == nil {
		metric = &httpMetric{}
		registry.http[key] = metric
	}
	metric.Count++
	metric.DurationSec += duration.Seconds()
}

// IncrementAuditFailure records a failed best-effort audit persistence.
func (registry *Registry) IncrementAuditFailure() {
	if registry == nil {
		return
	}
	registry.mu.Lock()
	registry.auditFailures++
	registry.mu.Unlock()
}

// ObserveRepoOperation records one bounded Repo operation. Callers supply only
// module constants; unexpected labels collapse to a fixed fallback.
func (registry *Registry) ObserveRepoOperation(
	operation string,
	outcome string,
	provider string,
	duration time.Duration,
) {
	if registry == nil {
		return
	}
	key := repoKey{
		Operation: boundedLabel(
			operation,
			map[string]bool{
				"cleanup": true, "clone": true, "commit": true,
				"connect": true, "read": true, "sync": true,
				"webhook": true, "checkout": true,
			},
			"other",
		),
		Outcome: boundedLabel(
			outcome,
			map[string]bool{"success": true, "error": true},
			"error",
		),
		Provider: boundedLabel(
			provider,
			map[string]bool{"github": true, "local": true},
			"unknown",
		),
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	metric := registry.repo[key]
	if metric == nil {
		metric = &httpMetric{}
		registry.repo[key] = metric
	}
	metric.Count++
	durationKey := repoDurationKey{
		Operation: key.Operation,
		Provider:  key.Provider,
	}
	durationMetric := registry.repoDurations[durationKey]
	if durationMetric == nil {
		durationMetric = &httpMetric{}
		registry.repoDurations[durationKey] = durationMetric
	}
	durationMetric.Count++
	durationMetric.DurationSec += duration.Seconds()
}

// SetRepoGauges replaces the current low-cardinality Repo gauges.
func (registry *Registry) SetRepoGauges(
	syncQueueDepth int64,
	checkoutsActive int64,
	storageBytes int64,
) {
	if registry == nil {
		return
	}
	registry.mu.Lock()
	registry.repoQueue = nonNegative(syncQueueDepth)
	registry.repoCheckouts = nonNegative(checkoutsActive)
	registry.repoStorage = nonNegative(storageBytes)
	registry.mu.Unlock()
}

// RegisterRoutes exposes Prometheus text at GET /metrics.
func (registry *Registry) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/metrics", registry.serveHTTP)
}

func (registry *Registry) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	response.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(registry.snapshot()))
}

func (registry *Registry) snapshot() string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	var output strings.Builder
	output.WriteString("# HELP mmdash_build_info Static service build information.\n")
	output.WriteString("# TYPE mmdash_build_info gauge\n")
	fmt.Fprintf(
		&output, "mmdash_build_info{service=%s,version=%s} 1\n",
		quote(registry.service), quote(registry.version),
	)
	output.WriteString("# HELP mmdash_http_requests_total Completed HTTP requests.\n")
	output.WriteString("# TYPE mmdash_http_requests_total counter\n")
	output.WriteString("# HELP mmdash_http_request_duration_seconds_sum Total HTTP request duration.\n")
	output.WriteString("# TYPE mmdash_http_request_duration_seconds_sum counter\n")
	output.WriteString("# HELP mmdash_http_request_duration_seconds_count Timed HTTP requests.\n")
	output.WriteString("# TYPE mmdash_http_request_duration_seconds_count counter\n")
	keys := make([]httpKey, 0, len(registry.http))
	for key := range registry.http {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].Method == keys[right].Method {
			return keys[left].Status < keys[right].Status
		}
		return keys[left].Method < keys[right].Method
	})
	for _, key := range keys {
		metric := registry.http[key]
		labels := fmt.Sprintf(
			"method=%s,status=%s", quote(key.Method), quote(strconv.Itoa(key.Status)),
		)
		fmt.Fprintf(&output, "mmdash_http_requests_total{%s} %d\n", labels, metric.Count)
		fmt.Fprintf(
			&output,
			"mmdash_http_request_duration_seconds_sum{%s} %s\n",
			labels,
			strconv.FormatFloat(metric.DurationSec, 'f', 6, 64),
		)
		fmt.Fprintf(
			&output,
			"mmdash_http_request_duration_seconds_count{%s} %d\n",
			labels,
			metric.Count,
		)
	}
	output.WriteString("# HELP mmdash_audit_write_failures_total Failed audit persistence attempts.\n")
	output.WriteString("# TYPE mmdash_audit_write_failures_total counter\n")
	fmt.Fprintf(
		&output, "mmdash_audit_write_failures_total %d\n", registry.auditFailures,
	)
	output.WriteString("# HELP mmdash_repo_operations_total Completed Repo operations.\n")
	output.WriteString("# TYPE mmdash_repo_operations_total counter\n")
	output.WriteString(
		"# HELP mmdash_repo_operation_duration_seconds Total Repo operation duration.\n",
	)
	output.WriteString("# TYPE mmdash_repo_operation_duration_seconds counter\n")
	repoKeys := make([]repoKey, 0, len(registry.repo))
	for key := range registry.repo {
		repoKeys = append(repoKeys, key)
	}
	sort.Slice(repoKeys, func(left, right int) bool {
		leftValue := repoKeys[left].Operation + repoKeys[left].Outcome + repoKeys[left].Provider
		rightValue := repoKeys[right].Operation + repoKeys[right].Outcome + repoKeys[right].Provider
		return leftValue < rightValue
	})
	for _, key := range repoKeys {
		metric := registry.repo[key]
		countLabels := fmt.Sprintf(
			"operation=%s,outcome=%s,provider=%s",
			quote(key.Operation),
			quote(key.Outcome),
			quote(key.Provider),
		)
		fmt.Fprintf(
			&output,
			"mmdash_repo_operations_total{%s} %d\n",
			countLabels,
			metric.Count,
		)
	}
	durationKeys := make([]repoDurationKey, 0, len(registry.repoDurations))
	for key := range registry.repoDurations {
		durationKeys = append(durationKeys, key)
	}
	sort.Slice(durationKeys, func(left, right int) bool {
		leftValue := durationKeys[left].Operation + durationKeys[left].Provider
		rightValue := durationKeys[right].Operation + durationKeys[right].Provider
		return leftValue < rightValue
	})
	for _, key := range durationKeys {
		metric := registry.repoDurations[key]
		labels := fmt.Sprintf(
			"operation=%s,provider=%s",
			quote(key.Operation),
			quote(key.Provider),
		)
		fmt.Fprintf(
			&output,
			"mmdash_repo_operation_duration_seconds{%s} %s\n",
			labels,
			strconv.FormatFloat(metric.DurationSec, 'f', 6, 64),
		)
	}
	output.WriteString("# HELP mmdash_repo_sync_queue_depth Repositories awaiting synchronization.\n")
	output.WriteString("# TYPE mmdash_repo_sync_queue_depth gauge\n")
	fmt.Fprintf(&output, "mmdash_repo_sync_queue_depth %d\n", registry.repoQueue)
	output.WriteString("# HELP mmdash_repo_checkouts_active Active detached Repo checkouts.\n")
	output.WriteString("# TYPE mmdash_repo_checkouts_active gauge\n")
	fmt.Fprintf(&output, "mmdash_repo_checkouts_active %d\n", registry.repoCheckouts)
	output.WriteString("# HELP mmdash_repo_storage_bytes Managed Repo storage bytes.\n")
	output.WriteString("# TYPE mmdash_repo_storage_bytes gauge\n")
	fmt.Fprintf(&output, "mmdash_repo_storage_bytes %d\n", registry.repoStorage)
	output.WriteString("# HELP mmdash_artifact_operations_total Completed Artifact operations.\n")
	output.WriteString("# TYPE mmdash_artifact_operations_total counter\n")
	output.WriteString(
		"# HELP mmdash_artifact_operation_duration_seconds Total Artifact operation duration.\n",
	)
	output.WriteString("# TYPE mmdash_artifact_operation_duration_seconds counter\n")
	artifactKeys := make([]artifactKey, 0, len(registry.artifact))
	for key := range registry.artifact {
		artifactKeys = append(artifactKeys, key)
	}
	sort.Slice(artifactKeys, func(left, right int) bool {
		leftValue := artifactKeys[left].Operation +
			artifactKeys[left].Outcome + artifactKeys[left].Backend
		rightValue := artifactKeys[right].Operation +
			artifactKeys[right].Outcome + artifactKeys[right].Backend
		return leftValue < rightValue
	})
	for _, key := range artifactKeys {
		metric := registry.artifact[key]
		labels := fmt.Sprintf(
			"backend=%s,operation=%s,outcome=%s",
			quote(key.Backend), quote(key.Operation), quote(key.Outcome),
		)
		fmt.Fprintf(
			&output, "mmdash_artifact_operations_total{%s} %d\n",
			labels, metric.Count,
		)
	}
	artifactDurationKeys := make(
		[]artifactDurationKey, 0, len(registry.artifactDurations),
	)
	for key := range registry.artifactDurations {
		artifactDurationKeys = append(artifactDurationKeys, key)
	}
	sort.Slice(artifactDurationKeys, func(left, right int) bool {
		leftValue := artifactDurationKeys[left].Operation +
			artifactDurationKeys[left].Backend
		rightValue := artifactDurationKeys[right].Operation +
			artifactDurationKeys[right].Backend
		return leftValue < rightValue
	})
	for _, key := range artifactDurationKeys {
		metric := registry.artifactDurations[key]
		labels := fmt.Sprintf(
			"backend=%s,operation=%s",
			quote(key.Backend), quote(key.Operation),
		)
		fmt.Fprintf(
			&output,
			"mmdash_artifact_operation_duration_seconds{%s} %s\n",
			labels,
			strconv.FormatFloat(metric.DurationSec, 'f', 6, 64),
		)
	}
	return output.String()
}

func quote(value string) string {
	return strconv.Quote(value)
}

func boundedLabel(value string, allowed map[string]bool, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if allowed[value] {
		return value
	}
	return fallback
}

func nonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
