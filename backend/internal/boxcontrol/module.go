package boxcontrol

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
)

type Module struct{ Service *Service }

func (Module) Name() string { return "boxcontrol" }

func (module Module) ProjectHandler() http.Handler { return http.HandlerFunc(module.handleProject) }

func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/boxes", module.handleCollection)
	mux.HandleFunc("/v1/boxes/", module.handleResource)
}

func (module Module) handleCollection(w http.ResponseWriter, r *http.Request) {
	identity, err := module.Service.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := module.Service.List(r.Context(), identity, r.URL.Query().Get("project_id"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
	case http.MethodPost:
		var body contract.RegisterBoxRequest
		if !httpx.DecodeJSON(w, r, &body) || body.Validate() != nil {
			if body.Validate() != nil {
				writeError(w, r, ErrInvalid)
			}
			return
		}
		box, err := module.Service.Register(r.Context(), identity, body.ProjectID, Box{CreatedBy: identity.User.ID, Name: body.Name, Version: body.Version, Capabilities: capabilities(body.Capabilities), Runtimes: runtimes(body.Runtimes), Limits: limits(body.Limits)}, body.IdempotencyKey)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, box)
	default:
		writeError(w, r, apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed"))
	}
}

func (module Module) handleResource(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/v1/boxes/"))
	if len(parts) < 1 {
		writeError(w, r, ErrNotFound)
		return
	}
	identity, err := module.Service.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	boxID := parts[0]
	if len(parts) == 1 && r.Method == http.MethodGet {
		box, err := module.Service.Get(r.Context(), identity, boxID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, box)
		return
	}
	if len(parts) == 2 && parts[1] == "heartbeat" && r.Method == http.MethodPost {
		var body contract.BoxHeartbeatRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		updated, err := module.Service.Heartbeat(r.Context(), identity, boxID, Box{Version: body.Version, Capabilities: capabilities(body.Capabilities), Runtimes: runtimes(body.Runtimes), Limits: limits(body.Limits), Load: load(body.Load)})
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, updated)
		return
	}
	if len(parts) == 3 && parts[1] == "tasks" {
		taskID := parts[2]
		if taskID == "claim" && r.Method == http.MethodPost {
			lease := time.Minute
			var body contract.ClaimBoxTaskRequest
			if r.Body != nil {
				_ = httpx.DecodeJSON(w, r, &body)
				if body.LeaseSeconds != nil {
					lease = time.Duration(*body.LeaseSeconds) * time.Second
				}
			}
			task, err := module.Service.Claim(r.Context(), identity, boxID, lease)
			if errors.Is(err, ErrNoTask) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if err != nil {
				writeError(w, r, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, task)
			return
		}
	}
	if len(parts) == 4 && parts[1] == "tasks" {
		taskID := parts[2]
		switch parts[3] {
		case "heartbeat":
			if r.Method != http.MethodPost {
				break
			}
			var body contract.BoxTaskHeartbeatRequest
			if r.Body != nil {
				_ = httpx.DecodeJSON(w, r, &body)
			}
			lease := time.Minute
			if body.LeaseSeconds != nil {
				lease = time.Duration(*body.LeaseSeconds) * time.Second
			}
			value, err := module.Service.Renew(r.Context(), identity, boxID, taskID, lease)
			if err != nil {
				writeError(w, r, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, value)
			return
		case "logs":
			if r.Method != http.MethodPost {
				break
			}
			var body contract.BoxTaskLogRequest
			if !httpx.DecodeJSON(w, r, &body) {
				return
			}
			if body.Validate() != nil {
				writeError(w, r, ErrInvalid)
				return
			}
			value, err := module.Service.AppendLog(r.Context(), identity, boxID, taskID, Log{TaskID: taskID, Level: body.Level, Message: body.Message, Fields: optionalMap(body.Fields)})
			if err != nil {
				writeError(w, r, err)
				return
			}
			httpx.WriteJSON(w, http.StatusCreated, value)
			return
		case "status":
			if r.Method != http.MethodPost {
				break
			}
			var body contract.BoxTaskStatusRequest
			if !httpx.DecodeJSON(w, r, &body) {
				return
			}
			if body.Validate() != nil {
				writeError(w, r, ErrInvalid)
				return
			}
			value, err := module.Service.ReportStatus(r.Context(), identity, boxID, taskID, body.Status, intPointer(body.ExitCode), stringPointer(body.ErrorCode), stringPointer(body.ErrorMessage), mapPointer(body.ResourceUsage), stringPointer(body.Summary))
			if err != nil {
				writeError(w, r, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, value)
			return
		case "result":
			if r.Method != http.MethodPost {
				break
			}
			var body contract.BoxTaskResultRequest
			if !httpx.DecodeJSON(w, r, &body) {
				return
			}
			if body.Validate() != nil {
				writeError(w, r, ErrInvalid)
				return
			}
			value, err := module.Service.SubmitResult(r.Context(), identity, boxID, taskID, Result{Manifest: body.Manifest, Artifact: body.Artifact})
			if err != nil {
				writeError(w, r, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, value)
			return
		case "artifact":
			if r.Method != http.MethodPost {
				break
			}
			size := r.ContentLength
			if size < 0 {
				writeError(w, r, ErrInvalid)
				return
			}
			if size > 5<<30 {
				writeError(w, r, ErrInvalid)
				return
			}
			value, err := module.Service.UploadArtifact(r.Context(), identity, boxID, taskID, strings.TrimSpace(r.Header.Get("X-Mmdash-Artifact-SHA256")), size, http.MaxBytesReader(w, r.Body, 5<<30))
			if err != nil {
				writeError(w, r, err)
				return
			}
			httpx.WriteJSON(w, http.StatusCreated, value)
			return
		}
	}
	writeError(w, r, ErrNotFound)
}

func (module Module) handleProject(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/v1/projects/"))
	if len(parts) != 2 || parts[1] != "box" {
		writeError(w, r, ErrNotFound)
		return
	}
	identity, err := module.Service.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body contract.BoxBindingRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if body.Validate() != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		box, err := module.Service.Bind(r.Context(), identity, parts[0], body.BoxID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, box)
	case http.MethodDelete:
		if err := module.Service.Unbind(r.Context(), identity, parts[0]); err != nil {
			writeError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, r, apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed"))
	}
}

func splitPath(value string) []string {
	raw := strings.Split(strings.Trim(value, "/"), "/")
	if len(raw) == 1 && raw[0] == "" {
		return nil
	}
	return raw
}
func capabilities(values []map[string]interface{}) []Capability {
	result := make([]Capability, 0, len(values))
	for _, value := range values {
		item := Capability{Name: stringValue(value["name"]), Version: stringValue(value["version"])}
		if raw, ok := value["features"].([]interface{}); ok {
			for _, feature := range raw {
				if text, ok := feature.(string); ok {
					item.Features = append(item.Features, text)
				}
			}
		}
		result = append(result, item)
	}
	return result
}
func runtimes(values []map[string]interface{}) []Runtime {
	result := make([]Runtime, 0, len(values))
	for _, value := range values {
		result = append(result, Runtime{Name: stringValue(value["name"]), Version: stringValue(value["version"]), Image: stringValue(value["image"])})
	}
	return result
}
func limits(value map[string]interface{}) ResourceLimits {
	return ResourceLimits{CPUMillis: int64Value(value["cpu_millis"]), MemoryBytes: int64Value(value["memory_bytes"]), TimeoutSecond: int(int64Value(value["timeout_seconds"])), DiskBytes: int64Value(value["disk_bytes"]), PIDs: int(int64Value(value["pids"])), Network: stringValue(value["network"])}
}
func load(value map[string]interface{}) Load {
	return Load{RunningTasks: int(int64Value(value["running_tasks"])), Capacity: int(int64Value(value["capacity"])), CPUMillis: int64Value(value["cpu_millis"]), MemoryBytes: int64Value(value["memory_bytes"])}
}
func stringValue(value interface{}) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}
func int64Value(value interface{}) int64 {
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
func stringPointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func intPointer(value *int64) *int {
	if value == nil {
		return nil
	}
	result := int(*value)
	return &result
}
func mapPointer(value *map[string]interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	return *value
}
func optionalMap(value *map[string]interface{}) map[string]interface{} { return mapPointer(value) }
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := http.StatusInternalServerError, "INTERNAL_ERROR", "The Box operation failed"
	switch {
	case errors.Is(err, ErrInvalid):
		status, code, message = http.StatusBadRequest, "INVALID_REQUEST", "Invalid Box request"
	case errors.Is(err, ErrForbidden):
		status, code, message = http.StatusForbidden, "FORBIDDEN", "Box access forbidden"
	case errors.Is(err, ErrNotFound):
		status, code, message = http.StatusNotFound, "NOT_FOUND", "Box resource not found"
	case errors.Is(err, ErrConflict):
		status, code, message = http.StatusConflict, "CONFLICT", "Box resource conflict"
	case errors.Is(err, ErrNoTask):
		status, code, message = http.StatusNoContent, "NO_TASK", "No task available"
	case errors.Is(err, ErrLeaseLost):
		status, code, message = http.StatusConflict, "LEASE_LOST", "Box task lease is no longer active"
	case errors.Is(err, auth.ErrUnauthenticated):
		status, code, message = http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required"
	}
	httpx.WriteError(w, r, apperror.New(status, code, message))
}
