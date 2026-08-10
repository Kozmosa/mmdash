package hermes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/agent"
)

type Adapter struct {
	instanceID string
	profile    string
	runtime    *apiClient
	management *managementClient
}

var _ agent.Adapter = (*Adapter)(nil)

func New(config Config) (*Adapter, error) {
	if strings.TrimSpace(config.InstanceID) == "" || strings.TrimSpace(config.RuntimeURL) == "" || strings.TrimSpace(config.APIKey) == "" {
		return nil, agent.ErrInvalidArgument
	}
	if err := validateProfile(config.Profile); err != nil {
		return nil, err
	}
	runtimeConnector, err := newConnector(config.RuntimeURL, config.RuntimePolicy)
	if err != nil {
		return nil, err
	}
	result := &Adapter{
		instanceID: strings.TrimSpace(config.InstanceID),
		profile:    strings.TrimSpace(config.Profile),
		runtime: &apiClient{
			connector:   runtimeConnector,
			bearerToken: config.APIKey,
			profile:     strings.TrimSpace(config.Profile),
		},
	}
	if config.Management != nil && strings.TrimSpace(config.Management.URL) != "" && config.Management.DashboardSessionToken != "" {
		managementConnector, connectorErr := newConnector(config.Management.URL, config.ManagementPolicy)
		if connectorErr != nil {
			return nil, connectorErr
		}
		result.management = newManagementClient(
			managementConnector,
			strings.TrimSpace(config.Profile),
			*config.Management,
			config.ManagementMinimumInterval,
		)
	}
	return result, nil
}

func (adapter *Adapter) Probe(ctx context.Context) (agent.ProbeResult, error) {
	checkedAt := time.Now().UTC()
	var health struct {
		Status   string `json:"status"`
		Platform string `json:"platform"`
		Version  string `json:"version"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.health", http.MethodGet, "/health", nil, nil, &health, http.StatusOK); err != nil {
		return agent.ProbeResult{CheckedAt: checkedAt}, err
	}
	result := agent.ProbeResult{
		Healthy: health.Status == "ok", Platform: health.Platform,
		Version: health.Version, CheckedAt: checkedAt,
	}
	var detailed map[string]any
	if err := adapter.runtime.doJSON(ctx, "hermes.authentication", http.MethodGet, "/health/detailed", nil, nil, &detailed, http.StatusOK); err != nil {
		return result, err
	}
	result.Authenticated = true

	var capabilityResponse struct {
		Platform string `json:"platform"`
		Model    string `json:"model"`
		// Hermes feature metadata is heterogeneous: boolean capability flags
		// share this object with values such as session header names. Decode the
		// object generically and only interpret the boolean flags we own.
		Features  map[string]any `json:"features"`
		Endpoints map[string]struct {
			Method string `json:"method"`
			Path   string `json:"path"`
		} `json:"endpoints"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.capabilities", http.MethodGet, "/v1/capabilities", nil, nil, &capabilityResponse, http.StatusOK); err != nil {
		return result, err
	}
	if capabilityResponse.Platform != "" {
		result.Platform = capabilityResponse.Platform
	}
	result.Model = capabilityResponse.Model
	features := capabilityResponse.Features
	result.Capabilities = agent.RuntimeCapabilities{
		Sessions: boolValue(features["session_resources"]), SessionFork: boolValue(features["session_fork"]),
		SessionChat: boolValue(features["session_chat"]), SessionStreaming: boolValue(features["session_chat_streaming"]),
		Runs: boolValue(features["run_submission"]) && boolValue(features["run_status"]), RunStreaming: boolValue(features["run_events_sse"]),
		RunStop: boolValue(features["run_stop"]), RunApproval: boolValue(features["run_approval_response"]),
		ToolProgress: boolValue(features["tool_progress_events"]), EventReplay: false,
		ProjectAccess: agent.ProjectAccessCapabilities{Verify: true},
	}
	if err := validateCapabilityEndpoints(capabilityResponse.Endpoints); err != nil {
		return result, err
	}
	if _, err := adapter.ListSessions(ctx, agent.SessionFilter{Page: agent.PageRequest{Limit: 1}}); err != nil {
		return result, err
	}
	if _, err := adapter.ListJobs(ctx, true); err == nil {
		result.Capabilities.Jobs = true
	} else if adapterError, ok := err.(*agent.AdapterError); !ok || adapterError.Code != agent.ErrorUnavailable && adapterError.Code != agent.ErrorUnsupported {
		return result, err
	}
	// Runtime health and capability probing is deliberately independent from
	// Dashboard management reachability. A configured management client means
	// the adapter supports those operations, while CheckConnections probes the
	// live management and project-access paths separately.
	if adapter.management != nil {
		result.Capabilities.ProjectAccess.Configure = true
		result.Capabilities.ProjectAccess.Rotate = true
	}
	return result, nil
}

