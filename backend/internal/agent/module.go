package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
	"github.com/mmdash/mmdash/backend/internal/project"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

// Module exposes the Runtime-neutral Agent management and chat surface. It
// deliberately constructs contract-shaped projections instead of serializing
// provider or persistence models directly.
type Module struct{ Service Service }

func (Module) Name() string { return "agent" }

func (module Module) RegisterRoutes(*http.ServeMux) {}

func (module Module) ProjectHandler() http.Handler {
	return http.HandlerFunc(module.handleProject)
}

func (module Module) handleProject(response http.ResponseWriter, request *http.Request) {
	identity, err := module.Service.Authenticate(
		request.Context(), request.Header.Get("Authorization"),
	)
	if err != nil {
		writeAgentError(response, request, err)
		return
	}
	segments := strings.Split(strings.Trim(strings.TrimPrefix(
		request.URL.Path, "/v1/projects/"), "/"), "/")
	if len(segments) < 2 || segments[0] == "" || segments[1] != "agent-instances" {
		writeAgentError(response, request, ErrNotFound)
		return
	}
	projectID := segments[0]
	rest := segments[2:]
	if len(rest) == 0 {
		module.handleInstances(response, request, identity, projectID)
		return
	}
	module.handleInstance(response, request, identity, projectID, rest)
}

func (module Module) handleInstances(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	switch request.Method {
	case http.MethodGet:
		items, err := module.Service.ListInstances(request.Context(), identity, projectID)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		views := make([]interface{}, 0, len(items))
		for _, item := range items {
			view, viewErr := module.instanceView(request, item)
			if viewErr != nil {
				writeAgentError(response, request, viewErr)
				return
			}
			views = append(views, view)
		}
		httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": views})
	case http.MethodPost:
		var body contract.CreateAgentInstanceRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		result, err := module.Service.CreateInstance(
			request.Context(), identity, projectID, CreateInstanceInput{
				APIKey:                 body.HermesAPIKey,
				AllowedTools:           body.AllowedTools,
				CloudflareClientID:     optional(body.CloudflareAccessClientID),
				CloudflareClientSecret: optional(body.CloudflareAccessClientSecret),
				DashboardToken:         optional(body.DashboardSessionToken),
				DashboardURL:           optional(body.ManagementURL),
				DisplayName:            body.DisplayName,
				ManagementMode:         body.ManagementMode,
				Profile:                optionalDefault(body.Profile, "default"),
				RequestTimeoutSeconds:  optionalIntValue(body.RequestTimeoutSeconds),
				RuntimeURL:             body.RuntimeURL,
			},
		)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		view, err := module.provisioningView(request, result)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusCreated, view)
	default:
		writeMethodNotAllowed(response, request)
	}
}

func (module Module) handleInstance(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	rest []string,
) {
	instanceID := rest[0]
	if instanceID == "" {
		writeAgentError(response, request, ErrNotFound)
		return
	}
	if len(rest) == 1 {
		module.handleInstanceResource(response, request, identity, projectID, instanceID)
		return
	}
	switch rest[1] {
	case "checks":
		if len(rest) == 2 {
			module.handleChecks(response, request, identity, projectID, instanceID)
			return
		}
	case "project-access":
		if len(rest) == 3 && rest[2] == "verify" {
			module.handleProjectAccessVerify(response, request, identity, projectID, instanceID)
			return
		}
	case "tokens":
		module.handleTokens(response, request, identity, projectID, instanceID, rest[2:])
		return
	case "prompt":
		module.handlePrompt(response, request, identity, projectID, instanceID, rest[2:])
		return
	case "sessions":
		module.handleSessions(response, request, identity, projectID, instanceID, rest[2:])
		return
	}
	writeAgentError(response, request, ErrNotFound)
}

func (module Module) handleInstanceResource(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	instanceID string,
) {
	switch request.Method {
	case http.MethodGet:
		item, err := module.Service.GetInstance(request.Context(), identity, projectID, instanceID)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		view, err := module.instanceView(request, item)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, view)
	case http.MethodPatch:
		var body contract.UpdateAgentInstanceRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		result, err := module.Service.UpdateInstance(
			request.Context(), identity, projectID, instanceID, UpdateInstanceInput{
				APIKey:                 body.HermesAPIKey,
				AllowedTools:           body.AllowedTools,
				CloudflareClientID:     body.CloudflareAccessClientID,
				CloudflareClientSecret: body.CloudflareAccessClientSecret,
				DashboardToken:         body.DashboardSessionToken,
				DashboardURL:           body.ManagementURL,
				DisplayName:            body.DisplayName,
				ManagementMode:         body.ManagementMode,
				Profile:                body.Profile,
				RequestTimeoutSeconds:  optionalInt(body.RequestTimeoutSeconds),
				RuntimeURL:             body.RuntimeURL,
			},
		)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		view, err := module.provisioningView(request, result)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, view)
	case http.MethodDelete:
		if err := module.Service.DisableInstance(
			request.Context(), identity, projectID, instanceID,
		); err != nil {
			writeAgentError(response, request, err)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(response, request)
	}
}

