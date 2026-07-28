package audit

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
	"github.com/mmdash/mmdash/backend/internal/platform/pagination"
	"github.com/mmdash/mmdash/backend/internal/project"
)

// Module exposes audit ingestion and authorized search.
type Module struct {
	Service Service
}

func (Module) Name() string { return "audit" }

func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/audit/events", module.handleEvents)
}

func (module Module) handleEvents(response http.ResponseWriter, request *http.Request) {
	identity, err := module.Service.Authenticate(
		request.Context(), request.Header.Get("Authorization"),
	)
	if err != nil {
		writeAuditError(response, request, err)
		return
	}
	switch request.Method {
	case http.MethodGet:
		page, ok := auditPage(request)
		if !ok {
			writeAuditError(response, request, ErrInvalid)
			return
		}
		result, err := module.Service.List(
			request.Context(), identity, Filter{
				Action:    request.URL.Query().Get("action"),
				ActorID:   request.URL.Query().Get("actor_id"),
				Category:  request.URL.Query().Get("category"),
				Outcome:   request.URL.Query().Get("outcome"),
				ProjectID: request.URL.Query().Get("project_id"),
				RequestID: request.URL.Query().Get("request_id"),
				Source:    request.URL.Query().Get("source"),
			}, page,
		)
		if err != nil {
			writeAuditError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, result)
	case http.MethodPost:
		var body contract.RecordAuditEventRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeAuditError(response, request, ErrInvalid)
			return
		}
		event, err := module.Service.Ingest(
			request.Context(), identity, Input{
				Action: body.Action, Category: body.Category,
				DurationMS: body.DurationMs, ErrorCode: optional(body.ErrorCode),
				Metadata: optionalMap(body.Metadata), OccurredAt: body.OccurredAt,
				Outcome: body.Outcome, ProjectID: optional(body.ProjectID),
				ResourceID:   optional(body.ResourceID),
				ResourceType: optional(body.ResourceType), Source: body.Source,
			},
		)
		if err != nil {
			writeAuditError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusCreated, event)
	default:
		response.Header().Set("Allow", "GET, POST")
		writeAuditError(response, request, apperror.New(
			http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed",
		))
	}
}

func auditPage(request *http.Request) (pagination.Request, bool) {
	limit := 0
	if value := request.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return pagination.Request{}, false
		}
		limit = parsed
	}
	return pagination.Request{
		Cursor: request.URL.Query().Get("cursor"), Limit: limit,
	}, true
}

func optional(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func optionalMap(value *map[string]interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	return *value
}

func writeAuditError(response http.ResponseWriter, request *http.Request, err error) {
	var applicationError *apperror.Error
	if errors.As(err, &applicationError) {
		httpx.WriteError(response, request, applicationError)
		return
	}
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		httpx.WriteError(response, request, apperror.New(
			http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required",
		))
	case errors.Is(err, project.ErrForbidden), errors.Is(err, ErrForbidden):
		httpx.WriteError(response, request, apperror.New(
			http.StatusForbidden, "FORBIDDEN", "Audit permission denied",
		))
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest, "INVALID_AUDIT_EVENT", "Audit input is invalid",
		))
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(response, request, apperror.New(
			http.StatusNotFound, "AUDIT_EVENT_NOT_FOUND", "Audit event not found",
		))
	default:
		httpx.WriteError(response, request, err)
	}
}