func validateCapabilityEndpoints(endpoints map[string]struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}) error {
	required := map[string]string{
		"sessions": "/api/sessions", "session_chat": "/api/sessions/{session_id}/chat",
		"session_chat_stream": "/api/sessions/{session_id}/chat/stream",
		"runs":                "/v1/runs", "run_status": "/v1/runs/{run_id}",
		"run_events": "/v1/runs/{run_id}/events", "run_stop": "/v1/runs/{run_id}/stop",
	}
	for name, path := range required {
		if endpoint, ok := endpoints[name]; !ok || endpoint.Path != path {
			return &agent.AdapterError{Code: agent.ErrorUnsupported, Operation: "hermes.capabilities", Message: "required Hermes endpoint is unavailable"}
		}
	}
	return nil
}

func (adapter *Adapter) ListSessions(ctx context.Context, filter agent.SessionFilter) (agent.SessionPage, error) {
	limit := filter.Page.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 || filter.Page.Offset < 0 {
		return agent.SessionPage{}, agent.ErrInvalidArgument
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(filter.Page.Offset)}}
	if filter.Source != "" {
		query.Set("source", filter.Source)
	}
	if filter.IncludeChildren {
		query.Set("include_children", "true")
	}
	var response struct {
		Data    []hermesSession `json:"data"`
		Limit   int             `json:"limit"`
		Offset  int             `json:"offset"`
		HasMore bool            `json:"has_more"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.sessions.list", http.MethodGet, "/api/sessions", query, nil, &response, http.StatusOK); err != nil {
		return agent.SessionPage{}, err
	}
	page := agent.SessionPage{Limit: response.Limit, Offset: response.Offset, HasMore: response.HasMore, Sessions: make([]agent.Session, 0, len(response.Data))}
	for _, session := range response.Data {
		page.Sessions = append(page.Sessions, session.normalized())
	}
	return page, nil
}

func (adapter *Adapter) CreateSession(ctx context.Context, request agent.CreateSessionRequest) (agent.Session, error) {
	body := map[string]any{}
	if request.RemoteID != "" {
		body["id"] = request.RemoteID
	}
	if request.Model != "" {
		body["model"] = request.Model
	}
	if request.SystemPrompt != "" {
		body["system_prompt"] = request.SystemPrompt
	}
	if request.Source != "" {
		body["source"] = request.Source
	}
	if request.Title != "" {
		body["title"] = request.Title
	}
	var response struct {
		Session hermesSession `json:"session"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.sessions.create", http.MethodPost, "/api/sessions", nil, body, &response, http.StatusCreated); err != nil {
		return agent.Session{}, err
	}
	return response.Session.normalized(), nil
}

func (adapter *Adapter) GetSession(ctx context.Context, remoteID string) (agent.Session, error) {
	id, err := pathID(remoteID)
	if err != nil {
		return agent.Session{}, err
	}
	var response struct {
		Session hermesSession `json:"session"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.sessions.get", http.MethodGet, "/api/sessions/"+id, nil, nil, &response, http.StatusOK); err != nil {
		return agent.Session{}, err
	}
	return response.Session.normalized(), nil
}

func (adapter *Adapter) UpdateSession(ctx context.Context, remoteID string, request agent.UpdateSessionRequest) (agent.Session, error) {
	id, err := pathID(remoteID)
	if err != nil {
		return agent.Session{}, err
	}
	body := map[string]any{}
	if request.Title != nil {
		body["title"] = *request.Title
	}
	if request.EndReason != nil {
		body["end_reason"] = *request.EndReason
	}
	if len(body) == 0 {
		return agent.Session{}, agent.ErrInvalidArgument
	}
	var response struct {
		Session hermesSession `json:"session"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.sessions.update", http.MethodPatch, "/api/sessions/"+id, nil, body, &response, http.StatusOK); err != nil {
		return agent.Session{}, err
	}
	return response.Session.normalized(), nil
}

