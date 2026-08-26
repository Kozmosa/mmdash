package artifact

import (
	"errors"
	"fmt"
)

var (
	ErrForbidden         = errors.New("artifact permission denied")
	ErrInvalid           = errors.New("invalid artifact input")
	ErrNotFound          = errors.New("artifact not found")
	ErrUploadConflict    = errors.New("artifact upload conflict")
	ErrUploadExpired     = errors.New("artifact upload expired")
	ErrUploadAborted     = errors.New("artifact upload aborted")
	ErrNotTrashed        = errors.New("artifact is not trashed")
	ErrPurgeConflict     = errors.New("artifact purge conflict")
	ErrNotAvailable      = errors.New("artifact is not available")
	ErrStorage           = errors.New("artifact storage unavailable")
	ErrPartInvalid       = errors.New("artifact multipart part invalid")
	ErrPartMissing       = errors.New("artifact multipart part missing")
	ErrUploadIncomplete  = errors.New("artifact upload incomplete")
	ErrSizeMismatch      = errors.New("artifact size mismatch")
	ErrHashMismatch      = errors.New("artifact hash mismatch")
	ErrTooLarge          = errors.New("artifact is too large")
	ErrKindInvalid       = errors.New("artifact kind invalid")
	ErrSourceInvalid     = errors.New("artifact source invalid")
	ErrTagInvalid        = errors.New("artifact tag invalid")
	ErrFolderConflict    = errors.New("artifact folder conflict")
	ErrFolderHasChildren = errors.New("artifact folder has child folders")
	ErrFolderCycle       = errors.New("artifact folder move would create a cycle")
	ErrTransferExpired   = errors.New("artifact transfer expired")
)

// SafeError carries a stable public code without leaking provider state.
type SafeError struct {
	Code    string
	Message string
	Cause   error
}

func (err *SafeError) Error() string {
	return err.Code + ": " + err.Message
}

func (err *SafeError) Unwrap() error {
	return err.Cause
}

func safe(code, message string, cause error) error {
	return &SafeError{Code: code, Message: message, Cause: cause}
}

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
