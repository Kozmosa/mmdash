package repo

import (
	"context"
	"time"
)

// MetricSink is the low-cardinality observability boundary owned by Core.
type MetricSink interface {
	ObserveRepoOperation(string, string, string, time.Duration)
	SetRepoGauges(int64, int64, int64)
}

// RepoGaugeSnapshot contains only aggregate counts.
type RepoGaugeSnapshot struct {
	CheckoutsActive int64
	SyncQueueDepth  int64
}

// RepoGaugeStore loads aggregate Repo state without identifiers or paths.
type RepoGaugeStore interface {
	RepoGaugeSnapshot(context.Context, time.Time) (RepoGaugeSnapshot, error)
}

// RepoStorageSizer reports aggregate bytes below the managed root.
type RepoStorageSizer interface {
	Size() (int64, error)
}

// MetricsCollector refreshes Repo gauges outside request paths.
type MetricsCollector struct {
	Clock    interface{ Now() time.Time }
	Interval time.Duration
	Metrics  MetricSink
	OnError  func(error)
	Storage  RepoStorageSizer
	Store    RepoGaugeStore
}

func (collector MetricsCollector) Run(ctx context.Context) {
	interval := collector.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := collector.RunOnce(ctx); err != nil && collector.OnError != nil {
				collector.OnError(err)
			}
			timer.Reset(interval)
		}
	}
}

func (collector MetricsCollector) RunOnce(ctx context.Context) error {
	if collector.Clock == nil ||
		collector.Metrics == nil ||
		collector.Storage == nil ||
		collector.Store == nil {
		return ErrInvalid
	}
	snapshot, err := collector.Store.RepoGaugeSnapshot(
		ctx,
		collector.Clock.Now().UTC(),
	)
	if err != nil {
		return err
	}
	storageBytes, err := collector.Storage.Size()
	if err != nil {
		return err
	}
	collector.Metrics.SetRepoGauges(
		snapshot.SyncQueueDepth,
		snapshot.CheckoutsActive,
		storageBytes,
	)
	return nil
}
