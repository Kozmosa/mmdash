package hermes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/agent"
)

type hermesSession struct {
	ID               string          `json:"id"`
	ParentSessionID  string          `json:"parent_session_id"`
	Source           string          `json:"source"`
	Model            string          `json:"model"`
	Title            string          `json:"title"`
	StartedAt        json.RawMessage `json:"started_at"`
	EndedAt          json.RawMessage `json:"ended_at"`
	EndReason        string          `json:"end_reason"`
	LastActive       json.RawMessage `json:"last_active"`
	Preview          string          `json:"preview"`
	MessageCount     int64           `json:"message_count"`
	ToolCallCount    int64           `json:"tool_call_count"`
	InputTokens      int64           `json:"input_tokens"`
	OutputTokens     int64           `json:"output_tokens"`
	CacheReadTokens  int64           `json:"cache_read_tokens"`
	CacheWriteTokens int64           `json:"cache_write_tokens"`
	ReasoningTokens  int64           `json:"reasoning_tokens"`
	EstimatedCostUSD float64         `json:"estimated_cost_usd"`
	ActualCostUSD    float64         `json:"actual_cost_usd"`
	APICallCount     int64           `json:"api_call_count"`
	HasSystemPrompt  bool            `json:"has_system_prompt"`
	HasModelConfig   bool            `json:"has_model_config"`
}

func (session hermesSession) normalized() agent.Session {
	started, _ := parseTime(session.StartedAt)
	ended, endedOK := parseTime(session.EndedAt)
	lastActive, lastActiveOK := parseTime(session.LastActive)
	result := agent.Session{
		RemoteID: session.ID, ParentRemoteID: session.ParentSessionID,
		Source: session.Source, Model: session.Model, Title: session.Title,
		StartedAt: started, EndReason: session.EndReason, Preview: session.Preview,
		MessageCount: session.MessageCount, ToolCallCount: session.ToolCallCount,
		InputTokens: session.InputTokens, OutputTokens: session.OutputTokens,
		CacheReadTokens: session.CacheReadTokens, CacheWriteTokens: session.CacheWriteTokens,
		ReasoningTokens: session.ReasoningTokens, EstimatedCostUSD: session.EstimatedCostUSD,
		ActualCostUSD: session.ActualCostUSD, APICallCount: session.APICallCount,
		HasSystemPrompt: session.HasSystemPrompt, HasModelConfig: session.HasModelConfig,
	}
	if endedOK {
		result.EndedAt = &ended
	}
	if lastActiveOK {
		result.LastActiveAt = &lastActive
	}
	return result
}

type hermesMessage struct {
	ID           string          `json:"id"`
	SessionID    string          `json:"session_id"`
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	ToolCallID   string          `json:"tool_call_id"`
	ToolCalls    json.RawMessage `json:"tool_calls"`
	ToolName     string          `json:"tool_name"`
	Timestamp    json.RawMessage `json:"timestamp"`
	TokenCount   int64           `json:"token_count"`
	FinishReason string          `json:"finish_reason"`
	// reasoning and reasoning_content are intentionally not represented.
}

func (message hermesMessage) normalized() agent.Message {
	result := agent.Message{
		RemoteID: message.ID, SessionRemoteID: message.SessionID,
		Role: message.Role, ToolCallID: message.ToolCallID,
		ToolName: message.ToolName, TokenCount: message.TokenCount,
		FinishReason: message.FinishReason,
	}
	if message.Role != "tool" {
		result.Content = safeContent(message.Content)
	}
	result.ToolCalls = safeToolCalls(message.ToolCalls)
	if timestamp, ok := parseTime(message.Timestamp); ok {
		result.Timestamp = &timestamp
	}
	return result
}

