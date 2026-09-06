// Package gitcli provides the reviewed Git command and storage runtime.
package gitcli

import "errors"

var (
	ErrAuthentication      = errors.New("git authentication failed")
	ErrBranchNotFound      = errors.New("git remote branch was not found")
	ErrBranchInvalid       = errors.New("git branch is invalid")
	ErrCommandFailed       = errors.New("git command failed")
	ErrLocalDisabled       = errors.New("local git provider is disabled")
	ErrNetworkUnavailable  = errors.New("git network is unavailable")
	ErrOutputLimit         = errors.New("git command output limit exceeded")
	ErrPathInvalid         = errors.New("repository path is invalid")
	ErrProviderUnavailable = errors.New("git provider is temporarily unavailable")
	ErrRemoteNotFound      = errors.New("git remote was not found")
	ErrRemoteInvalid       = errors.New("git remote is invalid")
	ErrRevisionInvalid     = errors.New("git revision is invalid")
	ErrStorageEscape       = errors.New("managed repository path escaped storage root")
	ErrTimeout             = errors.New("git command timed out")
)
