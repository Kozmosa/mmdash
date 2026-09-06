package repo

import (
	"context"
	"errors"

	"github.com/mmdash/mmdash/backend/internal/repo/gitcli"
	"github.com/mmdash/mmdash/backend/internal/repo/provider"
	"github.com/mmdash/mmdash/backend/internal/settings"
)

type failure struct {
	Code      string
	Message   string
	Outcome   string
	Retryable bool
}

func classifyProviderFailure(err error) failure {
	switch {
	case err == nil:
		return failure{}
	case errors.Is(err, provider.ErrAuthentication):
		return failure{Code: "REPO_AUTH_FAILED", Message: "Repository authentication failed", Outcome: "auth"}
	case errors.Is(err, provider.ErrBranchMissing):
		return failure{Code: "REPO_BRANCH_NOT_FOUND", Message: "A mapped repository branch was not found", Outcome: "not_found"}
	case errors.Is(err, provider.ErrRemoteNotFound):
		return failure{Code: "REPO_REMOTE_NOT_FOUND", Message: "Repository was not found", Outcome: "not_found"}
	case errors.Is(err, provider.ErrWritePermission):
		return failure{Code: "REPO_WRITE_PERMISSION_REQUIRED", Message: "Repository contents write permission is required", Outcome: "auth"}
	case errors.Is(err, provider.ErrNetworkUnavailable):
		return failure{Code: "REPO_NETWORK_UNAVAILABLE", Message: "External repository network is temporarily unavailable", Outcome: "network", Retryable: true}
	case errors.Is(err, provider.ErrTimeout):
		return failure{Code: "REPO_GIT_TIMEOUT", Message: "Repository operation timed out", Outcome: "timeout", Retryable: true}
	case errors.Is(err, provider.ErrTemporarilyUnavailable):
		return failure{Code: "REPO_PROVIDER_TEMPORARILY_UNAVAILABLE", Message: "GitHub is temporarily unavailable", Outcome: "provider_5xx", Retryable: true}
	case errors.Is(err, provider.ErrInvalidResponse):
		return failure{Code: "REPO_PROVIDER_RESPONSE_INVALID", Message: "GitHub returned an invalid response", Outcome: "error"}
	case errors.Is(err, provider.ErrUnsupported):
		return failure{Code: "REPO_PROVIDER_UNSUPPORTED", Message: "Repository provider is unsupported", Outcome: "error"}
	case errors.Is(err, provider.ErrUnavailable):
		return failure{Code: "REPO_PROVIDER_UNAVAILABLE", Message: "Repository provider is not enabled for this deployment", Outcome: "error"}
	case errors.Is(err, provider.ErrInvalidConfig):
		return failure{Code: "REPO_SETTINGS_INVALID", Message: "Repository settings are invalid", Outcome: "error"}
	default:
		return failure{Code: "REPO_CONNECTION_FAILED", Message: "Repository connection test failed", Outcome: "error"}
	}
}

func classifySyncFailure(err error) failure {
	var safeError *SafeError
	if errors.As(err, &safeError) {
		return failure{
			Code: safeError.Code, Message: safeError.Message,
			Outcome: failureOutcome(safeError.Code), Retryable: safeError.Retryable,
		}
	}
	providerFailure := classifyProviderFailure(err)
	if providerFailure.Code != "REPO_CONNECTION_FAILED" {
		return providerFailure
	}
	switch {
	case errors.Is(err, gitcli.ErrAuthentication):
		return failure{Code: "REPO_AUTH_FAILED", Message: "Repository authentication failed", Outcome: "auth"}
	case errors.Is(err, gitcli.ErrBranchNotFound):
		return failure{Code: "REPO_BRANCH_NOT_FOUND", Message: "A mapped repository branch was not found", Outcome: "not_found"}
	case errors.Is(err, gitcli.ErrRemoteNotFound):
		return failure{Code: "REPO_REMOTE_NOT_FOUND", Message: "Repository was not found", Outcome: "not_found"}
	case errors.Is(err, gitcli.ErrNetworkUnavailable):
		return failure{Code: "REPO_NETWORK_UNAVAILABLE", Message: "External repository network is temporarily unavailable", Outcome: "network", Retryable: true}
	case errors.Is(err, gitcli.ErrProviderUnavailable):
		return failure{Code: "REPO_PROVIDER_TEMPORARILY_UNAVAILABLE", Message: "GitHub is temporarily unavailable", Outcome: "provider_5xx", Retryable: true}
	case errors.Is(err, gitcli.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return failure{Code: "REPO_GIT_TIMEOUT", Message: "Repository operation timed out", Outcome: "timeout", Retryable: true}
	case errors.Is(err, settings.ErrNotFound), errors.Is(err, settings.ErrTypeNotFound):
		return failure{Code: "REPO_SETTINGS_NOT_FOUND", Message: "Repository settings are incomplete", Outcome: "error"}
	case errors.Is(err, settings.ErrInvalid):
		return failure{Code: "REPO_SETTINGS_INVALID", Message: "Repository settings are invalid", Outcome: "error"}
	case errors.Is(err, gitcli.ErrOutputLimit):
		return failure{Code: "REPO_GIT_OUTPUT_LIMIT", Message: "Repository command output exceeded its limit", Outcome: "error"}
	case errors.Is(err, gitcli.ErrPathInvalid), errors.Is(err, gitcli.ErrStorageEscape):
		return failure{Code: "REPO_STORAGE_INVALID", Message: "Repository storage failed a safety check", Outcome: "error"}
	case errors.Is(err, ErrWorktreeDirty):
		return failure{Code: "REPO_WORKTREE_DIRTY", Message: "A managed repository worktree contains changes", Outcome: "error"}
	default:
		return failure{Code: "REPO_SYNC_FAILED", Message: "Repository synchronization failed", Outcome: "error", Retryable: true}
	}
}

func failureOutcome(code string) string {
	switch code {
	case "REPO_AUTH_FAILED", "REPO_WRITE_PERMISSION_REQUIRED":
		return "auth"
	case "REPO_BRANCH_NOT_FOUND", "REPO_REMOTE_NOT_FOUND":
		return "not_found"
	case "REPO_NETWORK_UNAVAILABLE":
		return "network"
	case "REPO_GIT_TIMEOUT":
		return "timeout"
	case "REPO_PROVIDER_TEMPORARILY_UNAVAILABLE":
		return "provider_5xx"
	default:
		return "error"
	}
}

func retryableFailureCode(code string) bool {
	switch code {
	case "REPO_NETWORK_UNAVAILABLE", "REPO_GIT_TIMEOUT",
		"REPO_PROVIDER_TEMPORARILY_UNAVAILABLE", "REPO_SYNC_FAILED":
		return true
	default:
		return false
	}
}
