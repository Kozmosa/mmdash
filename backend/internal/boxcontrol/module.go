package boxcontrol

import (
	"errors"
	"net/http"
	"strconv"
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
	mux.HandleFunc("/v1/box-source-transfers/", module.handleSourceTransfer)
	mux.HandleFunc("/v1/boxes", module.handleRegistration)
	mux.HandleFunc("/v1/boxes/", module.handleGateway)
	mux.HandleFunc("/v1/users/me/boxes", module.handlePersonalCollection)
	mux.HandleFunc("/v1/users/me/boxes/", module.handlePersonalResource)
}

func (module Module) handleSourceTransfer(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	raw := strings.TrimPrefix(request.URL.Path, "/v1/box-source-transfers/")
	token, err := sourceTransferToken(raw)
	if err != nil {
		writeError(response, request, ErrNotFound)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/zip")
	response.Header().Set("Content-Disposition", `attachment; filename="source.zip"`)
	if err := module.Service.OpenSourceTransfer(request.Context(), token, response); err != nil {
		writeError(response, request, err)
	}
}

func (module Module) handleRegistration(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.RegisterBoxRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	if err := body.Validate(); err != nil {
		writeError(response, request, ErrInvalid)
		return
	}
	box, err := module.Service.Register(request.Context(), body.RegistrationGrant, Box{
		InstallationID: body.InstallationID,
		Name:           body.Name,
		Version:        body.Version,
		Capabilities:   capabilities(optionalMaps(body.Capabilities)),
		Runtimes:       runtimes(optionalMaps(body.Runtimes)),
		Limits:         limits(optionalMap(body.Limits)),
	})
	if err != nil {
		writeError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusCreated, box)
}

func (module Module) handlePersonalCollection(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	identity, err := module.authenticate(request)
	if err != nil {
		writeError(response, request, err)
		return
	}
	items, err := module.Service.ListOwned(request.Context(), identity)
	if err != nil {
		writeError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": items})
}

func (module Module) handlePersonalResource(response http.ResponseWriter, request *http.Request) {
	parts := splitPath(strings.TrimPrefix(request.URL.Path, "/v1/users/me/boxes/"))
	if len(parts) < 1 || len(parts) > 2 {
		writeError(response, request, ErrNotFound)
		return
	}
	identity, err := module.authenticate(request)
	if err != nil {
		writeError(response, request, err)
		return
	}
	boxID := parts[0]
	if len(parts) == 1 {
		switch request.Method {
		case http.MethodGet:
			box, err := module.Service.GetOwned(request.Context(), identity, boxID)
			if err != nil {
				writeError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusOK, box)
		case http.MethodPatch:
			var body contract.UpdateBoxRequest
			if !httpx.DecodeJSON(response, request, &body) {
				return
			}
			if err := body.Validate(); err != nil {
				writeError(response, request, ErrInvalid)
				return
			}
			box, err := module.Service.UpdateOwned(request.Context(), identity, boxID, body.Name)
			if err != nil {
				writeError(response, request, err)
				return
			}
			httpx.WriteJSON(response, http.StatusOK, box)
		default:
			writeError(response, request, methodNotAllowed("GET, PATCH"))
		}
		return
	}
	if parts[1] != "revoke" || request.Method != http.MethodPost {
		writeError(response, request, ErrNotFound)
		return
	}
	var body contract.RevokeBoxRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	if err := body.Validate(); err != nil {
		writeError(response, request, ErrInvalid)
		return
	}
	result, err := module.Service.Revoke(request.Context(), identity, boxID, body.Mode)
	if err != nil {
		writeError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusAccepted, result)
}

func (module Module) handleGateway(response http.ResponseWriter, request *http.Request) {
	parts := splitPath(strings.TrimPrefix(request.URL.Path, "/v1/boxes/"))
	if len(parts) < 2 {
		writeError(response, request, ErrNotFound)
		return
	}
	identity, err := module.authenticate(request)
	if err != nil {
		writeError(response, request, err)
		return
	}
	boxID := parts[0]
	if len(parts) == 2 && parts[1] == "heartbeat" && request.Method == http.MethodPost {
		module.handleHeartbeat(response, request, identity, boxID)
		return
	}
	if len(parts) == 3 && parts[1] == "tasks" && parts[2] == "claim" && request.Method == http.MethodPost {
		module.handleClaim(response, request, identity, boxID)
		return
	}
	if len(parts) == 4 && parts[1] == "tasks" {
		taskID := parts[2]
		switch parts[3] {
		case "resume":
			module.handleResume(response, request, identity, boxID, taskID)
		case "logs":
			module.handleLogs(response, request, identity, boxID, taskID)
		case "status":
			module.handleStatus(response, request, identity, boxID, taskID)
		case "result":
			module.handleResult(response, request, identity, boxID, taskID)
		case "artifact":
			module.handleArtifact(response, request, identity, boxID, taskID)
		default:
			writeError(response, request, ErrNotFound)
		}
		return
	}
	writeError(response, request, ErrNotFound)
}

func (module Module) handleHeartbeat(response http.ResponseWriter, request *http.Request, identity auth.Identity, boxID string) {
	var body contract.BoxHeartbeatRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	if err := body.Validate(); err != nil {
		writeError(response, request, ErrInvalid)
		return
	}
	updated, err := module.Service.Heartbeat(request.Context(), identity, boxID, Box{
		Version: body.Version, Capabilities: capabilities(body.Capabilities),
		Runtimes: runtimes(body.Runtimes), Limits: limits(body.Limits), Load: load(body.Load),
	})
	if err != nil {
		writeError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, updated)
}

func (module Module) handleClaim(response http.ResponseWriter, request *http.Request, identity auth.Identity, boxID string) {
	body := contract.ClaimBoxTaskRequest{}
	if request.ContentLength != 0 {
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
	}
	if err := body.Validate(); err != nil {
		writeError(response, request, ErrInvalid)
		return
	}
	wait := 30 * time.Second
	if body.WaitSeconds != nil {
		wait = time.Duration(*body.WaitSeconds) * time.Second
	}
	task, err := module.Service.Claim(request.Context(), identity, boxID, wait)
	if errors.Is(err, ErrNoTask) {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, task)
}

func (module Module) handleResume(response http.ResponseWriter, request *http.Request, identity auth.Identity, boxID, taskID string) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.ResumeBoxTaskRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	if err := body.Validate(); err != nil {
		writeError(response, request, ErrInvalid)
		return
	}
	result, err := module.Service.Resume(request.Context(), identity, boxID, taskID, ResumeRequest{
		ExecutionEpoch: body.ExecutionEpoch, LocalPhase: body.LocalPhase,
		LastLocalSequence: body.LastLocalSequence, BundleState: body.BundleState,
		AcknowledgedCallbacks: body.AcknowledgedCallbacks,
	})
	if err != nil {
		writeError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, result)
}

func (module Module) handleLogs(response http.ResponseWriter, request *http.Request, identity auth.Identity, boxID, taskID string) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.BoxTaskLogRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	if err := body.Validate(); err != nil {
		writeError(response, request, ErrInvalid)
		return
	}
	entries := make([]Log, 0, len(body.Entries))
	for _, value := range body.Entries {
		entries = append(entries, Log{
			Sequence: int64Value(value["sequence"]), Stream: stringValue(value["stream"]),
			Message: stringValue(value["message"]), Fields: mapValue(value["fields"]),
			OccurredAt: timeValue(value["occurred_at"]),
		})
	}
	result, err := module.Service.AppendLogs(request.Context(), identity, boxID, taskID, LogBatch{
		ExecutionEpoch: body.ExecutionEpoch, FirstSequence: body.FirstSequence,
		Entries: entries, LogsTruncated: body.LogsTruncated, TruncatedAt: body.LogsTruncatedAt,
	})
	if err != nil {
		writeError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, result)
}

