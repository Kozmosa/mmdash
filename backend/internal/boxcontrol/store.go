package boxcontrol

import (
	"context"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/platform/transaction"
	"github.com/mmdash/mmdash/backend/internal/project"
)

type Store interface {
	CreateInTransaction(context.Context, transaction.Tx, Box) error
	Get(context.Context, string) (Box, error)
	ListOwned(context.Context, string) ([]Box, error)
	ListProject(context.Context, string) ([]Box, error)
	UpdateName(context.Context, string, string, time.Time) (Box, error)
	UpdateHeartbeat(context.Context, string, Box, time.Time) (Box, bool, error)
	MarkOffline(context.Context, time.Time, time.Time, int) ([]Box, error)
	FailOfflineTimeouts(context.Context, time.Time, time.Time, int) ([]Task, error)
	BeginDrain(context.Context, string, time.Time) (Box, int, error)
	ForceRevoke(context.Context, string, time.Time) (Box, []Task, error)
	FinalizeDrained(context.Context, string, time.Time) (Box, bool, error)
	Assign(context.Context, ProjectBinding) (ProjectBinding, error)
	Unassign(context.Context, string, string, bool, time.Time) ([]Task, error)
	CreateTask(context.Context, Task) error
	GetTask(context.Context, string) (Task, error)
	ClaimTask(context.Context, string, time.Time) (*Task, error)
	ResumeTask(context.Context, string, string, ResumeRequest, time.Time) (Resume, error)
	CancelTask(context.Context, string, time.Time) (Task, error)
	AppendLogs(context.Context, string, string, LogBatch, time.Time) (LogAcknowledgement, error)
	ReportStatus(context.Context, string, string, string, string, time.Time, *int, *Failure, map[string]interface{}, string) (Task, error)
	SubmitResult(context.Context, string, string, Result, time.Time) (Task, error)
	ListLogs(context.Context, string, int64, int) ([]Log, bool, error)
}

type Access interface {
	Authenticate(context.Context, string) (auth.Identity, error)
	Authorize(context.Context, auth.Identity, string, project.Permission) error
}

type BoxCredentialIssuer interface {
	IssueBoxTokenFromRegistrationGrant(
		context.Context,
		string,
		string,
		func(context.Context, transaction.Tx, auth.Token) error,
	) (auth.IssuedToken, error)
}

type BoxCredentialRevoker interface {
	RevokeBoxToken(context.Context, string, string) error
}

type IDGenerator interface{ New() (string, error) }

type Clock interface{ Now() time.Time }