func (module Module) handleChecks(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	instanceID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.RunAgentChecksRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	item, err := module.Service.CheckConnections(
		request.Context(), identity, projectID, instanceID, body.Scope,
	)
	if err != nil {
		writeAgentError(response, request, err)
		return
	}
	view, err := module.instanceView(request, item)
	if err != nil {
		writeAgentError(response, request, err)
		return
	}
	checks := checksView(item, body.Scope)
	httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{
		"checked_at": latestCheckTime(item),
		"checks":     checks,
		"instance":   view,
		"status":     aggregateCheckStatus(checks),
	})
}

func (module Module) handleProjectAccessVerify(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	instanceID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	item, err := module.Service.CheckConnections(
		request.Context(), identity, projectID, instanceID, "project_access",
	)
	if err != nil {
		writeAgentError(response, request, err)
		return
	}
	view, err := module.instanceView(request, item)
	if err != nil {
		writeAgentError(response, request, err)
		return
	}
	check := checkView("project_access", item.ProjectAccessCheck)
	httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{
		"check":      check,
		"checked_at": item.ProjectAccessCheck.CheckedAt,
		"instance":   view,
		"verified":   item.ProjectAccessCheck.Status == "passed",
	})
}

func (module Module) handleTokens(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	instanceID string,
	rest []string,
) {
	if len(rest) == 1 && rest[0] == "rotate" {
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		var body contract.RotateAgentTokenRequest
		if !decodeOptionalJSON(response, request, &body) {
			return
		}
		result, err := module.Service.RotateToken(
			request.Context(), identity, projectID, instanceID,
			RotateTokenInput{ExpiresAt: body.ExpiresAt, Name: optional(body.Name)},
		)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		view, err := module.rotationView(request, result)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusCreated, view)
		return
	}
	if len(rest) == 2 && rest[0] != "" {
		tokenID := rest[0]
		switch rest[1] {
		case "verify":
			if !httpx.RequireMethod(response, request, http.MethodPost) {
				return
			}
			item, err := module.Service.VerifyToken(
				request.Context(), identity, projectID, instanceID, tokenID,
			)
			if err != nil {
				writeAgentError(response, request, err)
				return
			}
			token, err := module.Service.GetCredential(request.Context(), tokenID)
			if err != nil {
				writeAgentError(response, request, err)
				return
			}
			instance, err := module.instanceView(request, item)
			if err != nil {
				writeAgentError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{
				"credential":             credentialView(token),
				"instance":               instance,
				"old_credential_revoked": token.ReplacesTokenID != "",
				"verified":               true,
			})
			return
		case "abort":
			if !httpx.RequireMethod(response, request, http.MethodPost) {
				return
			}
			if err := module.Service.AbortToken(
				request.Context(), identity, projectID, instanceID, tokenID,
			); err != nil {
				writeAgentError(response, request, err)
				return
			}
			token, err := module.Service.GetCredential(request.Context(), tokenID)
			if err != nil {
				writeAgentError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{
				"credential":                    credentialView(token),
				"old_credential_remains_active": token.ReplacesTokenID != "",
			})
			return
		}
	}
	if len(rest) == 1 && rest[0] != "" && request.Method == http.MethodDelete {
		if err := module.Service.RevokeToken(
			request.Context(), identity, projectID, instanceID, rest[0],
		); err != nil {
			writeAgentError(response, request, err)
			return
		}
		response.WriteHeader(http.StatusNoContent)
		return
	}
	writeAgentError(response, request, ErrNotFound)
}

func (module Module) handlePrompt(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	instanceID string,
	rest []string,
) {
	if len(rest) == 1 && rest[0] == "reset" {
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		item, err := module.Service.ResetPrompt(request.Context(), identity, projectID, instanceID)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, promptView(projectID, instanceID, item))
		return
	}
	if len(rest) != 0 {
		writeAgentError(response, request, ErrNotFound)
		return
	}
	switch request.Method {
	case http.MethodGet:
		item, err := module.Service.GetPrompt(request.Context(), identity, projectID, instanceID)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, promptView(projectID, instanceID, item))
	case http.MethodPatch:
		var body contract.UpdateAgentPromptRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		item, err := module.Service.UpdatePrompt(
			request.Context(), identity, projectID, instanceID, body.Content,
		)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, promptView(projectID, instanceID, item))
	default:
		writeMethodNotAllowed(response, request)
	}
}

