package contract_test

import (
	"testing"
	"time"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
)

func TestGeneratedEventEnvelopeValidation(t *testing.T) {
	event := contract.EventEnvelope{
		EventID:       "d173b13f-6c45-4d31-b9f5-884422aa3ce8",
		EventType:     "project.created",
		OccurredAt:    time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC),
		Payload:       map[string]interface{}{"name": "Modeling Team"},
		Producer:      "project",
		SchemaVersion: 1,
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("expected valid event envelope: %v", err)
	}
	event.EventType = "invalid"
	if err := event.Validate(); err == nil {
		t.Fatal("expected invalid event type to fail")
	}
}
