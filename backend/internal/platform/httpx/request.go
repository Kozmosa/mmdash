package httpx

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/mmdash/mmdash/backend/internal/platform/apperror"
)

const maximumJSONBodyBytes = 1024 * 1024

// Validator is implemented by generated request DTOs.
type Validator interface {
	Validate() error
}

// DecodeJSON enforces one bounded, strict JSON value and generated validation.
func DecodeJSON(response http.ResponseWriter, request *http.Request, target interface{}) bool {
	return DecodeJSONLimit(
		response, request, target, maximumJSONBodyBytes,
	)
}

// DecodeJSONLimit applies the same strict decoding with an operation-specific
// bound for contracts whose encoded representation is intentionally larger.
func DecodeJSONLimit(
	response http.ResponseWriter,
	request *http.Request,
	target interface{},
	maximumBytes int64,
) bool {
	if maximumBytes < 1 {
		writeInvalidRequest(response, request)
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(
		response,
		request.Body,
		maximumBytes,
	))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeInvalidRequest(response, request)
		return false
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeInvalidRequest(response, request)
		return false
	}
	if validator, ok := target.(Validator); ok && validator.Validate() != nil {
		writeInvalidRequest(response, request)
		return false
	}
	return true
}

func writeInvalidRequest(response http.ResponseWriter, request *http.Request) {
	WriteError(response, request, apperror.New(
		http.StatusBadRequest,
		"INVALID_REQUEST",
		"Request body is invalid",
	))
}
