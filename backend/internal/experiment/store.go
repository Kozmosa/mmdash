package experiment

import (
	"context"
	"time"

	"github.com/mmdash/mmdash/backend/internal/boxcontrol"
)

type Store interface {
	GetSettings(context.Context, string) (Settings, error)
	UpdateSettings(context.Context, string, string, SettingsPatch, time.Time) (Settings, error)
	Create(context.Context, Experiment) (Experiment, bool, error)
	CreateRerun(context.Context, Experiment, Experiment, time.Time) (Experiment, bool, error)
	Get(context.Context, string, string) (Experiment, error)
	List(context.Context, string, string, int, int) (Page, error)
	QueueWithTask(context.Context, Experiment, boxcontrol.Task, string, time.Time) (Experiment, error)
	Cancel(context.Context, string, string, time.Time) (Experiment, error)
	Archive(context.Context, string, string, time.Time) (Experiment, error)
	ApplyTaskStatus(context.Context, boxcontrol.Task, time.Time) (Experiment, error)
	ApplyResult(context.Context, boxcontrol.Task, boxcontrol.Result, time.Time) (Experiment, error)
	BeginSelfBinding(context.Context, string, string, string, string, time.Time) (Experiment, error)
	CompleteResult(context.Context, string, string, ResultVerification, time.Time) (Experiment, error)
	FailResult(context.Context, string, string, Failure, time.Time) (Experiment, error)
	Result(context.Context, string, string) (ResultBundle, error)
	Compare(context.Context, string, []string) (Comparison, error)
}
