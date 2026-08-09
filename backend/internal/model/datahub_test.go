package model

import (
	"context"
	"errors"
	"testing"
	"time"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
)

type dataHubStoreStub struct {
	Store
	source Source
}

func (store dataHubStoreStub) GetSource(context.Context, string) (Source, error) {
	return store.source, nil
}

type dataHubSinkStub struct {
	DataHubProjectionSink
	source SourceProjection
	calls  int
}

func (sink *dataHubSinkStub) ProjectModelSource(_ context.Context, _ contract.EventEnvelope, item SourceProjection) error {
	sink.calls++
	sink.source = item
	return nil
}

func TestDataHubProjectorProjectsSingleModelSource(t *testing.T) {
	projectID := "00000000-0000-4000-8000-000000000001"
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	sink := &dataHubSinkStub{}
	projector := DataHubProjector{
		Store: dataHubStoreStub{source: Source{
			ID: "00000000-0000-4000-8000-000000000002", ProjectID: projectID,
			NotionRootPageID: "00000000-0000-4000-8000-000000000003", NotionRootTitle: "Models",
			AutoSyncEnabled: true, AutoSyncIntervalSeconds: 300, SyncStatus: SyncSucceeded,
			DiscoveredPageCount: 4,
		}},
		Sink: sink,
	}
	event := contract.EventEnvelope{
		EventID: "00000000-0000-4000-8000-000000000004", EventType: "model.source.changed",
		ProjectID: &projectID, OccurredAt: now,
		Payload: map[string]interface{}{
			"source_id": "00000000-0000-4000-8000-000000000002",
			"status":    "active",
		},
	}

	if err := projector.Project(context.Background(), event); err != nil {
		t.Fatalf("project source: %v", err)
	}
	if sink.calls != 1 || sink.source.RootTitle != "Models" || sink.source.DiscoveredPageCount != 4 || sink.source.Status != "active" {
		t.Fatalf("unexpected source projection: %#v", sink.source)
	}
}

func TestDataHubProjectorRejectsMismatchedSourceEvent(t *testing.T) {
	projectID := "00000000-0000-4000-8000-000000000001"
	projector := DataHubProjector{
		Store: dataHubStoreStub{source: Source{ID: "00000000-0000-4000-8000-000000000002", ProjectID: projectID}},
		Sink:  &dataHubSinkStub{},
	}
	event := contract.EventEnvelope{
		EventType: "model.source.changed", ProjectID: &projectID,
		Payload: map[string]interface{}{"source_id": "00000000-0000-4000-8000-000000000099", "status": "active"},
	}
	if err := projector.Project(context.Background(), event); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mismatched source error = %v", err)
	}
}