func (module Module) handleSessions(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	instanceID string,
	rest []string,
) {
	if len(rest) == 0 {
		switch request.Method {
		case http.MethodGet:
			items, err := module.Service.ListSessions(request.Context(), identity, projectID, instanceID)
			if err != nil {
				writeAgentError(response, request, err)
				return
			}
			instance, err := module.Service.GetInstance(request.Context(), identity, projectID, instanceID)
			if err != nil {
				writeAgentError(response, request, err)
				return
			}
			views := make([]interface{}, 0, len(items))
			for _, item := range items {
				views = append(views, sessionView(item, instance.Grant.DefaultSessionID))
			}
			httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": views})
		case http.MethodPost:
			var body contract.CreateAgentSessionRequest
			if !httpx.DecodeJSON(response, request, &body) {
				return
			}
			item, err := module.Service.CreateSession(
				request.Context(), identity, projectID, instanceID,
				CreateSessionInput{SessionType: body.SessionType, Title: body.Title},
			)
			if err != nil {
				writeAgentError(response, request, err)
				return
			}
			makeDefault := body.Default != nil && *body.Default
			if makeDefault {
				if err := module.Service.SetDefaultSession(
					request.Context(), identity, projectID, instanceID, item.ID,
				); err != nil {
					writeAgentError(response, request, err)
					return
				}
			}
			httpx.WriteJSON(response, http.StatusCreated, sessionView(item, defaultID(makeDefault, item.ID)))
		default:
			writeMethodNotAllowed(response, request)
		}
		return
	}
	sessionID := rest[0]
	if len(rest) == 1 {
		switch request.Method {
		case http.MethodGet:
			item, err := module.Service.GetSession(request.Context(), identity, projectID, instanceID, sessionID)
			if err != nil {
				writeAgentError(response, request, err)
				return
			}
			instance, err := module.Service.GetInstance(request.Context(), identity, projectID, instanceID)
			if err != nil {
				writeAgentError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusOK, sessionView(item, instance.Grant.DefaultSessionID))
		case http.MethodPatch:
			var body contract.UpdateAgentSessionRequest
			if !httpx.DecodeJSON(response, request, &body) {
				return
			}
			item, err := module.Service.RenameSession(
				request.Context(), identity, projectID, instanceID, sessionID, body.Title,
			)
			if err != nil {
				writeAgentError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusOK, sessionView(item, ""))
		default:
			writeMethodNotAllowed(response, request)
		}
		return
	}
	switch rest[1] {
	case "end":
		if len(rest) != 2 {
			break
		}
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		var body contract.EndAgentSessionRequest
		if !decodeOptionalJSON(response, request, &body) {
			return
		}
		item, err := module.Service.EndSession(
			request.Context(), identity, projectID, instanceID, sessionID, optional(body.Reason),
		)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, sessionView(item, ""))
		return
	case "continue":
		if len(rest) != 2 {
			break
		}
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		item, err := module.Service.ContinueSession(request.Context(), identity, projectID, instanceID, sessionID)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, sessionView(item, ""))
		return
	case "fork":
		if len(rest) != 2 {
			break
		}
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		var body contract.ForkAgentSessionRequest
		if !decodeOptionalJSON(response, request, &body) {
			return
		}
		item, err := module.Service.ForkSession(
			request.Context(), identity, projectID, instanceID, sessionID, optional(body.Title),
		)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		makeDefault := body.Default != nil && *body.Default
		if makeDefault {
			if err := module.Service.SetDefaultSession(request.Context(), identity, projectID, instanceID, item.ID); err != nil {
				writeAgentError(response, request, err)
				return
			}
		}
		httpx.WriteJSON(response, http.StatusCreated, sessionView(item, defaultID(makeDefault, item.ID)))
		return
	case "default":
		if len(rest) != 2 {
			break
		}
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		if err := module.Service.SetDefaultSession(request.Context(), identity, projectID, instanceID, sessionID); err != nil {
			writeAgentError(response, request, err)
			return
		}
		item, err := module.Service.GetSession(request.Context(), identity, projectID, instanceID, sessionID)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, sessionView(item, sessionID))
		return
	case "messages":
		if len(rest) != 2 {
			break
		}
		if !httpx.RequireMethod(response, request, http.MethodGet) {
			return
		}
		items, err := module.Service.ListMessages(request.Context(), identity, projectID, instanceID, sessionID)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		views := make([]interface{}, 0, len(items))
		for _, item := range items {
			views = append(views, messageView(item))
		}
		httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": views})
		return
	case "runs":
		module.handleRuns(response, request, identity, projectID, instanceID, sessionID, rest[2:])
		return
	}
	writeAgentError(response, request, ErrNotFound)
}