func safeContent(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []map[string]any
	if json.Unmarshal(raw, &blocks) == nil {
		parts := make([]string, 0, len(blocks))
		for _, block := range blocks {
			kind, _ := block["type"].(string)
			value, _ := block["text"].(string)
			if (kind == "text" || kind == "output_text") && value != "" {
				parts = append(parts, value)
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

func safeToolCalls(raw json.RawMessage) []agent.ToolCall {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var calls []map[string]any
	if json.Unmarshal(raw, &calls) != nil {
		return nil
	}
	result := make([]agent.ToolCall, 0, len(calls))
	for _, call := range calls {
		id, _ := call["id"].(string)
		name, _ := call["name"].(string)
		status, _ := call["status"].(string)
		if function, ok := call["function"].(map[string]any); ok {
			if functionName, ok := function["name"].(string); ok {
				name = functionName
			}
		}
		if id != "" || name != "" {
			result = append(result, agent.ToolCall{RemoteID: id, Name: name, Status: status})
		}
	}
	return result
}

func parseTime(raw json.RawMessage) (time.Time, bool) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == `""` {
		return time.Time{}, false
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		seconds, err := strconv.ParseFloat(number.String(), 64)
		if err == nil {
			whole := int64(seconds)
			return time.Unix(whole, int64((seconds-float64(whole))*float64(time.Second))).UTC(), true
		}
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, value); err == nil {
				return parsed.UTC(), true
			}
		}
		if seconds, err := strconv.ParseFloat(value, 64); err == nil {
			whole := int64(seconds)
			return time.Unix(whole, int64((seconds-float64(whole))*float64(time.Second))).UTC(), true
		}
	}
	return time.Time{}, false
}

func usageFromMap(value map[string]any) agent.Usage {
	return agent.Usage{
		InputTokens:  intValue(value["input_tokens"]),
		OutputTokens: intValue(value["output_tokens"]),
		TotalTokens:  intValue(value["total_tokens"]),
	}
}

func intValue(value any) int64 {
	switch typed := value.(type) {
	case json.Number:
		result, _ := typed.Int64()
		return result
	case float64:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case string:
		result, _ := strconv.ParseInt(typed, 10, 64)
		return result
	default:
		return 0
	}
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func rawTime(value any) *time.Time {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	parsed, ok := parseTime(payload)
	if !ok {
		return nil
	}
	return &parsed
}

func normalizedJob(value map[string]any) agent.Job {
	job := agent.Job{
		RemoteID: stringValue(value["id"]), Name: stringValue(value["name"]),
		Prompt: stringValue(value["prompt"]), ScheduleDisplay: stringValue(value["schedule_display"]),
		Enabled: boolValue(value["enabled"]), State: stringValue(value["state"]),
		Deliver: stringValue(value["deliver"]), Origin: stringValue(value["origin"]),
		CreatedAt: rawTime(value["created_at"]), NextRunAt: rawTime(value["next_run_at"]),
		LastRunAt: rawTime(value["last_run_at"]), LastStatus: stringValue(value["last_status"]),
		HasLastError:     strings.TrimSpace(stringValue(value["last_error"])) != "",
		HasDeliveryError: strings.TrimSpace(stringValue(value["last_delivery_error"])) != "",
	}
	job.Schedule = normalizedSchedule(value["schedule"])
	if skills, ok := value["skills"].([]any); ok {
		for _, skill := range skills {
			if name := stringValue(skill); name != "" {
				job.Skills = append(job.Skills, name)
			}
		}
	}
	if len(job.Skills) == 0 {
		if skill := stringValue(value["skill"]); skill != "" {
			job.Skills = []string{skill}
		}
	}
	if repeat, ok := value["repeat"].(map[string]any); ok {
		job.RepeatTimes = int(intValue(repeat["times"]))
		job.RepeatCompleted = int(intValue(repeat["completed"]))
	} else {
		job.RepeatTimes = int(intValue(value["repeat"]))
	}
	return job
}

func normalizedSchedule(value any) string {
	if text := stringValue(value); text != "" {
		return text
	}
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"expression", "cron", "schedule", "at", "every"} {
		if text := stringValue(object[key]); text != "" {
			return text
		}
	}
	kind := stringValue(object["type"])
	if kind != "" {
		return kind
	}
	return ""
}

func safeRunError(value any) *agent.SafeError {
	if value == nil || value == "" {
		return nil
	}
	code := "remote_run_failed"
	if object, ok := value.(map[string]any); ok {
		if candidate := stringValue(object["code"]); safeRemoteCode.MatchString(candidate) {
			if _, known := knownRemoteCodes[candidate]; known {
				code = candidate
			}
		}
	}
	return &agent.SafeError{Code: code, Message: "remote run failed"}
}

func runtimeSelection(value map[string]any) agent.RuntimeSelection {
	return agent.RuntimeSelection{
		Provider: stringValue(value["provider"]), Model: stringValue(value["model"]),
		RouteSource: stringValue(value["route_source"]), ModelLock: stringValue(value["model_lock"]),
	}
}

func requireString(value, field string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s is required", agent.ErrInvalidArgument, field)
	}
	return nil
}
