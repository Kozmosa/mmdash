package hermes

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/agent"
)

func (adapter *Adapter) StreamChat(ctx context.Context, remoteID string, request agent.ChatRequest, options agent.StreamOptions, handler agent.EventHandler) error {
	if err := requireString(request.Message, "message"); err != nil {
		return err
	}
	if handler == nil {
		return agent.ErrInvalidArgument
	}
	id, err := pathID(remoteID)
	if err != nil {
		return err
	}
	body := map[string]any{"message": request.Message}
	if request.Instructions != "" {
		body["instructions"] = request.Instructions
	}
	response, err := adapter.runtime.openStream(
		ctx,
		"hermes.sessions.chat_stream",
		http.MethodPost,
		"/api/sessions/"+id+"/chat/stream",
		nil,
		body,
		streamHeaders(options),
	)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return consumeSSE(ctx, response.Body, remoteID, adapter.runtime.connector.policy.MaxResponseBytes, handler)
}

func (adapter *Adapter) StreamRun(ctx context.Context, remoteID string, options agent.StreamOptions, handler agent.EventHandler) error {
	if handler == nil {
		return agent.ErrInvalidArgument
	}
	id, err := pathID(remoteID)
	if err != nil {
		return err
	}
	response, err := adapter.runtime.openStream(
		ctx,
		"hermes.runs.stream",
		http.MethodGet,
		"/v1/runs/"+id+"/events",
		nil,
		nil,
		streamHeaders(options),
	)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return consumeSSE(ctx, response.Body, remoteID, adapter.runtime.connector.policy.MaxResponseBytes, handler)
}

func streamHeaders(options agent.StreamOptions) http.Header {
	headers := http.Header{"Accept": {"text/event-stream"}}
	lastEventID := strings.TrimSpace(options.LastEventID)
	if lastEventID != "" && len(lastEventID) <= 1024 && !strings.ContainsAny(lastEventID, "\r\n\x00") {
		headers.Set("Last-Event-ID", lastEventID)
	}
	return headers
}

type sseFrame struct {
	event string
	id    string
	data  bytes.Buffer
}

func consumeSSE(ctx context.Context, reader io.Reader, streamID string, maxEventBytes int64, handler agent.EventHandler) error {
	if maxEventBytes <= 0 {
		maxEventBytes = 4 << 20
	}
	if maxEventBytes > 8<<20 {
		maxEventBytes = 8 << 20
	}
	scanner := bufio.NewScanner(reader)
	lineLimit := int(maxEventBytes)
	if lineLimit < 64<<10 {
		lineLimit = 64 << 10
	}
	scanner.Buffer(make([]byte, 64<<10), lineLimit)
	frame := sseFrame{}
	var sequence int64
	toolCalls := map[string][]string{}
	approvals := []string{}
	dispatch := func() error {
		if frame.data.Len() == 0 {
			frame = sseFrame{}
			return nil
		}
		sequence++
		event, ok := normalizeSSEEvent(frame.event, frame.id, frame.data.Bytes(), streamID, sequence)
		frame = sseFrame{}
		if !ok {
			return nil
		}
		correlateToolCall(&event, streamID, toolCalls)
		correlateApproval(&event, &approvals)
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := handler(ctx, event); err != nil {
			return err
		}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			if strings.Contains(strings.ToLower(line), "keepalive") {
				sequence++
				heartbeat := eventIdentity(agent.Event{Type: agent.EventHeartbeat, Status: "keepalive"}, "", streamID, sequence)
				if err := handler(ctx, heartbeat); err != nil {
					return err
				}
			}
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			value = ""
		} else {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "event":
			frame.event = value
		case "id":
			if !strings.ContainsRune(value, '\x00') {
				frame.id = value
			}
		case "data":
			if frame.data.Len() > 0 {
				frame.data.WriteByte('\n')
			}
			if int64(frame.data.Len()+len(value)) > maxEventBytes {
				return &agent.AdapterError{Code: agent.ErrorProtocol, Operation: "sse", Message: "remote event exceeded size limit"}
			}
			frame.data.WriteString(value)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return normalizeNetworkError("sse", err)
	}
	if err := dispatch(); err != nil {
		return err
	}
	return ctx.Err()
}

