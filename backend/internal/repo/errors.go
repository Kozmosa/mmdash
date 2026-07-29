package repo

import (
	"errors"
	"fmt"
)

var (
	ErrAlreadyConnected = errors.New("repository already connected")
	ErrBranchMapping    = errors.New("repository branch mapping invalid")
	ErrCheckoutNotFound = errors.New("repository checkout not found")
	ErrConflict         = errors.New("repository conflict")
	ErrInvalid          = errors.New("invalid repository input")
	ErrLocked           = errors.New("repository locked")
	ErrNotConfigured    = errors.New("repository not configured")
	ErrNotReady         = errors.New("repository not ready")
	ErrObjectNotFound   = errors.New("repository object not found")
	ErrWorktreeDirty    = errors.New("repository worktree is dirty")
)

// SafeError carries a stable public code while retaining no provider details.
type SafeError struct {
	Code    string
	Message string
}

func (err *SafeError) Error() string {
	return err.Code + ": " + err.Message
}

func safe(code, message string) error {
	return &SafeError{Code: code, Message: message}
}

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
