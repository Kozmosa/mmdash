package experiment

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
)

type Module struct{ Service *Service }

func (Module) Name() string                         { return "experiment" }
func (module Module) ProjectHandler() http.Handler  { return http.HandlerFunc(module.handleProject) }
func (module Module) RegisterRoutes(*http.ServeMux) {}

func (module Module) handleProject(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/projects/"), "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] != "experiments" {
		writeError(w, r, ErrNotFound)
		return
	}
	identity, err := module.Service.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	projectID := parts[0]
	if len(parts) == 2 {
		switch r.Method {
		case http.MethodGet:
			limit := queryInt(r, "limit", 50)
			page, err := module.Service.List(r.Context(), identity, projectID, r.URL.Query().Get("status"), r.URL.Query().Get("cursor"), limit)
			if err != nil {
				writeError(w, r, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, page)
		case http.MethodPost:
			var body contract.CreateExperimentRequest
			if !httpx.DecodeJSON(w, r, &body) {
				return
			}
			if body.Validate() != nil {
				writeError(w, r, ErrInvalid)
				return
			}
			item, err := module.Service.Create(r.Context(), identity, projectID, Experiment{Name: body.Name, SourceCommit: body.SourceCommit, Entrypoint: body.Entrypoint, Parameters: body.Parameters, Environment: environment(body.Environment), Inputs: body.Inputs, Runtime: body.Runtime, Limits: resourceLimits(body.Limits), IdempotencyKey: body.IdempotencyKey, MaxAttempts: int(optionalInt(body.MaxAttempts, 1))})
			if err != nil {
				writeError(w, r, err)
				return
			}
			httpx.WriteJSON(w, http.StatusCreated, item)
		default:
			writeError(w, r, methodError())
		}
		return
	}
	if len(parts) == 3 && parts[2] == "compare" && r.Method == http.MethodGet {
		ids := r.URL.Query()["experiment_id"]
		if len(ids) == 1 {
			ids = strings.Split(ids[0], ",")
		}
		value, err := module.Service.Compare(r.Context(), identity, projectID, ids)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, value)
		return
	}
	id := parts[2]
	if len(parts) == 3 && r.Method == http.MethodGet {
		item, err := module.Service.Get(r.Context(), identity, projectID, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
		return
	}
	if len(parts) == 4 {
		switch parts[3] {
		case "run":
			if r.Method == http.MethodPost {
				item, err := module.Service.Run(r.Context(), identity, projectID, id)
				if err != nil {
					writeError(w, r, err)
					return
				}
				httpx.WriteJSON(w, http.StatusAccepted, item)
				return
			}
		case "cancel":
			if r.Method == http.MethodPost {
				item, err := module.Service.Cancel(r.Context(), identity, projectID, id)
				if err != nil {
					writeError(w, r, err)
					return
				}
				httpx.WriteJSON(w, http.StatusAccepted, item)
				return
			}
		case "archive":
			if r.Method == http.MethodPost {
				item, err := module.Service.Archive(r.Context(), identity, projectID, id)
				if err != nil {
					writeError(w, r, err)
					return
				}
				httpx.WriteJSON(w, http.StatusAccepted, item)
				return
			}
		case "logs":
			if r.Method == http.MethodGet {
				logs, err := module.Service.Logs(r.Context(), identity, projectID, id, queryInt(r, "offset", 0), queryInt(r, "limit", 100))
				if err != nil {
					writeError(w, r, err)
					return
				}
				httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": logs, "has_more": false})
				return
			}
		case "result":
			if r.Method == http.MethodGet {
				result, err := module.Service.Result(r.Context(), identity, projectID, id)
				if err != nil {
					writeError(w, r, err)
					return
				}
				httpx.WriteJSON(w, http.StatusOK, result)
				return
			}
		}
	}
	writeError(w, r, ErrNotFound)
}

func environment(value map[string]interface{}) map[string]string {
	result := map[string]string{}
	for key, item := range value {
		if text, ok := item.(string); ok {
			result[key] = text
		}
	}
	return result
}
func resourceLimits(value map[string]interface{}) ResourceLimits {
	return ResourceLimits{CPUMillis: int64Number(value["cpu_millis"]), MemoryBytes: int64Number(value["memory_bytes"]), TimeoutSecond: int(int64Number(value["timeout_seconds"])), DiskBytes: int64Number(value["disk_bytes"]), PIDs: int(int64Number(value["pids"])), Network: stringNumber(value["network"])}
}
func int64Number(value interface{}) int64 {
	switch value := value.(type) {
	case int64:
		return value
	case float64:
		return int64(value)
	case int:
		return int64(value)
	default:
		return 0
	}
}
func stringNumber(value interface{}) string {
	if result, ok := value.(string); ok {
		return result
	}
	return ""
}
func optionalInt(value *int64, fallback int64) int64 {
	if value == nil {
		return fallback
	}
	return *value
}
func queryInt(r *http.Request, name string, fallback int) int {
	value := r.URL.Query().Get(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return parsed
}
func methodError() error {
	return apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := http.StatusInternalServerError, "INTERNAL_ERROR", "The experiment operation failed"
	switch {
	case errors.Is(err, ErrInvalid):
		status, code, message = http.StatusBadRequest, "INVALID_REQUEST", "Invalid experiment request"
	case errors.Is(err, ErrForbidden):
		status, code, message = http.StatusForbidden, "FORBIDDEN", "Experiment access forbidden"
	case errors.Is(err, ErrNotFound):
		status, code, message = http.StatusNotFound, "NOT_FOUND", "Experiment not found"
	case errors.Is(err, ErrConflict):
		status, code, message = http.StatusConflict, "CONFLICT", "Experiment state conflict"
	case errors.Is(err, ErrNoResult):
		status, code, message = http.StatusNotFound, "RESULT_NOT_AVAILABLE", "Experiment has no result"
	}
	httpx.WriteError(w, r, apperror.New(status, code, message))
}
