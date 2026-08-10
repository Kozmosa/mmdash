package boxcontrol

import (
	"context"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/project"
)

type Store interface {
	Create(context.Context, Box, string) error
	Get(context.Context, string) (Box, error)
	List(context.Context, string) ([]Box, error)
	UpdateHeartbeat(context.Context, string, Box, time.Time) (Box, error)
	MarkOffline(context.Context, time.Time, time.Time, int) ([]Box, error)
	Bind(context.Context, string, string, time.Time) (Box, error)
	Unbind(context.Context, string, time.Time) error
	CreateTask(context.Context, Task) error
	GetTask(context.Context, string) (Task, error)
	ClaimTask(context.Context, string, time.Time, time.Duration) (*Task, error)
	RecoverExpired(context.Context, time.Time, int) ([]Task, error)
	RenewTask(context.Context, string, string, time.Time, time.Duration) (TaskLease, error)
	CancelTask(context.Context, string, time.Time) (Task, error)
	AppendLog(context.Context, Log) (Log, error)
	ReportStatus(context.Context, string, string, string, *int, string, string, map[string]interface{}, string, time.Time) (Task, error)
	SubmitResult(context.Context, string, string, Result, time.Time) (Task, error)
	ListLogs(context.Context, string, int, int) ([]Log, error)
}

type Access interface {
	Authenticate(context.Context, string) (auth.Identity, error)
	Authorize(context.Context, auth.Identity, string, project.Permission) error
}

type TokenIssuer interface {
	IssueToken(context.Context, auth.Identity, string, string, string, *time.Time) (auth.IssuedToken, error)
}

type TokenRevoker interface {
	RevokeToken(context.Context, auth.Identity, string) error
}

type IDGenerator interface{ New() (string, error) }

type Clock interface{ Now() time.Time }