func (module Module) handleStatus(response http.ResponseWriter, request *http.Request, identity auth.Identity, boxID, taskID string) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.BoxTaskStatusRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	if err := body.Validate(); err != nil {
		writeError(response, request, ErrInvalid)
		return
	}
	result, err := module.Service.ReportStatus(
		request.Context(), identity, boxID, taskID, body.ExecutionEpoch, body.Status,
		body.OccurredAt, intPointer(body.ExitCode), failure(body.Failure),
		optionalMap(body.ResourceUsage), stringPointer(body.Summary),
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, result)
}

func (module Module) handleResult(response http.ResponseWriter, request *http.Request, identity auth.Identity, boxID, taskID string) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.BoxTaskResultRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	if err := body.Validate(); err != nil {
		writeError(response, request, ErrInvalid)
		return
	}
	result, err := module.Service.SubmitResult(request.Context(), identity, boxID, taskID, Result{
		ExecutionEpoch: body.ExecutionEpoch, ManifestSHA256: body.ManifestSha256,
		ExecutionBundle: artifactPointerFromMap(body.ExecutionBundle),
	})
	if err != nil {
		writeError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, result)
}

func (module Module) handleArtifact(response http.ResponseWriter, request *http.Request, identity auth.Identity, boxID, taskID string) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	expectedSize, err := strconv.ParseInt(strings.TrimSpace(request.Header.Get("X-Mmdash-Artifact-Size")), 10, 64)
	if err != nil || expectedSize < 1 || request.ContentLength != expectedSize {
		writeError(response, request, ErrInvalid)
		return
	}
	value, err := module.Service.UploadArtifact(
		request.Context(), identity, boxID, taskID,
		strings.TrimSpace(request.Header.Get("X-Mmdash-Execution-Epoch")),
		strings.TrimSpace(request.Header.Get("X-Mmdash-Artifact-SHA256")),
		expectedSize, http.MaxBytesReader(response, request.Body, 5<<30),
	)
	if err != nil {
		writeError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusCreated, value)
}