func (module Module) handleRuns(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
	rest []string,
) {
	if len(rest) == 0 {
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		var body contract.StartAgentRunRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		run, err := module.Service.StartRun(
			request.Context(), identity, projectID, instanceID, sessionID,
			StartRunInput{Input: body.Message},
		)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		session, err := module.Service.GetSession(request.Context(), identity, projectID, instanceID, sessionID)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusAccepted, map[string]interface{}{
			"run": runView(run), "session": sessionView(session, ""),
		})
		return
	}
	runID := rest[0]
	if len(rest) == 1 {
		if !httpx.RequireMethod(response, request, http.MethodGet) {
			return
		}
		item, err := module.Service.GetRun(request.Context(), identity, projectID, instanceID, sessionID, runID)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, runView(item))
		return
	}
	switch rest[1] {
	case "events":
		if len(rest) != 2 {
			break
		}
		module.streamRun(response, request, identity, projectID, instanceID, sessionID, runID)
		return
	case "stop":
		if len(rest) != 2 {
			break
		}
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		item, err := module.Service.StopRun(request.Context(), identity, projectID, instanceID, sessionID, runID)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, runView(item))
		return
	case "regenerate", "rerun":
		if len(rest) != 2 {
			break
		}
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		var body contract.ReplayAgentRunRequest
		if !decodeOptionalJSON(response, request, &body) {
			return
		}
		result, err := module.Service.ReplayRun(
			request.Context(), identity, projectID, instanceID, sessionID, runID,
			optional(body.MessageID), rest[1] == "regenerate",
		)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusAccepted, map[string]interface{}{
			"run": runView(result.Run), "session": sessionView(result.Session, ""),
		})
		return
	case "approvals":
		if len(rest) != 3 || rest[2] == "" {
			break
		}
		if !httpx.RequireMethod(response, request, http.MethodPost) {
			return
		}
		var body contract.RespondAgentRunApprovalRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		item, err := module.Service.ApproveRun(
			request.Context(), identity, projectID, instanceID, sessionID,
			runID, rest[2], ApprovalChoice(body.Choice),
		)
		if err != nil {
			writeAgentError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, runView(item))
		return
	}
	writeAgentError(response, request, ErrNotFound)
}

func (module Module) streamRun(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	instanceID string,
	sessionID string,
	runID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	if _, err := module.Service.GetRun(
		request.Context(), identity, projectID, instanceID, sessionID, runID,
	); err != nil {
		writeAgentError(response, request, err)
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeAgentError(response, request, ErrRuntime)
		return
	}
	response.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	response.Header().Set("Cache-Control", "no-cache, no-transform")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)
	flusher.Flush()
	sequence := int64(0)
	err := module.Service.StreamRun(
		request.Context(), identity, projectID, instanceID, sessionID, runID,
		request.Header.Get("Last-Event-ID"),
		func(eventContext context.Context, event Event) error {
			sequence++
			if event.Sequence > sequence {
				sequence = event.Sequence
			}
			eventID := event.ID
			if eventID == "" {
				eventID = fmt.Sprintf("%s:%d", runID, sequence)
			}
			occurredAt := time.Now().UTC()
			if event.Timestamp != nil {
				occurredAt = event.Timestamp.UTC()
			}
			eventType := string(event.Type)
			if eventType == "" {
				eventType = string(EventHeartbeat)
			}
			payload := map[string]interface{}{
				"event":       eventType,
				"event_id":    eventID,
				"occurred_at": occurredAt,
				"run_id":      runID,
				"sequence":    sequence,
				"session_id":  sessionID,
			}
			if event.MessageRemoteID != "" {
				payload["message_id"] = event.MessageRemoteID
			}
			if event.Text != "" {
				payload["delta"] = event.Text
			}
			if event.Tool != nil {
				payload["tool_call"] = adapterToolCallView(*event.Tool)
			}
			if event.Approval != nil {
				approvalID := event.Approval.RemoteID
				if approvalID == "" {
					approvalID = eventID
				}
				choices := make([]string, 0, len(event.Approval.Choices))
				for _, choice := range event.Approval.Choices {
					choices = append(choices, string(choice))
				}
				payload["approval"] = map[string]interface{}{
					"approval_id": approvalID,
					"choices":     choices,
				}
			}
			if event.Error != nil {
				payload["safe_error_code"] = event.Error.Code
				payload["safe_error_message"] = event.Error.Message
			}
			switch event.Type {
			case EventRunStarted, EventRunCompleted, EventRunFailed,
				EventRunCancelled, EventApprovalRequested, EventApprovalResponded:
				if current, getErr := module.Service.Store.GetRun(eventContext, sessionID, runID); getErr == nil {
					payload["run"] = runView(current)
				}
			}
			if err := encodeSSE(response, eventID, eventType, payload); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		},
	)
	if err != nil && request.Context().Err() == nil {
		sequence++
		eventID := fmt.Sprintf("%s:%d", runID, sequence)
		_ = encodeSSE(response, eventID, string(EventError), map[string]interface{}{
			"event":              string(EventError),
			"event_id":           eventID,
			"occurred_at":        time.Now().UTC(),
			"run_id":             runID,
			"safe_error_code":    "runtime_stream_failed",
			"safe_error_message": "The Agent event stream ended unexpectedly",
			"sequence":           sequence,
			"session_id":         sessionID,
		})
		flusher.Flush()
	}
}

