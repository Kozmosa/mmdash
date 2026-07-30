package repo

import (
	"errors"
	"fmt"
)

var (
	ErrAlreadyConnected  = errors.New("repository already connected")
	ErrBranchMapping     = errors.New("repository branch mapping invalid")
	ErrCheckoutNotFound  = errors.New("repository checkout not found")
	ErrConflict          = errors.New("repository conflict")
	ErrHeadChanged       = errors.New("repository head changed")
	ErrInvalid           = errors.New("invalid repository input")
	ErrLocked            = errors.New("repository locked")
	ErrNoChanges         = errors.New("repository commit has no changes")
	ErrNotConfigured     = errors.New("repository not configured")
	ErrNotReady          = errors.New("repository not ready")
	ErrObjectNotFound    = errors.New("repository object not found")
	ErrReconnectExpired  = errors.New("repository reconnect window expired")
	ErrReconnectMismatch = errors.New("repository reconnect remote mismatch")
	ErrWebhookConflict   = errors.New("repository webhook delivery conflict")
	ErrWebhookSignature  = errors.New("repository webhook signature invalid")
	ErrWorktreeDirty     = errors.New("repository worktree is dirty")
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
