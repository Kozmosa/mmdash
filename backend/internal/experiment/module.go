package experiment

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/boxcontrol"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
)

type Module struct{ Service *Service }

func (Module) Name() string                        { return "experiment" }
func (module Module) ProjectHandler() http.Handler { return http.HandlerFunc(module.handleProject) }
func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/internal/experiment-result-jobs/", module.handleWorker)
}

func (module Module) handleWorker(w http.ResponseWriter, r *http.Request) {
	identity, err := module.Service.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(
		r.URL.Path, "/v1/internal/experiment-result-jobs/",
	), "/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		writeError(w, r, ErrNotFound)
		return
	}
	switch parts[1] {
	case "input":
		if r.Method != http.MethodGet {
			writeError(w, r, methodError())
			return
		}
		value, err := module.Service.WorkerResultInput(r.Context(), identity, parts[0])
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, value)
	case "finalize":
		if r.Method != http.MethodPost {
			writeError(w, r, methodError())
			return
		}
		var body contract.FinalizeExperimentResultRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if body.Validate() != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		prepared := ResultPreparation{
			ManifestSHA256: body.ManifestSha256,
			Files:          make([]PreparedResultFile, 0, len(body.Files)),
		}
		if body.Summary != nil {
			prepared.Summary = *body.Summary
		}
		if body.Analysis != nil {
			prepared.Analysis = *body.Analysis
		}
		for _, file := range body.Files {
			prepared.Files = append(prepared.Files, PreparedResultFile{
				Path: mapString(file, "path"), SHA256: mapString(file, "sha256"),
				SizeBytes: mapInt64(file, "size_bytes"), Kind: mapString(file, "kind"),
				MediaType: mapString(file, "media_type"),
			})
		}
		value, err := module.Service.FinalizeWorkerResult(
			r.Context(), identity, parts[0], prepared,
		)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, value)
	default:
		writeError(w, r, ErrNotFound)
	}
}

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
		module.handleCollection(w, r, identity, projectID)
		return
	}
	if len(parts) == 3 && parts[2] == "settings" {
		module.handleSettings(w, r, identity, projectID)
		return
	}
	if len(parts) == 3 && parts[2] == "compare" {
		if r.Method != http.MethodGet {
			writeError(w, r, methodError())
			return
		}
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

	experimentID := parts[2]
	if len(parts) == 3 {
		if r.Method != http.MethodGet {
			writeError(w, r, methodError())
			return
		}
		item, err := module.Service.Get(r.Context(), identity, projectID, experimentID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
		return
	}
	if len(parts) == 5 && parts[3] == "result" && parts[4] == "bind" {
		if r.Method != http.MethodPost {
			writeError(w, r, methodError())
			return
		}
		var body contract.BindExperimentResultRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if body.Validate() != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		item, err := module.Service.BindResult(
			r.Context(), identity, projectID, experimentID,
			body.CommitSha, body.IdempotencyKey,
		)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, item)
		return
	}
	if len(parts) != 4 {
		writeError(w, r, ErrNotFound)
		return
	}
	module.handleAction(w, r, identity, projectID, experimentID, parts[3])
}

func (module Module) handleCollection(w http.ResponseWriter, r *http.Request, identity interfaceIdentity, projectID string) {
	switch r.Method {
	case http.MethodGet:
		page, err := module.Service.List(
			r.Context(), identity, projectID,
			r.URL.Query().Get("execution_status"), r.URL.Query().Get("cursor"),
			queryInt(r, "limit", 50),
		)
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
		item := Experiment{
			Name: body.Name, Type: body.ExperimentType,
			SourceCommit: body.SourceCommit, Entrypoint: body.Entrypoint,
			Parameters: body.Parameters, Environment: environment(body.Environment),
			Inputs: body.Inputs, IdempotencyKey: body.IdempotencyKey,
		}
		if body.RuntimePolicy != nil {
			item.RequestedRuntimePolicy = *body.RuntimePolicy
		}
		if body.RequestedBoxID != nil {
			item.RequestedBoxID = *body.RequestedBoxID
		}
		if body.LimitsOverride != nil {
			item.Limits = resourceLimits(*body.LimitsOverride)
		}
		created, err := module.Service.Create(r.Context(), identity, projectID, item)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, created)
	default:
		writeError(w, r, methodError())
	}
}

