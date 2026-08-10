package progress

import (
	"context"
	"testing"
	"time"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
)

type trackingEventStoreStub struct {
	TrackingStore
	events []contract.EventEnvelope
}

func (store *trackingEventStoreStub) ScheduleEvent(_ context.Context, event contract.EventEnvelope, _ string) error {
	store.events = append(store.events, event)
	return nil
}

func TestTrackingEventIgnoresProgressEvaluationAgentRun(t *testing.T) {
	projectID := "00000000-0000-4000-8000-000000000001"
	store := &trackingEventStoreStub{}
	service := Service{Tracking: store}
	event := contract.EventEnvelope{
		EventID: "00000000-0000-4000-8000-000000000002", EventType: "agent.run.completed",
		OccurredAt: time.Now().UTC(), Payload: map[string]interface{}{
			"resource_id": "00000000-0000-4000-8000-000000000003",
			"source":      "progress_evaluation",
		}, ProjectID: &projectID,
	}
	if err := service.HandleTrackingEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 0 {
		t.Fatalf("Progress evaluation Agent Run retriggered tracking: %#v", store.events)
	}
	event.Payload["source"] = "message"
	if err := service.HandleTrackingEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 1 {
		t.Fatalf("ordinary Agent Run was not scheduled: %#v", store.events)
	}
}

func TestDecodeEvaluationResultRejectsDuplicateSuggestionKeys(t *testing.T) {
	_, err := decodeEvaluationResult(map[string]interface{}{"output": map[string]interface{}{
		"stage": "execution", "summary": "summary", "changes_since_last": []string{},
		"completed_items": []string{}, "in_progress_items": []string{}, "blockers": []string{},
		"risks": []interface{}{}, "pending_questions": []string{},
		"suggestions": []interface{}{
			map[string]interface{}{"key": "same", "proposal_type": "task.create", "title": "one", "rationale": "", "changes": map[string]interface{}{"title": "one"}},
			map[string]interface{}{"key": "same", "proposal_type": "task.create", "title": "two", "rationale": "", "changes": map[string]interface{}{"title": "two"}},
		},
	}})
	if err != ErrInvalidEvaluationOutput {
		t.Fatalf("duplicate suggestion keys returned %v", err)
	}
}
