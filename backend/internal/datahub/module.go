package datahub

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

// Module exposes stable Data Hub routing and Project Context APIs.
type Module struct {
	Service Service
}

func (Module) Name() string { return "datahub" }

func (module Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/data/projects/", module.handleProjectData)
}

func (module Module) handleProjectData(response http.ResponseWriter, request *http.Request) {
	identity, err := module.Service.Authenticate(
		request.Context(),
		request.Header.Get("Authorization"),
	)
	if err != nil {
		writeDataError(response, request, err)
		return
	}
	segments := strings.Split(strings.Trim(
		strings.TrimPrefix(request.URL.Path, "/v1/data/projects/"), "/",
	), "/")
	if len(segments) < 2 || segments[0] == "" {
		writeDataError(response, request, ErrNotFound)
		return
	}
	projectID := segments[0]
	switch segments[1] {
	case "objects":
		if len(segments) == 2 {
			module.handleObjects(response, request, identity, projectID)
			return
		}
		if len(segments) == 3 {
			module.handleObject(response, request, identity, projectID, segments[2])
			return
		}
	case "activity":
		if len(segments) == 2 {
			module.handleActivity(response, request, identity, projectID)
			return
		}
	case "context":
		if len(segments) == 2 {
			module.handleContext(response, request, identity, projectID)
			return
		}
		if len(segments) == 3 && segments[2] == "proposals" {
			module.handleProposals(response, request, identity, projectID)
			return
		}
		if len(segments) == 5 && segments[2] == "proposals" &&
			segments[4] == "review" {
			module.handleReview(
				response, request, identity, projectID, segments[3],
			)
			return
		}
	case "home":
		if len(segments) == 2 {
			module.handleHome(response, request, identity, projectID)
			return
		}
	}
	writeDataError(response, request, ErrNotFound)
}

func (module Module) handleObjects(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	page, ok := pageRequest(request)
	if !ok {
		writeDataError(response, request, ErrInvalid)
		return
	}
	result, err := module.Service.ListObjects(
		request.Context(), identity, projectID,
		request.URL.Query().Get("type"), page,
	)
	if err != nil {
		writeDataError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, result)
}

func (module Module) handleObject(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	objectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	result, err := module.Service.ReadObject(
		request.Context(), identity, projectID, objectID,
	)
	if err != nil {
		writeDataError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, result)
}

func (module Module) handleActivity(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	page, ok := pageRequest(request)
	if !ok {
		writeDataError(response, request, ErrInvalid)
		return
	}
	result, err := module.Service.ListActivity(
		request.Context(), identity, projectID, page,
	)
	if err != nil {
		writeDataError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, result)
}

func (module Module) handleContext(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	items, err := module.Service.ListContext(request.Context(), identity, projectID)
	if err != nil {
		writeDataError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": items})
}

func (module Module) handleProposals(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	switch request.Method {
	case http.MethodGet:
		items, err := module.Service.ListProposals(
			request.Context(), identity, projectID,
		)
		if err != nil {
			writeDataError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusOK, map[string]interface{}{"items": items})
	case http.MethodPost:
		var body contract.CreateContextProposalRequest
		if !httpx.DecodeJSON(response, request, &body) {
			return
		}
		if err := body.Validate(); err != nil {
			writeDataError(response, request, ErrInvalid)
			return
		}
		proposal, err := module.Service.CreateProposal(
			request.Context(), identity, projectID, CreateProposalInput{
				AgentRunID:     optionalString(body.AgentRunID),
				AgentSessionID: optionalString(body.AgentSessionID),
				Content:        body.Content, ContextType: body.ContextType,
				Rationale:       optionalString(body.Rationale),
				SourceObjectIDs: optionalStrings(body.SourceObjectIDs),
				Title:           body.Title,
			},
		)
		if err != nil {
			writeDataError(response, request, err)
			return
		}
		httpx.WriteJSON(response, http.StatusCreated, proposal)
	default:
		writeDataError(response, request, apperror.New(
			http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed",
		))
	}
}

func (module Module) handleReview(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
	proposalID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodPost) {
		return
	}
	var body contract.ReviewContextProposalRequest
	if !httpx.DecodeJSON(response, request, &body) {
		return
	}
	if err := body.Validate(); err != nil {
		writeDataError(response, request, ErrInvalid)
		return
	}
	proposal, err := module.Service.ReviewProposal(
		request.Context(), identity, projectID, proposalID,
		ReviewProposalInput{Decision: body.Decision, Note: optionalString(body.ReviewNote)},
	)
	if err != nil {
		writeDataError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, proposal)
}

func (module Module) handleHome(
	response http.ResponseWriter,
	request *http.Request,
	identity auth.Identity,
	projectID string,
) {
	if !httpx.RequireMethod(response, request, http.MethodGet) {
		return
	}
	home, err := module.Service.Home(request.Context(), identity, projectID)
	if err != nil {
		writeDataError(response, request, err)
		return
	}
	httpx.WriteJSON(response, http.StatusOK, home)
}

func pageRequest(request *http.Request) (pagination.Request, bool) {
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

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalStrings(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}

func writeDataError(response http.ResponseWriter, request *http.Request, err error) {
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
			http.StatusForbidden, "FORBIDDEN", "Project permission denied",
		))
	case errors.Is(err, ErrHumanRequired):
		httpx.WriteError(response, request, apperror.New(
			http.StatusForbidden, "HUMAN_REVIEW_REQUIRED",
			"A human browser session must confirm project context",
		))
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(response, request, apperror.New(
			http.StatusBadRequest, "INVALID_REQUEST", "Data Hub input is invalid",
		))
	case errors.Is(err, ErrConflict):
		httpx.WriteError(response, request, apperror.New(
			http.StatusConflict, "CONTEXT_PROPOSAL_ALREADY_REVIEWED",
			"The context proposal is no longer pending",
		))
	case errors.Is(err, ErrAdapterNotFound):
		httpx.WriteError(response, request, apperror.New(
			http.StatusNotImplemented, "DATA_ADAPTER_NOT_REGISTERED",
			"The object type does not have a full-content reader",
		))
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(response, request, apperror.New(
			http.StatusNotFound, "DATA_OBJECT_NOT_FOUND", "Data Hub record not found",
		))
	default:
		httpx.WriteError(response, request, err)
	}
}