func (adapter *Adapter) DeleteSession(ctx context.Context, remoteID string) error {
	id, err := pathID(remoteID)
	if err != nil {
		return err
	}
	var response struct {
		Deleted bool `json:"deleted"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.sessions.delete", http.MethodDelete, "/api/sessions/"+id, nil, nil, &response, http.StatusOK); err != nil {
		return err
	}
	if !response.Deleted {
		return &agent.AdapterError{Code: agent.ErrorProtocol, Operation: "hermes.sessions.delete", Message: "remote session was not deleted"}
	}
	return nil
}

func (adapter *Adapter) ForkSession(ctx context.Context, remoteID string, request agent.ForkSessionRequest) (agent.Session, error) {
	id, err := pathID(remoteID)
	if err != nil {
		return agent.Session{}, err
	}
	body := map[string]any{}
	if request.RemoteID != "" {
		body["id"] = request.RemoteID
	}
	if request.Title != "" {
		body["title"] = request.Title
	}
	var response struct {
		Session hermesSession `json:"session"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.sessions.fork", http.MethodPost, "/api/sessions/"+id+"/fork", nil, body, &response, http.StatusCreated); err != nil {
		return agent.Session{}, err
	}
	return response.Session.normalized(), nil
}

func (adapter *Adapter) ListMessages(ctx context.Context, remoteID string) ([]agent.Message, error) {
	id, err := pathID(remoteID)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data []hermesMessage `json:"data"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.sessions.messages", http.MethodGet, "/api/sessions/"+id+"/messages", nil, nil, &response, http.StatusOK); err != nil {
		return nil, err
	}
	messages := make([]agent.Message, 0, len(response.Data))
	for _, message := range response.Data {
		messages = append(messages, message.normalized())
	}
	return messages, nil
}

func (adapter *Adapter) Chat(ctx context.Context, remoteID string, request agent.ChatRequest) (agent.ChatResponse, error) {
	if err := requireString(request.Message, "message"); err != nil {
		return agent.ChatResponse{}, err
	}
	id, err := pathID(remoteID)
	if err != nil {
		return agent.ChatResponse{}, err
	}
	body := map[string]any{"message": request.Message}
	if request.Instructions != "" {
		body["instructions"] = request.Instructions
	}
	var response struct {
		SessionID string `json:"session_id"`
		Message   struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		Usage   map[string]any `json:"usage"`
		Runtime map[string]any `json:"runtime"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.sessions.chat", http.MethodPost, "/api/sessions/"+id+"/chat", nil, body, &response, http.StatusOK); err != nil {
		return agent.ChatResponse{}, err
	}
	return agent.ChatResponse{
		SessionRemoteID: response.SessionID,
		Message:         agent.Message{SessionRemoteID: response.SessionID, Role: response.Message.Role, Content: safeContent(response.Message.Content)},
		Usage:           usageFromMap(response.Usage), Runtime: runtimeSelection(response.Runtime),
	}, nil
}

