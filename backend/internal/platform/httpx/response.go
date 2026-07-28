// Package httpx provides Core's stable JSON response and error envelope.
package httpx

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
	"github.com/mmdash/mmdash/backend/internal/platform/requestctx"
)

// ErrorBody is the public Core error envelope.
type ErrorBody struct {
	Code      string      `json:"code"`
	Details   interface{} `json:"details,omitempty"`
	Message   string      `json:"message"`
	RequestID string      `json:"request_id,omitempty"`
}

// WriteJSON serializes a JSON response.
func WriteJSON(response http.ResponseWriter, status int, body interface{}) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}

// WriteError converts application and unexpected errors without leaking causes.
func WriteError(response http.ResponseWriter, request *http.Request, err error) {
	var applicationError *apperror.Error
	if !errors.As(err, &applicationError) {
		applicationError = apperror.New(
			http.StatusInternalServerError,
			"INTERNAL_ERROR",
			"An unexpected error occurred",
		)
	}
	WriteJSON(response, applicationError.Status, ErrorBody{
		Code:      applicationError.Code,
		Details:   applicationError.Details,
		Message:   applicationError.Message,
		RequestID: requestctx.RequestID(request.Context()),
	})
}

// RequireMethod writes HTTP 405 and returns false for a method mismatch.
func RequireMethod(response http.ResponseWriter, request *http.Request, allowed string) bool {
	if request.Method == allowed {
		return true
	}
	response.Header().Set("Allow", allowed)
	WriteError(
		response,
		request,
		apperror.New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed"),
	)
	return false
}
