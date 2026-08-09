package model

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

// Module exposes Model project resources and the Job-bound Worker export.
type Module struct{ Service Service }

func (Module) Name() string { return "model" }

func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/internal/model-notion-jobs/", module.handleWorkerExport)
}

func (module Module) ProjectHandler() http.Handler {
	return http.HandlerFunc(module.handleProject)
}

func (module Module) handleProject(w http.ResponseWriter, r *http.Request) {
	caller, err := module.Service.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeModelError(w, r, err)
		return
	}
	segments := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/projects/"), "/"), "/")
	if len(segments) < 2 || segments[0] == "" || segments[1] != "models" {
		writeModelError(w, r, ErrNotFound)
		return
	}
	projectID := segments[0]
	rest := segments[2:]
	if len(rest) == 0 {
		if !httpx.RequireMethod(w, r, http.MethodGet) {
			return
		}
		value, err := module.Service.GetOverview(r.Context(), caller, projectID)
		if err != nil {
			writeModelError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, value)
		return
	}
	switch rest[0] {
	case "source":
		module.handleSource(w, r, caller, projectID, rest[1:])
	case "questions":
		module.handleQuestions(w, r, caller, projectID, rest[1:])
	default:
		writeModelError(w, r, ErrNotFound)
	}
}

func (module Module) handleSource(w http.ResponseWriter, r *http.Request, caller auth.Identity, projectID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		value, err := module.Service.GetSource(r.Context(), caller, projectID)
		if err != nil {
			writeModelError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, value)
		return
	}
	if len(rest) == 1 && rest[0] == "sync" && r.Method == http.MethodPost {
		value, err := module.Service.RequestSourceSync(r.Context(), caller, projectID, SyncTriggerManual)
		if err != nil {
			writeModelError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, value)
		return
	}
	writeModelError(w, r, ErrNotFound)
}

