// Package apperror defines stable, user-safe application errors.
package apperror

import "fmt"

// Error is the shared Core error representation.
type Error struct {
	Cause   error
	Code    string
	Details interface{}
	Message string
	Status  int
}

// New constructs a safe application error.
func New(status int, code, message string) *Error {
	return &Error{Code: code, Message: message, Status: status}
}

// Error returns the stable code and safe message.
func (err *Error) Error() string {
	return fmt.Sprintf("%s: %s", err.Code, err.Message)
}

// Unwrap exposes the internal cause for errors.Is/errors.As without serializing it.
func (err *Error) Unwrap() error {
	return err.Cause
}

// WithCause attaches an internal cause.
func (err *Error) WithCause(cause error) *Error {
	err.Cause = cause
	return err
}

// WithDetails attaches structured, caller-safe details.
func (err *Error) WithDetails(details interface{}) *Error {
	err.Details = details
	return err
}