func (module Module) instanceView(request *http.Request, item Instance) (map[string]interface{}, error) {
	credentials, err := module.Service.ListCredentials(request.Context(), item.Grant.GrantID)
	if err != nil {
		return nil, err
	}
	credentialViews := make([]interface{}, 0, len(credentials))
	for _, credential := range credentials {
		credentialViews = append(credentialViews, credentialView(credential))
	}
	path := item.ManagementPath
	if path == "" {
		path = "unreachable"
	}
	view := map[string]interface{}{
		"adapter_type":            item.AdapterType,
		"agent_instance_id":       item.ID,
		"capabilities":            capabilitiesView(item.Capabilities),
		"created_at":              item.CreatedAt,
		"created_by":              item.CreatedBy,
		"credentials":             credentialViews,
		"display_name":            item.DisplayName,
		"grant":                   grantView(*item.Grant, item),
		"management_mode":         item.ManagementMode,
		"management_path":         path,
		"project_id":              item.Grant.ProjectID,
		"request_timeout_seconds": requestTimeoutView(item.RequestTimeoutSeconds),
		"runtime_url":             item.RuntimeURL,
		"secrets": map[string]interface{}{
			"cloudflare_access_configured":       item.SecretStatus.CloudflareClientIDConfigured && item.SecretStatus.CloudflareClientSecretConfigured,
			"dashboard_session_token_configured": item.SecretStatus.DashboardTokenConfigured,
			"hermes_api_key_configured":          item.SecretStatus.HermesAPIKeyConfigured,
		},
		"status":     item.Status,
		"updated_at": item.UpdatedAt,
		"version":    item.Version,
	}
	if item.Profile != "" {
		view["profile"] = item.Profile
	}
	if item.DashboardURL != "" {
		view["management_url"] = item.DashboardURL
	}
	if !item.RuntimeCheck.CheckedAt.IsZero() {
		view["runtime_check"] = checkView("runtime", item.RuntimeCheck)
	}
	if !item.ManagementCheck.CheckedAt.IsZero() {
		view["management_check"] = checkView("management", item.ManagementCheck)
	}
	if !item.ProjectAccessCheck.CheckedAt.IsZero() {
		view["project_access_check"] = checkView("project_access", item.ProjectAccessCheck)
	}
	if item.DisabledAt != nil {
		view["disabled_at"] = item.DisabledAt
	}
	return view, nil
}

func (module Module) provisioningView(request *http.Request, result InstanceResult) (map[string]interface{}, error) {
	instance, err := module.instanceView(request, result.Instance)
	if err != nil {
		return nil, err
	}
	view := map[string]interface{}{"instance": instance}
	if result.OneTimeToken != nil {
		credential, err := module.Service.GetCredential(request.Context(), result.OneTimeToken.TokenID)
		if err != nil {
			return nil, err
		}
		view["one_time_credential"] = oneTimeCredentialView(*result.OneTimeToken, credential)
	}
	return view, nil
}

func (module Module) rotationView(request *http.Request, result InstanceResult) (map[string]interface{}, error) {
	if result.Rotation == nil {
		return nil, ErrConflict
	}
	credential, err := module.Service.GetCredential(request.Context(), result.Rotation.NewTokenID)
	if err != nil {
		return nil, err
	}
	view := map[string]interface{}{
		"credential":                    credentialView(credential),
		"old_credential_remains_active": result.Rotation.OldTokenID != "" && result.Rotation.Status != "completed",
		"rotation_status":               result.Rotation.Status,
	}
	if result.Rotation.SafeErrorCode != "" {
		view["safe_error_code"] = result.Rotation.SafeErrorCode
	}
	if result.OneTimeToken != nil {
		view["one_time_credential"] = oneTimeCredentialView(*result.OneTimeToken, credential)
	}
	return view, nil
}