func (module Module) handleQuestions(w http.ResponseWriter, r *http.Request, caller auth.Identity, projectID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		items, err := module.Service.ListQuestions(r.Context(), caller, projectID)
		if err != nil {
			writeModelError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
		return
	}
	if len(rest) == 0 && r.Method == http.MethodPost {
		var body contract.CreateModelQuestionRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeModelError(w, r, ErrInvalid)
			return
		}
		position := 0
		if body.Position != nil {
			position = int(*body.Position)
		}
		value, err := module.Service.CreateQuestion(r.Context(), caller, projectID, CreateQuestionInput{Code: body.Code, Title: body.Title, NotionPageID: body.NotionPageID, Position: position})
		if err != nil {
			writeModelError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, value)
		return
	}
	if len(rest) < 1 || rest[0] == "" {
		writeModelError(w, r, ErrNotFound)
		return
	}
	questionID := rest[0]
	if len(rest) == 1 {
		switch r.Method {
		case http.MethodGet:
			value, err := module.Service.GetQuestion(r.Context(), caller, projectID, questionID)
			if err != nil {
				writeModelError(w, r, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, value)
		case http.MethodPatch:
			var body contract.UpdateModelQuestionRequest
			if !httpx.DecodeJSON(w, r, &body) {
				return
			}
			if err := body.Validate(); err != nil {
				writeModelError(w, r, ErrInvalid)
				return
			}
			var position *int
			if body.Position != nil {
				value := int(*body.Position)
				position = &value
			}
			value, err := module.Service.UpdateQuestion(r.Context(), caller, projectID, questionID, UpdateQuestionInput{Code: body.Code, Title: body.Title, NotionPageID: body.NotionPageID, Position: position})
			if err != nil {
				writeModelError(w, r, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, value)
		case http.MethodDelete:
			if err := module.Service.DeleteQuestion(r.Context(), caller, projectID, questionID); err != nil {
				writeModelError(w, r, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			writeModelError(w, r, apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed"))
		}
		return
	}
	switch rest[1] {
	case "sync":
		if len(rest) != 2 || r.Method != http.MethodPost {
			writeModelError(w, r, ErrNotFound)
			return
		}
		value, err := module.Service.RequestQuestionSync(r.Context(), caller, projectID, questionID, SyncTriggerManual)
		if err != nil {
			writeModelError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, value)
	case "snapshots":
		module.handleSnapshots(w, r, caller, projectID, questionID, rest[2:])
	case "diff":
		if len(rest) != 2 || r.Method != http.MethodGet {
			writeModelError(w, r, ErrNotFound)
			return
		}
		fromID, toID := strings.TrimSpace(r.URL.Query().Get("from_snapshot_id")), strings.TrimSpace(r.URL.Query().Get("to_snapshot_id"))
		if fromID == "" || toID == "" || fromID == toID {
			writeModelError(w, r, ErrInvalid)
			return
		}
		value, err := module.Service.Diff(r.Context(), caller, projectID, questionID, fromID, toID)
		if err != nil {
			writeModelError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, value)
	default:
		writeModelError(w, r, ErrNotFound)
	}
}

func (module Module) handleSnapshots(w http.ResponseWriter, r *http.Request, caller auth.Identity, projectID, questionID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		items, err := module.Service.ListSnapshots(r.Context(), caller, projectID, questionID)
		if err != nil {
			writeModelError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
		return
	}
	if len(rest) != 1 || rest[0] == "" {
		writeModelError(w, r, ErrNotFound)
		return
	}
	snapshotID := rest[0]
	switch r.Method {
	case http.MethodGet:
		value, err := module.Service.GetSnapshot(r.Context(), caller, projectID, questionID, snapshotID)
		if err != nil {
			writeModelError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, value)
	case http.MethodPatch:
		var body contract.UpdateModelSnapshotRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeModelError(w, r, ErrInvalid)
			return
		}
		value, err := module.Service.UpdateSnapshot(r.Context(), caller, projectID, questionID, snapshotID, UpdateSnapshotInput{Tags: body.Tags, VersionNote: body.VersionNote})
		if err != nil {
			writeModelError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, value)
	default:
		writeModelError(w, r, apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed"))
	}
}

func (module Module) handleWorkerExport(w http.ResponseWriter, r *http.Request) {
	if !httpx.RequireMethod(w, r, http.MethodGet) {
		return
	}
	caller, err := module.Service.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeModelError(w, r, err)
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/internal/model-notion-jobs/"), "/")
	segments := strings.Split(path, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != "export" {
		writeModelError(w, r, ErrNotFound)
		return
	}
	value, err := module.Service.WorkerExport(r.Context(), caller, segments[0])
	if err != nil {
		writeModelError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, value)
}

func writeModelError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		httpx.WriteError(w, r, appErr)
		return
	}
	status, code, message := http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred"
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		status, code, message = http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required"
	case errors.Is(err, ErrInvalid):
		status, code, message = http.StatusBadRequest, "INVALID_MODEL_REQUEST", "Model input is invalid"
	case errors.Is(err, ErrNotConfigured):
		status, code, message = http.StatusConflict, "MODEL_SOURCE_NOT_CONFIGURED", "Configure and test the project Notion source first"
	case errors.Is(err, ErrPageUndiscovered):
		status, code, message = http.StatusConflict, "MODEL_PAGE_NOT_DISCOVERED", "The Notion page is not a discovered child of this source"
	case errors.Is(err, ErrConflict):
		status, code, message = http.StatusConflict, "MODEL_CONFLICT", "The Model record or synchronization conflicts with current state"
	case errors.Is(err, ErrNotFound):
		status, code, message = http.StatusNotFound, "MODEL_NOT_FOUND", "Model record not found"
	case errors.Is(err, ErrSyncUnavailable):
		status, code, message = http.StatusServiceUnavailable, "MODEL_SYNC_UNAVAILABLE", "Model synchronization is temporarily unavailable"
	case errors.Is(err, project.ErrForbidden):
		status, code, message = http.StatusForbidden, "FORBIDDEN", "Project permission denied"
	}
	httpx.WriteError(w, r, apperror.New(status, code, message))
}
