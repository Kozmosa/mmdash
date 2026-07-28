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
	http          map[httpKey]*httpMetric
	mu            sync.RWMutex
	service       string
	version       string
	auditFailures uint64
}

type httpKey struct {
	Method string
	Status int
}

type httpMetric struct {
	Count       uint64
	DurationSec float64
}

func New(service, version string) *Registry {
	return &Registry{
		http:    map[httpKey]*httpMetric{},
		service: strings.TrimSpace(service),
		version: strings.TrimSpace(version),
	}
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
	return output.String()
}

func quote(value string) string {
	return strconv.Quote(value)
}