func (adapter *Adapter) StartRun(ctx context.Context, request agent.StartRunRequest) (agent.Run, error) {
	if err := requireString(request.Input, "input"); err != nil {
		return agent.Run{}, err
	}
	body := map[string]any{"input": request.Input}
	if request.Instructions != "" {
		body["instructions"] = request.Instructions
	}
	if request.SessionRemoteID != "" {
		body["session_id"] = request.SessionRemoteID
	}
	if len(request.ConversationHistory) > 0 {
		body["conversation_history"] = request.ConversationHistory
	}
	if request.Model != "" {
		body["model"] = request.Model
	}
	if request.Provider != "" {
		body["provider"] = request.Provider
	}
	var response struct {
		RunID  string `json:"run_id"`
		Status string `json:"status"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.runs.start", http.MethodPost, "/v1/runs", nil, body, &response, http.StatusAccepted); err != nil {
		return agent.Run{}, err
	}
	return agent.Run{RemoteID: response.RunID, SessionRemoteID: request.SessionRemoteID, Status: normalizeRunStatus(response.Status)}, nil
}

func (adapter *Adapter) GetRun(ctx context.Context, remoteID string) (agent.Run, error) {
	id, err := pathID(remoteID)
	if err != nil {
		return agent.Run{}, err
	}
	var response map[string]any
	if err := adapter.runtime.doJSON(ctx, "hermes.runs.get", http.MethodGet, "/v1/runs/"+id, nil, nil, &response, http.StatusOK); err != nil {
		return agent.Run{}, err
	}
	return normalizedRun(response), nil
}

func (adapter *Adapter) ApproveRun(ctx context.Context, remoteID string, request agent.ApprovalRequest) (agent.ApprovalResult, error) {
	if request.Choice != agent.ApprovalOnce && request.Choice != agent.ApprovalSession && request.Choice != agent.ApprovalAlways && request.Choice != agent.ApprovalDeny {
		return agent.ApprovalResult{}, agent.ErrInvalidArgument
	}
	approvalID := strings.TrimSpace(request.RemoteID)
	if approvalID == "" || len(approvalID) > 500 || strings.ContainsAny(approvalID, "\r\n\x00") {
		return agent.ApprovalResult{}, agent.ErrInvalidArgument
	}
	id, err := pathID(remoteID)
	if err != nil {
		return agent.ApprovalResult{}, err
	}
	// Hermes v2026.8.3 has no approval-ID request field. Core has already
	// claimed the FIFO head represented by approvalID; resolve_all=false maps
	// that stable mmdash ID to Hermes' verified oldest-pending operation.
	body := map[string]any{"choice": request.Choice}
	if request.ResolveAll {
		body["resolve_all"] = true
	}
	var response struct {
		RunID    string               `json:"run_id"`
		Choice   agent.ApprovalChoice `json:"choice"`
		Resolved int                  `json:"resolved"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.runs.approve", http.MethodPost, "/v1/runs/"+id+"/approval", nil, body, &response, http.StatusOK); err != nil {
		return agent.ApprovalResult{}, err
	}
	if response.RunID != remoteID || response.Choice != request.Choice ||
		response.Resolved < 1 || (!request.ResolveAll && response.Resolved != 1) {
		return agent.ApprovalResult{}, unexpectedObject("hermes.runs.approve")
	}
	return agent.ApprovalResult{
		RemoteID: approvalID, RunRemoteID: response.RunID,
		Choice: response.Choice, Resolved: response.Resolved,
	}, nil
}

func (adapter *Adapter) StopRun(ctx context.Context, remoteID string) (agent.Run, error) {
	id, err := pathID(remoteID)
	if err != nil {
		return agent.Run{}, err
	}
	var response struct {
		RunID  string `json:"run_id"`
		Status string `json:"status"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.runs.stop", http.MethodPost, "/v1/runs/"+id+"/stop", nil, map[string]any{}, &response, http.StatusOK); err != nil {
		return agent.Run{}, err
	}
	return agent.Run{RemoteID: response.RunID, Status: normalizeRunStatus(response.Status)}, nil
}

func normalizedRun(value map[string]any) agent.Run {
	run := agent.Run{
		RemoteID: stringValue(value["run_id"]), SessionRemoteID: stringValue(value["session_id"]),
		Status: normalizeRunStatus(stringValue(value["status"])), Model: stringValue(value["model"]),
		Output: stringValue(value["output"]), LastEvent: stringValue(value["last_event"]),
		Error: safeRunError(value["error"]), CreatedAt: rawTime(value["created_at"]), UpdatedAt: rawTime(value["updated_at"]),
	}
	if usage, ok := value["usage"].(map[string]any); ok {
		run.Usage = usageFromMap(usage)
	}
	return run
}

func normalizeRunStatus(value string) agent.RunStatus {
	switch value {
	case "started":
		return agent.RunQueued
	case string(agent.RunQueued), string(agent.RunRunning), string(agent.RunWaitingForApproval), string(agent.RunStopping), string(agent.RunCompleted), string(agent.RunFailed), string(agent.RunCancelled):
		return agent.RunStatus(value)
	default:
		return agent.RunStatus(value)
	}
}

func (adapter *Adapter) ListJobs(ctx context.Context, includeDisabled bool) ([]agent.Job, error) {
	query := url.Values{}
	if includeDisabled {
		query.Set("include_disabled", "true")
	}
	var response struct {
		Jobs []map[string]any `json:"jobs"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.jobs.list", http.MethodGet, "/api/jobs", query, nil, &response, http.StatusOK); err != nil {
		return nil, err
	}
	jobs := make([]agent.Job, 0, len(response.Jobs))
	for _, value := range response.Jobs {
		jobs = append(jobs, normalizedJob(value))
	}
	return jobs, nil
}

