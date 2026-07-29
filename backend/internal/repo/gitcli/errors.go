// Package gitcli provides the reviewed Git command and storage runtime.
package gitcli

import "errors"

var (
	ErrAuthentication  = errors.New("git authentication failed")
	ErrBranchInvalid   = errors.New("git branch is invalid")
	ErrCommandFailed   = errors.New("git command failed")
	ErrLocalDisabled   = errors.New("local git provider is disabled")
	ErrOutputLimit     = errors.New("git command output limit exceeded")
	ErrPathInvalid     = errors.New("repository path is invalid")
	ErrRemoteInvalid   = errors.New("git remote is invalid")
	ErrRevisionInvalid = errors.New("git revision is invalid")
	ErrStorageEscape   = errors.New("managed repository path escaped storage root")
	ErrTimeout         = errors.New("git command timed out")
)
