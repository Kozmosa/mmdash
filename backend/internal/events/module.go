package events

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mmdash/mmdash/backend/internal/auth"
	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/httpx"
	"github.com/mmdash/mmdash/backend/internal/platform/outbox"
)

// Module exposes system-administrator event operations.
type Module struct {
	Service Service
}

func (Module) Name() string { return "events" }

// RegisterRoutes attaches Outbox inspection and replay routes.
func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/events/test", module.handleTest)
	mux.HandleFunc("/v1/events/consumers", module.handleConsumers)
	mux.HandleFunc("/v1/events/", module.handleResource)
}

func (module Module) handleTest(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	var body contract.EmitTestEventRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	enqueued, err := module.Service.EmitTest(
		request.Context(),
		identity,
		body.Message,
		mapPointerValue(body.Payload),
	)
	if err != nil {
		writeEventError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusCreated, enqueued)
}

func (module Module) handleConsumers(response http.ResponseWriter, request *http.Request) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	consumers, err := module.Service.Consumers(identity)
	if err != nil {
		writeEventError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": consumers})
}

func (module Module) handleResource(response http.ResponseWriter, request *http.Request) {
	identity, ok := module.identity(response, request)
	if !ok {
		return
	}
	segments := strings.Split(
		strings.Trim(strings.TrimPrefix(request.URL.Path, "/v1/events/"), "/"),
		"/",
	)
	if len(segments) == 0 || segments[0] == "" {
		writeEventError(response, request, outbox.ErrNotFound)
		return
	}
	eventID := segments[0]
	if len(segments) == 1 {
		if !httpx.RequireMethod(response, request, http.MethodGet) {
			return
		}
		state, err := module.Service.Get(request.Context(), identity, eventID)
		if err != nil {
			writeEventError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, state)
		return
	}
	if len(segments) != 2 || segments[1] != "replay" {
		writeEventError(response, request, outbox.ErrNotFound)
		return
	}
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.ReplayEventRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	replay, err := module.Service.Replay(
		request.Context(),
		identity,
		eventID,
		stringPointerValue(body.ConsumerName),
		body.Reason,
	)
	if err != nil {
		writeEventError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusAccepted, replay)
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
		writeEventError(response, request, err)
		return auth.Identity{}, false
	}
	return identity, true
}

func writeEventError(response http.ResponseWriter, request *http.Request, err error) {
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
	case errors.Is(err, ErrForbidden):
		httpx.WriteError(response, request, apperror.New(
			http.StatusForbidden,
			"FORBIDDEN",
			"Event operations require a system administrator",
		))
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest,
			"INVALID_EVENT_REQUEST",
			"Event operation input is invalid",
		))
	case errors.Is(err, outbox.ErrNoConsumers):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict,
			"EVENT_HAS_NO_MATCHING_CONSUMERS",
			"No registered consumer matches this event",
		))
	case errors.Is(err, outbox.ErrNotFound):
		httpx.WriteError(response, request, apperror.New(
			http.StatusNotFound,
			"EVENT_NOT_FOUND",
			"Outbox event not found",
		))
	default:
		httpx.WriteError(response, request, err)
	}
}

func mapPointerValue(value *map[string]interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	return *value
}

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