func (adapter *Adapter) CreateJob(ctx context.Context, request agent.CreateJobRequest) (agent.Job, error) {
	if requireString(request.Name, "name") != nil || requireString(request.Schedule, "schedule") != nil || len(request.Name) > 200 || len(request.Prompt) > 5000 || request.Repeat < 0 {
		return agent.Job{}, agent.ErrInvalidArgument
	}
	body := map[string]any{"name": request.Name, "schedule": request.Schedule, "prompt": request.Prompt}
	if request.Deliver != "" {
		body["deliver"] = request.Deliver
	}
	if len(request.Skills) > 0 {
		body["skills"] = request.Skills
	}
	if request.Repeat > 0 {
		body["repeat"] = request.Repeat
	}
	return adapter.jobMutation(ctx, "hermes.jobs.create", http.MethodPost, "/api/jobs", body)
}

func (adapter *Adapter) GetJob(ctx context.Context, remoteID string) (agent.Job, error) {
	id, err := pathID(remoteID)
	if err != nil {
		return agent.Job{}, err
	}
	return adapter.jobMutation(ctx, "hermes.jobs.get", http.MethodGet, "/api/jobs/"+id, nil)
}

func (adapter *Adapter) UpdateJob(ctx context.Context, remoteID string, request agent.UpdateJobRequest) (agent.Job, error) {
	id, err := pathID(remoteID)
	if err != nil {
		return agent.Job{}, err
	}
	body := map[string]any{}
	if request.Name != nil {
		body["name"] = *request.Name
	}
	if request.Schedule != nil {
		body["schedule"] = *request.Schedule
	}
	if request.Prompt != nil {
		body["prompt"] = *request.Prompt
	}
	if request.Deliver != nil {
		body["deliver"] = *request.Deliver
	}
	if request.Skills != nil {
		body["skills"] = *request.Skills
	}
	if request.Repeat != nil {
		if *request.Repeat < 1 {
			return agent.Job{}, agent.ErrInvalidArgument
		}
		body["repeat"] = *request.Repeat
	}
	if request.Enabled != nil {
		body["enabled"] = *request.Enabled
	}
	if len(body) == 0 {
		return agent.Job{}, agent.ErrInvalidArgument
	}
	return adapter.jobMutation(ctx, "hermes.jobs.update", http.MethodPatch, "/api/jobs/"+id, body)
}

func (adapter *Adapter) DeleteJob(ctx context.Context, remoteID string) error {
	id, err := pathID(remoteID)
	if err != nil {
		return err
	}
	var response struct {
		OK bool `json:"ok"`
	}
	if err := adapter.runtime.doJSON(ctx, "hermes.jobs.delete", http.MethodDelete, "/api/jobs/"+id, nil, nil, &response, http.StatusOK); err != nil {
		return err
	}
	if !response.OK {
		return &agent.AdapterError{Code: agent.ErrorProtocol, Operation: "hermes.jobs.delete", Message: "remote job was not deleted"}
	}
	return nil
}

func (adapter *Adapter) PauseJob(ctx context.Context, remoteID string) (agent.Job, error) {
	return adapter.jobAction(ctx, remoteID, "pause")
}
func (adapter *Adapter) ResumeJob(ctx context.Context, remoteID string) (agent.Job, error) {
	return adapter.jobAction(ctx, remoteID, "resume")
}
func (adapter *Adapter) RunJob(ctx context.Context, remoteID string) (agent.Job, error) {
	return adapter.jobAction(ctx, remoteID, "run")
}

func (adapter *Adapter) jobAction(ctx context.Context, remoteID, action string) (agent.Job, error) {
	id, err := pathID(remoteID)
	if err != nil {
		return agent.Job{}, err
	}
	return adapter.jobMutation(ctx, "hermes.jobs."+action, http.MethodPost, "/api/jobs/"+id+"/"+action, map[string]any{})
}

func (adapter *Adapter) jobMutation(ctx context.Context, operation, method, path string, body any) (agent.Job, error) {
	var response struct {
		Job map[string]any `json:"job"`
	}
	if err := adapter.runtime.doJSON(ctx, operation, method, path, nil, body, &response, http.StatusOK); err != nil {
		return agent.Job{}, err
	}
	return normalizedJob(response.Job), nil
}

func sortedUnique(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func missingTools(actual, expected []string) []string {
	set := make(map[string]struct{}, len(actual))
	for _, tool := range actual {
		set[tool] = struct{}{}
	}
	missing := []string{}
	for _, tool := range sortedUnique(expected) {
		if _, ok := set[tool]; !ok {
			missing = append(missing, tool)
		}
	}
	return missing
}

func unexpectedObject(operation string) error {
	return &agent.AdapterError{Code: agent.ErrorProtocol, Operation: operation, Message: fmt.Sprintf("unexpected Hermes %s response", TestedVersion)}
}