func (module Module) handleSettings(w http.ResponseWriter, r *http.Request, identity interfaceIdentity, projectID string) {
	switch r.Method {
	case http.MethodGet:
		settings, err := module.Service.GetSettings(r.Context(), identity, projectID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, settings)
	case http.MethodPatch:
		var body contract.UpdateExperimentSettingsRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if body.Validate() != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		patch := SettingsPatch{
			Timezone: body.Timezone, DefaultRuntimePolicy: body.DefaultRuntimePolicy,
			GitLargeFileThresholdBytes: body.GitLargeFileThresholdBytes,
		}
		if body.DefaultLimits != nil {
			value := resourceLimits(*body.DefaultLimits)
			patch.DefaultLimits = &value
		}
		settings, err := module.Service.UpdateSettings(r.Context(), identity, projectID, patch)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, settings)
	default:
		writeError(w, r, methodError())
	}
}

func (module Module) handleAction(
	w http.ResponseWriter,
	r *http.Request,
	identity interfaceIdentity,
	projectID, experimentID, action string,
) {
	switch action {
	case "run":
		if r.Method != http.MethodPost {
			break
		}
		var body contract.RunExperimentRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if body.Validate() != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		item, err := module.Service.Run(
			r.Context(), identity, projectID, experimentID, body.IdempotencyKey,
		)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, item)
		return
	case "cancel":
		if r.Method != http.MethodPost {
			break
		}
		item, err := module.Service.Cancel(r.Context(), identity, projectID, experimentID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, item)
		return
	case "archive":
		if r.Method != http.MethodPost {
			break
		}
		item, err := module.Service.Archive(r.Context(), identity, projectID, experimentID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, item)
		return
	case "rerun":
		if r.Method != http.MethodPost {
			break
		}
		var body contract.RerunExperimentRequest
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if body.Validate() != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		overrides := RerunOverrides{
			Name: body.Name, SourceCommit: body.SourceCommit, Entrypoint: body.Entrypoint,
			Parameters: body.Parameters, Inputs: body.Inputs,
			RequestedRuntimePolicy: body.RuntimePolicy, RequestedBoxID: body.RequestedBoxID,
			IdempotencyKey: body.IdempotencyKey,
		}
		if body.Environment != nil {
			value := environment(*body.Environment)
			overrides.Environment = &value
		}
		if body.LimitsOverride != nil {
			value := resourceLimits(*body.LimitsOverride)
			overrides.Limits = &value
		}
		item, err := module.Service.Rerun(r.Context(), identity, projectID, experimentID, overrides)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, item)
		return
	case "logs":
		if r.Method != http.MethodGet {
			break
		}
		after, err := queryInt64(r, "cursor", 0)
		if err != nil {
			writeError(w, r, ErrInvalid)
			return
		}
		tailValue := r.URL.Query().Get("tail")
		if tailValue != "" && tailValue != "true" && tailValue != "false" {
			writeError(w, r, ErrInvalid)
			return
		}
		limit := queryInt(r, "limit", 100)
		var logs []boxcontrol.Log
		var hasMore bool
		if tailValue == "true" {
			if after != 0 {
				writeError(w, r, ErrInvalid)
				return
			}
			logs, hasMore, err = module.Service.TailLogs(
				r.Context(), identity, projectID, experimentID, limit,
			)
		} else {
			logs, hasMore, err = module.Service.Logs(
				r.Context(), identity, projectID, experimentID, after, limit,
			)
		}
		if err != nil {
			writeError(w, r, err)
			return
		}
		next := ""
		if len(logs) > 0 {
			next = strconv.FormatInt(logs[len(logs)-1].Sequence, 10)
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"items": logs, "has_more": hasMore, "next_cursor": next,
		})
		return
	case "result":
		if r.Method != http.MethodGet {
			break
		}
		result, err := module.Service.Result(r.Context(), identity, projectID, experimentID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
		return
	default:
		writeError(w, r, ErrNotFound)
		return
	}
	writeError(w, r, methodError())
}

// Keep handlers decoupled from concrete auth response fields while retaining
// the exact identity type expected by Service methods.
type interfaceIdentity = auth.Identity

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
	return ResourceLimits{
		CPUMillis:     int64Number(value["cpu_millis"]),
		MemoryBytes:   int64Number(value["memory_bytes"]),
		TimeoutSecond: int(int64Number(value["timeout_seconds"])),
		DiskBytes:     int64Number(value["disk_bytes"]),
		PIDs:          int(int64Number(value["pids"])), Network: stringNumber(value["network"]),
	}
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

func queryInt64(r *http.Request, name string, fallback int64) (int64, error) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, ErrInvalid
	}
	return parsed, nil
}

func methodError() error {
	return apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var processingFailure *ResultProcessingError
	if errors.As(err, &processingFailure) {
		status := http.StatusUnprocessableEntity
		if processingFailure.Retryable {
			status = http.StatusServiceUnavailable
		}
		httpx.WriteError(w, r, apperror.New(
			status, processingFailure.Code, processingFailure.Message,
		))
		return
	}
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
