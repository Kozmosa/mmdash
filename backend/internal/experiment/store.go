package experiment

import (
	"context"
	"time"

	"github.com/mmdash/mmdash/backend/internal/auth"
	"github.com/mmdash/mmdash/backend/internal/boxcontrol"
)

type Store interface {
	Create(context.Context, Experiment) (Experiment, bool, error)
	Get(context.Context, string, string) (Experiment, error)
	List(context.Context, string, string, int, int) (Page, error)
	Queue(context.Context, string, string, time.Time) (Experiment, error)
	Cancel(context.Context, string, string, time.Time) (Experiment, error)
	Archive(context.Context, string, string, time.Time) (Experiment, error)
	ApplyTaskStatus(context.Context, boxcontrol.Task, time.Time) (Experiment, error)
	ApplyResult(context.Context, boxcontrol.Task, boxcontrol.Result, time.Time) (Experiment, error)
	Compare(context.Context, string, []string) (Comparison, error)
}

type QueueCoordinator interface {
	QueueWithTask(context.Context, Experiment, boxcontrol.Task, time.Time) (Experiment, error)
}

type Access interface {
	Authenticate(context.Context, string) (auth.Identity, error)
}
