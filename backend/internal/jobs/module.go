package jobs

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
)

// Module exposes project job and authenticated Worker routes.
type Module struct {
	Service Service
}

func (Module) Name() string { return "jobs" }

// RegisterRoutes attaches the stable Job API to Core.
func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/jobs", module.handleCollection)
	mux.HandleFunc("/v1/jobs/", module.handleResource)
}

func (module Module) handleCollection(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	var body contract.CreateJobRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	job, created, err := module.Service.Create(request.Context(), identity, CreateInput{
		AvailableAt:    body.AvailableAt,
		IdempotencyKey: stringPointerValue(body.IdempotencyKey),
		JobType:        body.JobType,
		MaxAttempts:    intPointerValue(body.MaxAttempts),
		Payload:        body.Payload,
		Priority:       intPointerValue(body.Priority),
		ProjectID:      body.ProjectID,
		TimeoutSeconds: intPointerValue(body.TimeoutSeconds),
	})
	if err != nil {
		writeJobError(response, request, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(response, status, job)
}

func (module Module) handleResource(response http.ResponseWriter, request *http.Request) {
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	path := strings.Trim(strings.TrimPrefix(request.URL.Path, "/v1/jobs/"), "/")
	switch path {
	case "claim":
		module.handleClaim(response, request, identity)
		return
	case "workers/heartbeat":
		module.handleWorkerHeartbeat(response, request, identity)
		return
	}
	segments := strings.Split(path, "/")
	if len(segments) == 0 || segments[0] == "" {
		writeJobError(response, request, ErrNotFound)
		return
	}
	jobID := segments[0]
	if len(segments) == 1 {
		module.handleJob(response, request, identity, jobID)
		return
	}
	if len(segments) != 2 {
		writeJobError(response, request, ErrNotFound)
		return
	}
	switch segments[1] {
	case "cancel":
		module.handleCancel(response, request, identity, jobID)
	case "heartbeat":
		module.handleLeaseHeartbeat(response, request, identity, jobID)
	case "logs":
		module.handleLogs(response, request, identity, jobID)
	case "complete":
		module.handleComplete(response, request, identity, jobID)
	case "fail":
		module.handleFail(response, request, identity, jobID)
	default:
		writeJobError(response, request, ErrNotFound)
	}
}

func (module Module) handleClaim(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.ClaimJobRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	job, err := module.Service.Claim(request.Context(), identity, ClaimInput{
		JobTypes:     append([]string(nil), body.JobTypes...),
		LeaseSeconds: intPointerValue(body.LeaseSeconds),
		WorkerID:     body.WorkerID,
	})
	if err != nil {
		writeJobError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"job": job})
}

func (module Module) handleWorkerHeartbeat(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.WorkerHeartbeatRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	err := module.Service.HeartbeatWorker(request.Context(), identity, WorkerHeartbeat{
		Capabilities: body.Capabilities,
		Metadata:     mapPointerValue(body.Metadata),
		Version:      body.Version,
		WorkerID:     body.WorkerID,
	})
	if err != nil {
		writeJobError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (module Module) handleJob(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	jobID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	job, err := module.Service.Get(request.Context(), identity, jobID)
	if err != nil {
		writeJobError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, job)
}

func (module Module) handleCancel(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	jobID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	job, err := module.Service.Cancel(request.Context(), identity, jobID)
	if err != nil {
		writeJobError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, job)
}

func (module Module) handleLeaseHeartbeat(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	jobID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.RenewJobLeaseRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	job, err := module.Service.Renew(
		request.Context(),
		identity,
		jobID,
		body.WorkerID,
		int(body.LeaseSeconds),
	)
	if err != nil {
		writeJobError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, job)
}

func (module Module) handleLogs(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	jobID string,
) {
	switch request.Method {
	case http.MethodGet:
		logs, err := module.Service.Logs(request.Context(), identity, jobID)
		if err != nil {
			writeJobError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": logs})
	case http.MethodPost:
		var body contract.AppendJobLogRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		entry, err := module.Service.AppendLog(
			request.Context(),
			identity,
			jobID,
			body.WorkerID,
			body.Level,
			body.Message,
			mapPointerValue(body.Fields),
		)
		if err != nil {
			writeJobError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusCreated, entry)
	default:
		writeJobError(response, request, apperror.New(
			http.StatusMethodNotAllowed,
			"METHOD_NOT_ALLOWED",
			"Method not allowed",
		))
	}
}

func (module Module) handleComplete(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	jobID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.CompleteJobRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	job, err := module.Service.Complete(
		request.Context(),
		identity,
		jobID,
		body.WorkerID,
		body.Result,
	)
	if err != nil {
		writeJobError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, job)
}

func (module Module) handleFail(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	jobID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.FailJobRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	job, err := module.Service.Fail(request.Context(), identity, jobID, Failure{
		Code:              body.Code,
		Message:           body.Message,
		RetryDelaySeconds: intPointerValue(body.RetryDelaySeconds),
		Retryable:         body.Retryable,
		WorkerID:          body.WorkerID,
	})
	if err != nil {
		writeJobError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, job)
}

func (module Module) identity(
	response http.ResponseWriter,
	request *http.Request,
) (auth.Identity, bool) {
	identity, err := module.Service.Authenticate(
		request.Context(),
		request.Header.Get("Authorization"),
	)
	if err != nil {
		writeJobError(response, request, err)
		return auth.Identity{}, false
	}
	return identity, true
}

func writeJobError(response http.ResponseWriter, request *http.Request, err error) {
	var applicationError *apperror.Error
	if errors.As(err, &applicationError) {
		httpx.WriteError(response, request, applicationError)
		return
	}
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		httpx.WriteError(response, request, apperror.New(
			http.StatusUnauthorized,
			"UNAUTHENTICATED",
			"Authentication is required",
		))
	case errors.Is(err, ErrWorkerToken):
		httpx.WriteError(response, request, apperror.New(
			http.StatusForbidden,
			"WORKER_API_TOKEN_REQUIRED",
			"Worker operations require an API token",
		))
	case errors.Is(err, ErrForbidden):
		httpx.WriteError(response, request, apperror.New(
			http.StatusForbidden,
			"FORBIDDEN",
			"Job permission denied",
		))
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest,
			"INVALID_JOB_REQUEST",
			"Job input is invalid",
		))
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(response, request, apperror.New(
			http.StatusNotFound,
			"JOB_NOT_FOUND",
			"Job not found",
		))
	case errors.Is(err, ErrLeaseLost), errors.Is(err, ErrConflict):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict,
			"JOB_LEASE_LOST",
			"The Worker no longer owns an active job lease",
		))
	default:
		httpx.WriteError(response, request, err)
	}
}

func intPointerValue(value *int64) int {
	if value == nil {
		return 0
	}
	return int(*value)
}

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func mapPointerValue(value *map[string]interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}
	return *value
}