func credentialView(item auth.AgentToken) map[string]interface{} {
	view := map[string]interface{}{
		"agent_instance_id": item.AgentInstanceID,
		"allowed_tools":     nonNilStrings(item.AllowedTools),
		"created_at":        item.CreatedAt,
		"grant_id":          item.GrantID,
		"id":                item.ID,
		"name":              item.Name,
		"project_id":        item.ProjectID,
		"status":            item.Status,
	}
	if item.ReplacesTokenID != "" {
		view["replaces_token_id"] = item.ReplacesTokenID
	}
	if item.ExpiresAt != nil {
		view["expires_at"] = item.ExpiresAt
	}
	if item.ActivatedAt != nil {
		view["activated_at"] = item.ActivatedAt
	}
	if item.LastUsedAt != nil {
		view["last_used_at"] = item.LastUsedAt
	}
	if item.RevokedAt != nil {
		view["revoked_at"] = item.RevokedAt
	}
	return view
}

func oneTimeCredentialView(material OneTimeTokenMaterial, credential auth.AgentToken) map[string]interface{} {
	return map[string]interface{}{
		"credential":   credentialView(credential),
		"mcp_endpoint": material.GatewayURL,
		"server_name":  "mmdash-project",
		"token":        material.Token,
	}
}

func grantView(item ProjectGrant, instance Instance) map[string]interface{} {
	view := map[string]interface{}{
		"agent_instance_id":     item.AgentInstanceID,
		"allowed_tools":         nonNilStrings(item.AllowedTools),
		"created_at":            item.CreatedAt,
		"grant_id":              item.GrantID,
		"project_access_status": projectAccessStatus(instance),
		"project_id":            item.ProjectID,
		"status":                item.Status,
		"updated_at":            item.UpdatedAt,
		"version":               item.Version,
	}
	if item.DefaultSessionID != "" {
		view["default_session_id"] = item.DefaultSessionID
	}
	if item.LastAccessAt != nil {
		view["last_access_at"] = item.LastAccessAt
	}
	return view
}

func projectAccessStatus(item Instance) string {
	if item.Status == InstanceDisabled || item.Grant.Status == "revoked" {
		return "revoked"
	}
	if item.ProjectAccessCheck.Code == "rotation_failed" {
		return "rotation_failed"
	}
	switch item.ProjectAccessCheck.Status {
	case "passed":
		return "verified"
	case "failed":
		return "failed"
	default:
		return "pending"
	}
}

func capabilitiesView(values map[string]interface{}) map[string]interface{} {
	projectAccess := mapValue(values["project_access"])
	return map[string]interface{}{
		"jobs":            boolValue(values["jobs"]),
		"message_history": boolValue(values["sessions"]),
		"project_access": map[string]interface{}{
			"configure": boolValue(projectAccess["configure"]),
			"rotate":    boolValue(projectAccess["rotate"]),
			"verify":    boolValue(projectAccess["verify"]),
		},
		"run_approval":        boolValue(values["run_approval"]),
		"run_events":          boolValue(values["run_streaming"]),
		"run_status":          boolValue(values["runs"]),
		"run_stop":            boolValue(values["run_stop"]),
		"runs":                boolValue(values["runs"]),
		"session_chat_stream": boolValue(values["session_streaming"]) || boolValue(values["session_chat"]),
		"session_fork":        boolValue(values["session_fork"]),
		"sessions":            boolValue(values["sessions"]),
	}
}

func checkView(kind string, item CheckSnapshot) map[string]interface{} {
	status := item.Status
	if status == "" {
		status = "not_configured"
	}
	return map[string]interface{}{
		"checked_at": item.CheckedAt,
		"code":       item.Code,
		"kind":       kind,
		"status":     status,
	}
}

func checksView(item Instance, scope string) []interface{} {
	checks := []interface{}{}
	if scope == "runtime" || scope == "all" {
		runtime := item.RuntimeCheck
		checks = append(checks,
			checkView("runtime", runtime),
			checkView("authentication", runtime),
			capabilityCheck("capabilities", runtime, true),
			capabilityCheck("sessions", runtime, boolFromCapabilities(item.Capabilities, "sessions")),
			capabilityCheck("messages", runtime, boolFromCapabilities(item.Capabilities, "sessions")),
			capabilityCheck("sse", runtime, boolFromCapabilities(item.Capabilities, "run_streaming")),
			capabilityCheck("runs", runtime, boolFromCapabilities(item.Capabilities, "runs")),
			capabilityCheck("jobs", runtime, boolFromCapabilities(item.Capabilities, "jobs")),
		)
	}
	if scope == "management" || scope == "all" {
		checks = append(checks, checkView("management", item.ManagementCheck))
	}
	if scope == "project_access" || scope == "all" {
		checks = append(checks, checkView("project_access", item.ProjectAccessCheck))
	}
	return checks
}

