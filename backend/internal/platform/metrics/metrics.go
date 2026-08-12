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
	agent                           map[agentKey]uint64
	agentChecks                     map[agentCheckKey]uint64
	agentRuns                       map[string]uint64
	agentStreamsActive              int64
	agentTokens                     map[agentTokenKey]uint64
	http                            map[httpKey]*httpMetric
	mu                              sync.RWMutex
	repo                            map[repoKey]*httpMetric
	repoDurations                   map[repoDurationKey]*httpMetric
	repoCheckouts                   int64
	repoQueue                       int64
	repoStorage                     int64
	artifact                        map[artifactKey]*httpMetric
	artifactDurations               map[artifactDurationKey]*httpMetric
	service                         string
	version                         string
	auditFailures                   uint64
	notificationCreated             uint64
	notificationUnread              int64
	notificationPending             int64
	notificationRetries             uint64
	notificationFailures            uint64
	notificationDeliveryCount       uint64
	notificationDeliveryDurationSec float64
	progressReminderTriggered       uint64
	progressReminderRetries         uint64
	progressReminderFailures        uint64
	progressEvaluations             map[string]uint64
}

type agentKey struct {
	Adapter   string
	Operation string
	Outcome   string
}

type agentCheckKey struct {
	Kind    string
	Outcome string
}

type agentTokenKey struct {
	Action  string
	Outcome string
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
		agent:               map[agentKey]uint64{},
		agentChecks:         map[agentCheckKey]uint64{},
		agentRuns:           map[string]uint64{},
		agentTokens:         map[agentTokenKey]uint64{},
		http:                map[httpKey]*httpMetric{},
		repo:                map[repoKey]*httpMetric{},
		repoDurations:       map[repoDurationKey]*httpMetric{},
		artifact:            map[artifactKey]*httpMetric{},
		artifactDurations:   map[artifactDurationKey]*httpMetric{},
		progressEvaluations: map[string]uint64{},
		service:             strings.TrimSpace(service),
		version:             strings.TrimSpace(version),
	}
}

// ObserveAgentOperation records one adapter request using bounded labels only.
func (registry *Registry) ObserveAgentOperation(adapter, operation, outcome string) {
	if registry == nil {
		return
	}
	key := agentKey{
		Adapter: boundedLabel(adapter, map[string]bool{"hermes": true}, "unknown"),
		Operation: boundedLabel(operation, map[string]bool{
			"probe": true, "session": true, "messages": true, "run": true,
			"stream": true, "stop": true, "project_access": true, "job": true,
		}, "other"),
		Outcome: boundedLabel(outcome, map[string]bool{"success": true, "error": true}, "error"),
	}
	registry.mu.Lock()
	registry.agent[key]++
	registry.mu.Unlock()
}

func (registry *Registry) ObserveAgentConnectionCheck(kind, outcome string) {
	if registry == nil {
		return
	}
	key := agentCheckKey{
		Kind: boundedLabel(kind, map[string]bool{
			"runtime": true, "management": true, "project_access": true,
		}, "other"),
		Outcome: boundedLabel(outcome, map[string]bool{
			"passed": true, "failed": true, "unsupported": true,
		}, "failed"),
	}
	registry.mu.Lock()
	registry.agentChecks[key]++
	registry.mu.Unlock()
}

func (registry *Registry) ObserveAgentRun(status string) {
	if registry == nil {
		return
	}
	status = boundedLabel(status, map[string]bool{
		"queued": true, "running": true, "waiting_for_approval": true,
		"stopping": true, "completed": true, "failed": true, "stopped": true,
	}, "other")
	registry.mu.Lock()
	registry.agentRuns[status]++
	registry.mu.Unlock()
}

func (registry *Registry) AddAgentStream(delta int64) {
	if registry == nil {
		return
	}
	registry.mu.Lock()
	registry.agentStreamsActive = nonNegative(registry.agentStreamsActive + delta)
	registry.mu.Unlock()
}