func (module Module) handleProject(response http.ResponseWriter, request *http.Request) {
	parts := splitPath(strings.TrimPrefix(request.URL.Path, "/v1/projects/"))
	if len(parts) < 2 || parts[1] != "boxes" {
		writeError(response, request, ErrNotFound)
		return
	}
	identity, err := module.authenticate(request)
	if err != nil {
		writeError(response, request, err)
		return
	}
	projectID := parts[0]
	if len(parts) == 2 && request.Method == http.MethodGet {
		items, err := module.Service.ListProject(request.Context(), identity, projectID)
		if err != nil {
			writeError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": items})
		return
	}
	if len(parts) != 3 {
		writeError(response, request, ErrNotFound)
		return
	}
	boxID := parts[2]
	switch request.Method {
	case http.MethodPut:
		binding, err := module.Service.Assign(request.Context(), identity, projectID, boxID)
		if err != nil {
			writeError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, binding)
	case http.MethodDelete:
		force, err := strconv.ParseBool(defaultString(request.URL.Query().Get("force"), "false"))
		if err != nil {
			writeError(response, request, ErrInvalid)
			return
		}
		if err := module.Service.Unassign(request.Context(), identity, projectID, boxID, force); err != nil {
			writeError(response, request, err)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	default:
		writeError(response, request, methodNotAllowed("PUT, DELETE"))
	}
}

func (module Module) authenticate(request *http.Request) (auth.Identity, error) {
	return module.Service.Authenticate(request.Context(), request.Header.Get("Authorization"))
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
		for _, feature := range stringSlice(value["features"]) {
			item.Features = append(item.Features, feature)
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

func failure(value *map[string]interface{}) *Failure {
	if value == nil {
		return nil
	}
	result := &Failure{
		Stage: stringValue((*value)["stage"]), Code: stringValue((*value)["code"]),
		Message: stringValue((*value)["message"]), FailedAt: timeValue((*value)["failed_at"]),
		BoxID: stringValue((*value)["box_id"]), Runtime: stringValue((*value)["runtime"]),
		Attempt: int(int64Value((*value)["attempt"])), Retryable: boolValue((*value)["retryable"]),
		CleanupResult: mapValue((*value)["cleanup_result"]),
	}
	return result
}

func optionalMaps(value *[]map[string]interface{}) []map[string]interface{} {
	if value == nil {
		return []map[string]interface{}{}
	}
	return *value
}

func optionalMap(value *map[string]interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	return *value
}

func mapValue(value interface{}) map[string]interface{} {
	result, _ := value.(map[string]interface{})
	if result == nil {
		return map[string]interface{}{}
	}
	return result
}

func stringValue(value interface{}) string {
	result, _ := value.(string)
	return result
}

func stringSlice(value interface{}) []string {
	result := []string{}
	items, _ := value.([]interface{})
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func int64Value(value interface{}) int64 {
	switch value := value.(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func boolValue(value interface{}) bool {
	result, _ := value.(bool)
	return result
}

func timeValue(value interface{}) time.Time {
	switch value := value.(type) {
	case time.Time:
		return value
	case string:
		result, _ := time.Parse(time.RFC3339Nano, value)
		return result
	default:
		return time.Time{}
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

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func methodNotAllowed(_ string) error {
	return apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
}

func writeError(response http.ResponseWriter, request *http.Request, err error) {
	var applicationError *apperror.Error
	if errors.As(err, &applicationError) {
		httpx.WriteError(response, request, applicationError)
		return
	}
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
	case errors.Is(err, ErrSourceTransferExpired):
		status, code, message = http.StatusGone, "BOX_SOURCE_TRANSFER_EXPIRED", "Box source transfer expired"
	case errors.Is(err, auth.ErrUnauthenticated):
		status, code, message = http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required"
	}
	httpx.WriteError(response, request, apperror.New(status, code, message))
}