func capabilityCheck(kind string, runtime CheckSnapshot, supported bool) map[string]interface{} {
	check := runtime
	if check.Status == "passed" && !supported {
		check.Status = "unsupported"
		check.Code = "capability_unsupported"
	}
	return checkView(kind, check)
}

func aggregateCheckStatus(checks []interface{}) string {
	passed, failed := false, false
	for _, raw := range checks {
		item, _ := raw.(map[string]interface{})
		switch item["status"] {
		case "passed":
			passed = true
		case "failed", "not_configured":
			failed = true
		}
	}
	if failed && passed {
		return "partial"
	}
	if failed {
		return "failed"
	}
	return "passed"
}

func latestCheckTime(item Instance) time.Time {
	latest := item.RuntimeCheck.CheckedAt
	for _, candidate := range []time.Time{
		item.ManagementCheck.CheckedAt, item.ProjectAccessCheck.CheckedAt,
	} {
		if candidate.After(latest) {
			latest = candidate
		}
	}
	return latest
}

func promptView(projectID, instanceID string, item Prompt) map[string]interface{} {
	view := map[string]interface{}{
		"agent_instance_id": instanceID,
		"custom":            item.Override != "",
		"custom_prompt":     item.Override,
		"default_prompt":    item.Default,
		"effective_prompt":  item.Effective,
		"project_id":        projectID,
		"version":           item.Version,
	}
	if !item.UpdatedAt.IsZero() {
		view["updated_at"] = item.UpdatedAt
	}
	return view
}

func sessionView(item SessionRecord, defaultSessionID string) map[string]interface{} {
	view := map[string]interface{}{
		"agent_instance_id": item.AgentInstanceID,
		"created_at":        item.CreatedAt,
		"default":           item.ID == defaultSessionID,
		"project_id":        item.ProjectID,
		"remote_session_id": item.RemoteSessionID,
		"session_id":        item.ID,
		"session_type":      item.SessionType,
		"status":            item.Status,
		"title":             item.Title,
		"updated_at":        item.UpdatedAt,
		"version":           item.Version,
	}
	if item.ParentSessionID != "" {
		view["parent_session_id"] = item.ParentSessionID
	}
	if item.EndReason != "" {
		view["end_reason"] = item.EndReason
	}
	if item.LastMessageAt != nil {
		view["last_message_at"] = item.LastMessageAt
	}
	if item.LastRunAt != nil {
		view["last_run_at"] = item.LastRunAt
	}
	if item.EndedAt != nil {
		view["ended_at"] = item.EndedAt
	}
	return view
}

func messageView(item Message) map[string]interface{} {
	view := map[string]interface{}{
		"content":    item.Content,
		"message_id": item.RemoteID,
		"role":       safeMessageRole(item.Role),
	}
	if item.ToolCallID != "" {
		view["tool_call_id"] = item.ToolCallID
	}
	if len(item.ToolCalls) > 0 {
		calls := make([]interface{}, 0, len(item.ToolCalls))
		for _, call := range item.ToolCalls {
			calls = append(calls, adapterToolCallView(call))
		}
		view["tool_calls"] = calls
	}
	if item.Timestamp != nil {
		view["created_at"] = item.Timestamp
	}
	return view
}

func runView(item RunRecord) map[string]interface{} {
	calls := make([]interface{}, 0, len(item.ToolCalls))
	for _, call := range item.ToolCalls {
		calls = append(calls, toolCallRecordView(call))
	}
	view := map[string]interface{}{
		"created_at":    item.CreatedAt,
		"remote_run_id": item.RemoteRunID,
		"run_id":        item.ID,
		"session_id":    item.SessionID,
		"source":        item.Source,
		"status":        item.Status,
		"tool_calls":    calls,
		"updated_at":    item.UpdatedAt,
		"version":       item.Version,
	}
	if item.SourceRunID != "" {
		view["source_run_id"] = item.SourceRunID
	}
	if item.SafeErrorCode != "" {
		view["safe_error_code"] = item.SafeErrorCode
	}
	if item.StartedAt != nil {
		view["started_at"] = item.StartedAt
	}
	if item.CompletedAt != nil {
		view["completed_at"] = item.CompletedAt
	}
	return view
}

