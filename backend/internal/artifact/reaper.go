package artifact

import (
	"context"
	"time"
)

// UploadExpirer aborts bounded batches of expired staging sessions.
type UploadExpirer interface {
	ExpireUploads(context.Context, int) (int, error)
}

// UploadReaper removes only unconfirmed staging state. It never scans or
// deletes available Artifact content-addressed objects.
type UploadReaper struct {
	Interval time.Duration
	Limit    int
	Metrics  MetricRecorder
	OnError  func(error)
	Service  UploadExpirer
	Backend  string
}

func (reaper UploadReaper) Run(ctx context.Context) {
	interval := reaper.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			started := time.Now()
			_, err := reaper.RunOnce(ctx)
			outcome := "success"
			if err != nil {
				outcome = "error"
				if reaper.OnError != nil {
					reaper.OnError(err)
				}
			}
			if reaper.Metrics != nil {
				reaper.Metrics.ObserveArtifactOperation(
					"expire", outcome, reaper.Backend, time.Since(started),
				)
			}
			timer.Reset(interval)
		}
	}
}

func (reaper UploadReaper) RunOnce(ctx context.Context) (int, error) {
	if reaper.Service == nil {
		return 0, ErrInvalid
	}
	limit := reaper.Limit
	if limit < 1 {
		limit = 50
	}
	return reaper.Service.ExpireUploads(ctx, limit)
}