func normalizeSSEEvent(namedEvent, upstreamID string, data []byte, streamID string, fallbackSequence int64) (agent.Event, bool) {
	if string(data) == "[DONE]" {
		return eventIdentity(agent.Event{Type: agent.EventDone}, upstreamID, streamID, fallbackSequence), true
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return agent.Event{}, false
	}
	name := strings.TrimSpace(namedEvent)
	if name == "" {
		name = stringValue(payload["event"])
	}
	event := agent.Event{
		RunRemoteID:     stringValue(payload["run_id"]),
		SessionRemoteID: stringValue(payload["session_id"]),
		MessageRemoteID: firstString(payload, "message_id", "id"),
		Timestamp:       rawTime(firstValue(payload, "timestamp", "ts")),
	}
	switch name {
	case "run.started":
		event.Type, event.Status = agent.EventRunStarted, "running"
	case "run.completed":
		event.Type, event.Status = agent.EventRunCompleted, "completed"
		event.Text = stringValue(payload["output"])
		if usage, ok := payload["usage"].(map[string]any); ok {
			event.Usage = usageFromMap(usage)
		}
	case "run.failed":
		event.Type, event.Status = agent.EventRunFailed, "failed"
		event.Error = &agent.SafeError{Code: "remote_run_failed", Message: "remote run failed"}
	case "run.cancelled":
		event.Type, event.Status = agent.EventRunCancelled, "cancelled"
	case "message.started":
		event.Type, event.Status = agent.EventMessageStarted, "streaming"
		if message, ok := payload["message"].(map[string]any); ok {
			event.MessageRemoteID = stringValue(message["id"])
		}
	case "assistant.delta", "message.delta":
		event.Type, event.Status = agent.EventMessageDelta, "streaming"
		event.Text = stringValue(payload["delta"])
	case "assistant.completed":
		event.Type, event.Status = agent.EventMessageCompleted, "completed"
		event.Text = stringValue(payload["content"])
	case "tool.progress":
		event.Type, event.Status = agent.EventToolProgress, "running"
		event.Tool = &agent.ToolCall{Name: firstString(payload, "tool_name", "tool"), Status: "running"}
	case "tool.started":
		event.Type, event.Status = agent.EventToolStarted, "running"
		event.Tool = &agent.ToolCall{Name: firstString(payload, "tool_name", "tool"), Status: "running"}
	case "tool.completed":
		event.Type, event.Status = agent.EventToolCompleted, "completed"
		toolStatus := "completed"
		if boolValue(payload["error"]) {
			event.Type, event.Status = agent.EventToolFailed, "failed"
			toolStatus = "failed"
		}
		event.Tool = &agent.ToolCall{Name: firstString(payload, "tool_name", "tool"), Status: toolStatus}
	case "tool.failed":
		event.Type, event.Status = agent.EventToolFailed, "failed"
		event.Tool = &agent.ToolCall{Name: firstString(payload, "tool_name", "tool"), Status: "failed"}
	case "reasoning.available":
		// Presence is useful UI state, but the reasoning text is deliberately
		// discarded at this boundary.
		event.Type, event.Status = agent.EventToolProgress, "reasoning_available"
	case "subagent.start":
		event.Type, event.Status = agent.EventSubagentStarted, "running"
	case "subagent.complete":
		event.Type, event.Status = agent.EventSubagentCompleted, "completed"
	case "approval.request":
		event.Type, event.Status = agent.EventApprovalRequested, "waiting_for_approval"
		event.Approval = &agent.ApprovalEvent{
			RemoteID: firstString(payload, "approval_id", "request_id"),
			Choices:  normalizedApprovalChoices(payload["choices"]),
		}
	case "approval.responded":
		event.Type, event.Status = agent.EventApprovalResponded, "running"
		event.Approval = &agent.ApprovalEvent{
			RemoteID: firstString(payload, "approval_id", "request_id"),
			Choice:   normalizedApprovalChoice(stringValue(payload["choice"])),
			Resolved: int(intValue(payload["resolved"])),
		}
	case "error":
		event.Type, event.Status = agent.EventError, "failed"
		event.Error = &agent.SafeError{Code: "remote_stream_error", Message: "remote event stream failed"}
	case "done":
		event.Type = agent.EventDone
	default:
		return agent.Event{}, false
	}
	return eventIdentity(event, upstreamID, streamID, fallbackSequence), true
}

func correlateApproval(event *agent.Event, active *[]string) {
	if event == nil || event.Approval == nil || active == nil {
		return
	}
	switch event.Type {
	case agent.EventApprovalRequested:
		if event.Approval.RemoteID == "" {
			event.Approval.RemoteID = event.ID
		}
		*active = append(*active, event.Approval.RemoteID)
	case agent.EventApprovalResponded:
		if event.Approval.RemoteID == "" && len(*active) > 0 {
			event.Approval.RemoteID = (*active)[0]
			*active = (*active)[1:]
		}
		if event.Approval.RemoteID == "" {
			event.Approval.RemoteID = event.ID
		}
	}
}

func correlateToolCall(event *agent.Event, streamID string, active map[string][]string) {
	if event == nil || event.Tool == nil {
		return
	}
	name := event.Tool.Name
	if name == "" {
		name = "unknown"
	}
	switch event.Type {
	case agent.EventToolStarted:
		remoteID := fmt.Sprintf("%s:tool:%d", streamID, event.Sequence)
		event.Tool.RemoteID = remoteID
		active[name] = append(active[name], remoteID)
	case agent.EventToolCompleted, agent.EventToolFailed:
		queue := active[name]
		if len(queue) > 0 {
			event.Tool.RemoteID = queue[0]
			if len(queue) == 1 {
				delete(active, name)
			} else {
				active[name] = queue[1:]
			}
		} else {
			event.Tool.RemoteID = fmt.Sprintf("%s:tool:%d", streamID, event.Sequence)
		}
	}
}

func eventIdentity(event agent.Event, upstreamID, streamID string, fallbackSequence int64) agent.Event {
	event.Sequence = fallbackSequence
	if upstreamID != "" {
		event.ID = upstreamID
	} else {
		event.ID = fmt.Sprintf("%s:%d", streamID, fallbackSequence)
	}
	return event
}

func normalizedApprovalChoices(value any) []agent.ApprovalChoice {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]agent.ApprovalChoice, 0, len(items))
	for _, item := range items {
		choice := normalizedApprovalChoice(stringValue(item))
		if choice != "" {
			result = append(result, choice)
		}
	}
	return result
}

func normalizedApprovalChoice(value string) agent.ApprovalChoice {
	switch agent.ApprovalChoice(value) {
	case agent.ApprovalOnce, agent.ApprovalSession, agent.ApprovalAlways, agent.ApprovalDeny:
		return agent.ApprovalChoice(value)
	default:
		return ""
	}
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if result := stringValue(value[key]); result != "" {
			return result
		}
	}
	return ""
}

func firstValue(value map[string]any, keys ...string) any {
	for _, key := range keys {
		if result, ok := value[key]; ok && result != nil {
			return result
		}
	}
	return nil
}