func toolCallRecordView(item ToolCallRecord) map[string]interface{} {
	view := map[string]interface{}{
		"name":         item.ToolName,
		"status":       safeToolStatus(item.Status),
		"tool_call_id": item.ID,
	}
	if item.SafePreview != "" {
		view["input_summary"] = item.SafePreview
	}
	if !item.StartedAt.IsZero() {
		view["started_at"] = item.StartedAt
	}
	if item.CompletedAt != nil {
		view["completed_at"] = item.CompletedAt
	}
	return view
}

func adapterToolCallView(item ToolCall) map[string]interface{} {
	return map[string]interface{}{
		"name":         item.Name,
		"status":       safeToolStatus(item.Status),
		"tool_call_id": item.RemoteID,
	}
}

func safeMessageRole(value string) string {
	switch value {
	case "user", "assistant", "tool", "system":
		return value
	default:
		return "assistant"
	}
}

func safeToolStatus(value string) string {
	switch value {
	case "queued", "running", "completed", "failed":
		return value
	case "started":
		return "running"
	default:
		return "running"
	}
}

func mapValue(value interface{}) map[string]interface{} {
	result, _ := value.(map[string]interface{})
	if result == nil {
		return map[string]interface{}{}
	}
	return result
}

func boolValue(value interface{}) bool {
	result, _ := value.(bool)
	return result
}

func boolFromCapabilities(values map[string]interface{}, key string) bool {
	return boolValue(values[key])
}

func optional(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalDefault(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return *value
}

func optionalInt(value *int64) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}

func optionalIntValue(value *int64) int {
	converted := optionalInt(value)
	if converted == nil {
		return 0
	}
	return *converted
}

func requestTimeoutView(value int) int {
	if value < 1 || value > 300 {
		return 30
	}
	return value
}

func defaultID(makeDefault bool, sessionID string) string {
	if makeDefault {
		return sessionID
	}
	return ""
}

func decodeOptionalJSON(response http.ResponseWriter, request *http.Request, target interface{}) bool {
	if request.Body == nil || request.Body == http.NoBody || request.ContentLength == 0 {
		if validator, ok := target.(httpx.Validator); ok && validator.Validate() != nil {
			writeAgentError(response, request, ErrInvalid)
			return false
		}
		return true
	}
	return httpx.DecodeJSON(response, request, target)
}

func writeAgentError(response http.ResponseWriter, request *http.Request, err error) {
	var applicationError *apperror.Error
	if errors.As(err, &applicationError) {
		httpx.WriteError(response, request, applicationError)
		return
	}
	status, code, message := http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred"
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		status, code, message = http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required"
	case errors.Is(err, ErrForbidden), errors.Is(err, auth.ErrForbidden),
		errors.Is(err, project.ErrForbidden), errors.Is(err, settings.ErrForbidden):
		status, code, message = http.StatusForbidden, "FORBIDDEN", "Agent permission denied"
	case errors.Is(err, ErrInvalid), errors.Is(err, auth.ErrInvalid),
		errors.Is(err, settings.ErrInvalid):
		status, code, message = http.StatusBadRequest, "INVALID_AGENT_REQUEST", "Agent input is invalid"
	case errors.Is(err, ErrNotFound), errors.Is(err, auth.ErrNotFound),
		errors.Is(err, settings.ErrNotFound):
		status, code, message = http.StatusNotFound, "AGENT_NOT_FOUND", "Agent resource not found"
	case errors.Is(err, ErrConflict), errors.Is(err, auth.ErrConflict):
		status, code, message = http.StatusConflict, "AGENT_CONFLICT", "Agent state changed or the operation cannot proceed"
	case errors.Is(err, ErrNotConfigured):
		status, code, message = http.StatusConflict, "AGENT_NOT_CONFIGURED", "Agent configuration is incomplete"
	case errors.Is(err, ErrRuntime):
		status, code, message = http.StatusBadGateway, "AGENT_RUNTIME_UNAVAILABLE", "The Agent runtime is unavailable"
	}
	httpx.WriteError(response, request, apperror.New(status, code, message))
}

func writeMethodNotAllowed(response http.ResponseWriter, request *http.Request) {
	writeAgentError(response, request, apperror.New(
		http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed",
	))
}

func encodeSSE(response http.ResponseWriter, eventID, eventType string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if eventID != "" {
		if _, err := fmt.Fprintf(response, "id: %s\n", strings.ReplaceAll(eventID, "\n", "")); err != nil {
			return err
		}
	}
	if eventType != "" {
		if _, err := fmt.Fprintf(response, "event: %s\n", strings.ReplaceAll(eventType, "\n", "")); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(response, "data: %s\n\n", data)
	return err
}
