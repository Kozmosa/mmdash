package progress

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
	"github.com/mmdash/mmdash/backend/internal/project"
)

type Module struct{ Service Service }

func (Module) Name() string { return "progress" }

func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/progress/", module.handleStandalone)
}

func (module Module) ProjectHandler() http.Handler { return http.HandlerFunc(module.handleProject) }

func (module Module) handleStandalone(response http.ResponseWriter, request *http.Request) {
	identity, err := module.Service.Authenticate(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	segments := strings.Split(strings.Trim(strings.TrimPrefix(request.URL.Path, "/v1/progress/"), "/"), "/")
	if len(segments) < 1 || segments[0] == "" {
		writeError(response, request, ErrNotFound)
		return
	}
	module.dispatch(response, request, identity, segments[0], segments[1:])
}

func (module Module) handleProject(response http.ResponseWriter, request *http.Request) {
	identity, err := module.Service.Authenticate(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	segments := strings.Split(strings.Trim(strings.TrimPrefix(request.URL.Path, "/v1/projects/"), "/"), "/")
	if len(segments) < 2 || segments[0] == "" || segments[1] != "progress" {
		writeError(response, request, ErrNotFound)
		return
	}
	module.dispatch(response, request, identity, segments[0], segments[2:])
}

func (module Module) dispatch(response http.ResponseWriter, request *http.Request, identity auth.Identity, projectID string, segments []string) {
	if len(segments) == 0 {
		module.handleAggregate(response, request, identity, projectID)
		return
	}
	switch segments[0] {
	case "milestones":
		module.handleMilestones(response, request, identity, projectID, segments[1:])
	case "tasks":
		module.handleTasks(response, request, identity, projectID, segments[1:])
	case "dependencies":
		module.handleDependencies(response, request, identity, projectID, segments[1:])
	case "reminders":
		module.handleReminders(response, request, identity, projectID, segments[1:])
	case "proposals":
		module.handleProposals(response, request, identity, projectID, segments[1:])
	case "settings":
		module.handleSettings(response, request, identity, projectID)
	default:
		writeError(response, request, ErrNotFound)
	}
}

func (module Module) handleAggregate(w http.ResponseWriter, r *http.Request, identity auth.Identity, projectID string) {
	if !httpx.RequireMethod(w, r, http.MethodGet) {
		return
	}
	value, err := module.Service.List(r.Context(), identity, projectID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func (module Module) handleMilestones(w http.ResponseWriter, r *http.Request, identity auth.Identity, projectID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		items, err := module.Service.ListMilestones(r.Context(), identity, projectID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
		return
	}
	if len(rest) == 0 && r.Method == http.MethodPost {
		var body contract.CreateMilestoneRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		item, err := module.Service.CreateMilestone(r.Context(), identity, projectID, CreateMilestoneInput{Title: body.Title, Description: stringValue(body.Description), Critical: boolValue(body.Critical), StartAt: body.StartAt, TargetAt: body.TargetAt})
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, item)
		return
	}
	if len(rest) == 1 && r.Method == http.MethodPatch {
		var body contract.UpdateMilestoneRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		input := UpdateMilestoneInput{Title: body.Title, Description: body.Description, Status: body.Status, Critical: body.Critical}
		if body.StartAt != nil {
			value := body.StartAt
			input.StartAt = &value
		}
		if body.TargetAt != nil {
			value := body.TargetAt
			input.TargetAt = &value
		}
		item, err := module.Service.UpdateMilestone(r.Context(), identity, projectID, rest[0], input)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
		return
	}
	writeError(w, r, ErrNotFound)
}

func (module Module) handleTasks(w http.ResponseWriter, r *http.Request, identity auth.Identity, projectID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		items, err := module.Service.ListTasks(r.Context(), identity, projectID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
		return
	}
	if len(rest) == 0 && r.Method == http.MethodPost {
		var body contract.CreateTaskRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		item, err := module.Service.CreateTask(r.Context(), identity, projectID, CreateTaskInput{MilestoneID: stringValue(body.MilestoneID), Title: body.Title, Description: stringValue(body.Description), Status: stringValue(body.Status), AssigneeID: stringValue(body.AssigneeID), StartAt: body.StartAt, DueAt: body.DueAt, RelatedObjectIDs: sliceValue(body.RelatedObjectIDs), SourceRunID: stringValue(body.SourceRunID)})
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, item)
		return
	}
	if len(rest) == 1 && r.Method == http.MethodPatch {
		var body contract.UpdateTaskRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		input := UpdateTaskInput{MilestoneID: body.MilestoneID, Title: body.Title, Description: body.Description, Status: body.Status, AssigneeID: body.AssigneeID, RelatedObjectIDs: body.RelatedObjectIDs, SourceRunID: body.SourceRunID}
		if body.StartAt != nil {
			value := body.StartAt
			input.StartAt = &value
		}
		if body.DueAt != nil {
			value := body.DueAt
			input.DueAt = &value
		}
		item, err := module.Service.UpdateTask(r.Context(), identity, projectID, rest[0], input)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
		return
	}
	if len(rest) == 1 && r.Method == http.MethodDelete {
		if err := module.Service.DeleteTask(r.Context(), identity, projectID, rest[0]); err != nil {
			writeError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeError(w, r, ErrNotFound)
}

func (module Module) handleDependencies(w http.ResponseWriter, r *http.Request, identity auth.Identity, projectID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		items, err := module.Service.ListDependencies(r.Context(), identity, projectID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
		return
	}
	if len(rest) == 0 && r.Method == http.MethodPost {
		var body contract.CreateDependencyRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		item, err := module.Service.CreateDependency(r.Context(), identity, projectID, CreateDependencyInput{TaskID: body.TaskID, DependsOnTaskID: body.DependsOnTaskID, Kind: stringValue(body.Kind)})
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, item)
		return
	}
	if len(rest) == 1 && r.Method == http.MethodDelete {
		if err := module.Service.DeleteDependency(r.Context(), identity, projectID, rest[0]); err != nil {
			writeError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeError(w, r, ErrNotFound)
}

func (module Module) handleReminders(w http.ResponseWriter, r *http.Request, identity auth.Identity, projectID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		items, err := module.Service.ListReminders(r.Context(), identity, projectID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
		return
	}
	if len(rest) == 0 && r.Method == http.MethodPost {
		var body contract.CreateReminderRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		item, err := module.Service.CreateReminder(r.Context(), identity, projectID, CreateReminderInput{TaskID: stringValue(body.TaskID), MilestoneID: stringValue(body.MilestoneID), RemindAt: body.RemindAt, Note: stringValue(body.Note)})
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, item)
		return
	}
	if len(rest) == 2 && rest[1] == "trigger" && r.Method == http.MethodPost {
		item, err := module.Service.TriggerReminder(r.Context(), identity, projectID, rest[0])
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
		return
	}
	writeError(w, r, ErrNotFound)
}

func (module Module) handleProposals(w http.ResponseWriter, r *http.Request, identity auth.Identity, projectID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		items, err := module.Service.ListProposals(r.Context(), identity, projectID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
		return
	}
	if len(rest) == 0 && r.Method == http.MethodPost {
		var body contract.CreateProgressProposalRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		item, err := module.Service.CreateProposal(r.Context(), identity, projectID, CreateProposalInput{ProposalType: body.ProposalType, TargetID: stringValue(body.TargetID), Title: body.Title, Rationale: stringValue(body.Rationale), Changes: body.Changes, SourceRunID: stringValue(body.SourceRunID)})
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, item)
		return
	}
	if len(rest) == 2 && rest[1] == "review" && r.Method == http.MethodPost {
		var body contract.ReviewProgressProposalRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		item, err := module.Service.ReviewProposal(r.Context(), identity, projectID, rest[0], ReviewProposalInput{Decision: body.Decision, Note: stringValue(body.Note)})
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
		return
	}
	writeError(w, r, ErrNotFound)
}

func (module Module) handleSettings(w http.ResponseWriter, r *http.Request, identity auth.Identity, projectID string) {
	switch r.Method {
	case http.MethodGet:
		item, err := module.Service.GetSettings(r.Context(), identity, projectID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	case http.MethodPatch:
		var body contract.UpdateProgressSettingsRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		item, err := module.Service.UpdateSettings(r.Context(), identity, projectID, body.AutoTaskChanges)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	default:
		writeError(w, r, apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed"))
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func boolValue(value *bool) bool { return value != nil && *value }
func sliceValue(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		httpx.WriteError(w, r, appErr)
		return
	}
	status, code, message := http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred"
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		status, code, message = http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required"
	case errors.Is(err, ErrHumanRequired):
		status, code, message = http.StatusForbidden, "HUMAN_REVIEW_REQUIRED", "A human browser session must confirm this Progress change"
	case errors.Is(err, ErrProposalRequired):
		status, code, message = http.StatusConflict, "PROGRESS_PROPOSAL_REQUIRED", "This automatic change must be submitted as a Progress Proposal"
	case errors.Is(err, ErrInvalid):
		status, code, message = http.StatusBadRequest, "INVALID_PROGRESS_REQUEST", "Progress input is invalid"
	case errors.Is(err, ErrReferenceInvalid):
		status, code, message = http.StatusBadRequest, "PROGRESS_REFERENCE_INVALID", "Progress reference is invalid"
	case errors.Is(err, ErrConflict):
		status, code, message = http.StatusConflict, "PROGRESS_CONFLICT", "The Progress record has changed or is already resolved"
	case errors.Is(err, ErrNotFound):
		status, code, message = http.StatusNotFound, "PROGRESS_NOT_FOUND", "Progress record not found"
	case errors.Is(err, project.ErrForbidden):
		status, code, message = http.StatusForbidden, "FORBIDDEN", "Project permission denied"
	}
	httpx.WriteError(w, r, apperror.New(status, code, message))
}
