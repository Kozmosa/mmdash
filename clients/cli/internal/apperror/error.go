package apperror

import (
	"errors"
	"fmt"
)

type Error struct {
	Code      string `json:"code"`
	ExitCode  int    `json:"-"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
	Retryable bool   `json:"retryable"`
	Cause     error  `json:"-"`
}

func (err *Error) Error() string { return err.Message }
func (err *Error) Unwrap() error { return err.Cause }

func New(code string, message string, exitCode int) *Error {
	return &Error{Code: code, ExitCode: exitCode, Message: message}
}

func Wrap(code string, message string, exitCode int, cause error) *Error {
	return &Error{Code: code, ExitCode: exitCode, Message: message, Cause: cause}
}

func Normalize(err error) *Error {
	var typed *Error
	if errors.As(err, &typed) {
		return typed
	}
	return Wrap("INTERNAL_ERROR", "The command could not be completed", 1, err)
}

func Usage(format string, values ...interface{}) *Error {
	return New("INVALID_ARGUMENT", fmt.Sprintf(format, values...), 2)
}