func (registry *Registry) ObserveAgentToken(action, outcome string) {
	if registry == nil {
		return
	}
	key := agentTokenKey{
		Action: boundedLabel(action, map[string]bool{
			"issue": true, "activate": true, "rotate": true,
			"rotation_failed": true, "revoke": true,
		}, "other"),
		Outcome: boundedLabel(outcome, map[string]bool{"success": true, "error": true}, "error"),
	}
	registry.mu.Lock()
	registry.agentTokens[key]++
	registry.mu.Unlock()
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

// ObserveNotificationCreated records a durable Notification creation. Labels
// such as project IDs, type keys, and user IDs are deliberately excluded.
func (registry *Registry) ObserveNotificationCreated() {
	if registry == nil {
		return
	}
	registry.mu.Lock()
	registry.notificationCreated++
	registry.mu.Unlock()
}

// ObserveNotificationDelivery records one provider attempt using a bounded
// outcome label represented by separate counters.
func (registry *Registry) ObserveNotificationDelivery(outcome string, duration time.Duration) {
	if registry == nil {
		return
	}
	registry.mu.Lock()
	registry.notificationDeliveryCount++
	registry.notificationDeliveryDurationSec += duration.Seconds()
	switch outcome {
	case "retrying":
		registry.notificationRetries++
	case "failed":
		registry.notificationFailures++
	}
	registry.mu.Unlock()
}

// SetNotificationGauges refreshes low-cardinality Notification gauges.
func (registry *Registry) SetNotificationGauges(unread, pending int64) {
	if registry == nil {
		return
	}
	registry.mu.Lock()
	registry.notificationUnread = nonNegative(unread)
	registry.notificationPending = nonNegative(pending)
	registry.mu.Unlock()
}

// ObserveProgressReminder records one bounded reminder processing outcome.
// Reminder, Project, user, note, and error values are deliberately excluded.
func (registry *Registry) ObserveProgressReminder(outcome string) {
	if registry == nil {
		return
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	switch outcome {
	case "triggered":
		registry.progressReminderTriggered++
	case "pending":
		registry.progressReminderRetries++
	case "failed":
		registry.progressReminderFailures++
	}
}

// ObserveProgressEvaluation records one bounded automatic tracking outcome.
func (registry *Registry) ObserveProgressEvaluation(outcome string) {
	if registry == nil {
		return
	}
	outcome = boundedLabel(outcome, map[string]bool{
		"assembly_failed": true,
		"cron_failed":     true,
		"cron_scheduled":  true,
		"merged":          true,
		"queue_failed":    true,
		"queued":          true,
	}, "other")
	registry.mu.Lock()
	registry.progressEvaluations[outcome]++
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
	output.WriteString("# HELP mmdash_agent_adapter_requests_total Agent adapter requests.\n")
	output.WriteString("# TYPE mmdash_agent_adapter_requests_total counter\n")
	agentKeys := make([]agentKey, 0, len(registry.agent))
	for key := range registry.agent {
		agentKeys = append(agentKeys, key)
	}
	sort.Slice(agentKeys, func(left, right int) bool {
		return agentKeys[left].Adapter+agentKeys[left].Operation+agentKeys[left].Outcome <
			agentKeys[right].Adapter+agentKeys[right].Operation+agentKeys[right].Outcome
	})
	for _, key := range agentKeys {
		fmt.Fprintf(&output,
			"mmdash_agent_adapter_requests_total{adapter=%s,operation=%s,outcome=%s} %d\n",
			quote(key.Adapter), quote(key.Operation), quote(key.Outcome), registry.agent[key])
	}
	output.WriteString("# HELP mmdash_agent_connection_checks_total Agent connection checks.\n")
	output.WriteString("# TYPE mmdash_agent_connection_checks_total counter\n")
	checkKeys := make([]agentCheckKey, 0, len(registry.agentChecks))
	for key := range registry.agentChecks {
		checkKeys = append(checkKeys, key)
	}
	sort.Slice(checkKeys, func(left, right int) bool {
		return checkKeys[left].Kind+checkKeys[left].Outcome < checkKeys[right].Kind+checkKeys[right].Outcome
	})
	for _, key := range checkKeys {
		fmt.Fprintf(&output,
			"mmdash_agent_connection_checks_total{kind=%s,outcome=%s} %d\n",
			quote(key.Kind), quote(key.Outcome), registry.agentChecks[key])
	}
	output.WriteString("# HELP mmdash_agent_runs_total Agent Runs observed by terminal or start status.\n")
	output.WriteString("# TYPE mmdash_agent_runs_total counter\n")
	runStatuses := make([]string, 0, len(registry.agentRuns))
	for status := range registry.agentRuns {
		runStatuses = append(runStatuses, status)
	}
	sort.Strings(runStatuses)
	for _, status := range runStatuses {
		fmt.Fprintf(&output, "mmdash_agent_runs_total{status=%s} %d\n", quote(status), registry.agentRuns[status])
	}
	output.WriteString("# HELP mmdash_agent_sse_streams_active Active Agent SSE streams.\n")
	output.WriteString("# TYPE mmdash_agent_sse_streams_active gauge\n")
	fmt.Fprintf(&output, "mmdash_agent_sse_streams_active %d\n", registry.agentStreamsActive)
	output.WriteString("# HELP mmdash_agent_token_lifecycle_total Agent token lifecycle actions.\n")
	output.WriteString("# TYPE mmdash_agent_token_lifecycle_total counter\n")
	tokenKeys := make([]agentTokenKey, 0, len(registry.agentTokens))
	for key := range registry.agentTokens {
		tokenKeys = append(tokenKeys, key)
	}
	sort.Slice(tokenKeys, func(left, right int) bool {
		return tokenKeys[left].Action+tokenKeys[left].Outcome < tokenKeys[right].Action+tokenKeys[right].Outcome
	})
	for _, key := range tokenKeys {
		fmt.Fprintf(&output,
			"mmdash_agent_token_lifecycle_total{action=%s,outcome=%s} %d\n",
			quote(key.Action), quote(key.Outcome), registry.agentTokens[key])
	}
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
	output.WriteString("# HELP mmdash_notification_created_total Durable Notifications created.\n")
	output.WriteString("# TYPE mmdash_notification_created_total counter\n")
	fmt.Fprintf(&output, "mmdash_notification_created_total %d\n", registry.notificationCreated)
	output.WriteString("# HELP mmdash_notification_inbox_unread Current unread Inbox items.\n")
	output.WriteString("# TYPE mmdash_notification_inbox_unread gauge\n")
	fmt.Fprintf(&output, "mmdash_notification_inbox_unread %d\n", registry.notificationUnread)
	output.WriteString("# HELP mmdash_notification_delivery_pending Pending Notification deliveries.\n")
	output.WriteString("# TYPE mmdash_notification_delivery_pending gauge\n")
	fmt.Fprintf(&output, "mmdash_notification_delivery_pending %d\n", registry.notificationPending)
	output.WriteString("# HELP mmdash_notification_delivery_retries_total Notification delivery retries.\n")
	output.WriteString("# TYPE mmdash_notification_delivery_retries_total counter\n")
	fmt.Fprintf(&output, "mmdash_notification_delivery_retries_total %d\n", registry.notificationRetries)
	output.WriteString("# HELP mmdash_notification_delivery_failures_total Terminal Notification delivery failures.\n")
	output.WriteString("# TYPE mmdash_notification_delivery_failures_total counter\n")
	fmt.Fprintf(&output, "mmdash_notification_delivery_failures_total %d\n", registry.notificationFailures)
	output.WriteString("# HELP mmdash_notification_delivery_duration_seconds_sum Notification delivery duration.\n")
	output.WriteString("# TYPE mmdash_notification_delivery_duration_seconds_sum counter\n")
	fmt.Fprintf(&output, "mmdash_notification_delivery_duration_seconds_sum %s\n", strconv.FormatFloat(registry.notificationDeliveryDurationSec, 'f', 6, 64))
	output.WriteString("# HELP mmdash_notification_delivery_duration_seconds_count Notification delivery attempts counted.\n")
	output.WriteString("# TYPE mmdash_notification_delivery_duration_seconds_count counter\n")
	fmt.Fprintf(&output, "mmdash_notification_delivery_duration_seconds_count %d\n", registry.notificationDeliveryCount)
	output.WriteString("# HELP mmdash_progress_reminders_triggered_total Progress reminders committed to the Outbox.\n")
	output.WriteString("# TYPE mmdash_progress_reminders_triggered_total counter\n")
	fmt.Fprintf(&output, "mmdash_progress_reminders_triggered_total %d\n", registry.progressReminderTriggered)
	output.WriteString("# HELP mmdash_progress_reminder_retries_total Progress reminder processing retries.\n")
	output.WriteString("# TYPE mmdash_progress_reminder_retries_total counter\n")
	fmt.Fprintf(&output, "mmdash_progress_reminder_retries_total %d\n", registry.progressReminderRetries)
	output.WriteString("# HELP mmdash_progress_reminder_failures_total Terminal Progress reminder processing failures.\n")
	output.WriteString("# TYPE mmdash_progress_reminder_failures_total counter\n")
	fmt.Fprintf(&output, "mmdash_progress_reminder_failures_total %d\n", registry.progressReminderFailures)
	output.WriteString("# HELP mmdash_progress_evaluations_total Progress automatic tracking outcomes.\n")
	output.WriteString("# TYPE mmdash_progress_evaluations_total counter\n")
	progressOutcomes := make([]string, 0, len(registry.progressEvaluations))
	for outcome := range registry.progressEvaluations {
		progressOutcomes = append(progressOutcomes, outcome)
	}
	sort.Strings(progressOutcomes)
	for _, outcome := range progressOutcomes {
		fmt.Fprintf(&output, "mmdash_progress_evaluations_total{outcome=%s} %d\n", quote(outcome), registry.progressEvaluations[outcome])
	}
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
