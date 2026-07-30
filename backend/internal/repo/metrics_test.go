package repo

import (
	"context"
	"testing"
	"time"

	"github.com/mmdash/mmdash/backend/internal/platform/clock"
)

type gaugeStoreFixture struct {
	snapshot RepoGaugeSnapshot
}

func (store gaugeStoreFixture) RepoGaugeSnapshot(
	context.Context,
	time.Time,
) (RepoGaugeSnapshot, error) {
	return store.snapshot, nil
}

type storageSizerFixture struct {
	bytes int64
}

func (storage storageSizerFixture) Size() (int64, error) {
	return storage.bytes, nil
}

type metricSinkFixture struct {
	checkouts int64
	queue     int64
	storage   int64
}

func (*metricSinkFixture) ObserveRepoOperation(
	string,
	string,
	string,
	time.Duration,
) {
}

func (sink *metricSinkFixture) SetRepoGauges(queue, checkouts, storage int64) {
	sink.queue = queue
	sink.checkouts = checkouts
	sink.storage = storage
}

func TestMetricsCollectorPublishesAggregateGauges(t *testing.T) {
	sink := &metricSinkFixture{}
	collector := MetricsCollector{
		Clock:   clock.Fixed{Time: time.Now()},
		Metrics: sink,
		Storage: storageSizerFixture{bytes: 8192},
		Store: gaugeStoreFixture{snapshot: RepoGaugeSnapshot{
			CheckoutsActive: 2,
			SyncQueueDepth:  4,
		}},
	}
	if err := collector.RunOnce(context.Background()); err != nil {
		t.Fatalf("collect Repo metrics: %v", err)
	}
	if sink.queue != 4 || sink.checkouts != 2 || sink.storage != 8192 {
		t.Fatalf("unexpected metrics: %+v", sink)
	}
}
