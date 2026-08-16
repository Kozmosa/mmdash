package datahub

import (
	"testing"

	contract "github.com/mmdash/mmdash/backend/internal/contract/generated"
)

func TestStage8ProjectionIncludesExperimentRunAndResultBundleCards(t *testing.T) {
	projectID := "00000000-0000-4000-8000-000000000001"
	objects := stage8Objects(contract.EventEnvelope{
		EventType: "experiment.succeeded",
		ProjectID: &projectID,
		Payload: map[string]interface{}{
			"experiment_id":     "00000000-0000-4000-8000-000000000002",
			"task_id":           "00000000-0000-4000-8000-000000000003",
			"name":              "sweep",
			"execution_status":  "succeeded",
			"result_commit_sha": "0123456789012345678901234567890123456789",
		},
	})
	if len(objects) != 3 {
		t.Fatalf("stage8 objects = %#v", objects)
	}
	if objects[0].objectType != "experiment" || objects[1].objectType != "experiment_run" || objects[2].objectType != "result_bundle" {
		t.Fatalf("unexpected object types: %#v", objects)
	}
	if objects[1].sourceID != "00000000-0000-4000-8000-000000000003" || objects[2].sourceID != "00000000-0000-4000-8000-000000000002" {
		t.Fatalf("unexpected source IDs: %#v", objects)
	}
}

func TestStage8ProjectionDoesNotCreateSuccessfulResultForFailure(t *testing.T) {
	objects := stage8Objects(contract.EventEnvelope{EventType: "experiment.failed", Payload: map[string]interface{}{
		"experiment_id": "00000000-0000-4000-8000-000000000002", "task_id": "00000000-0000-4000-8000-000000000003", "execution_status": "failed",
	}})
	for _, object := range objects {
		if object.objectType == "result_bundle" {
			t.Fatal("failed experiment created a result bundle")
		}
	}
}

func TestStage8ProjectionMarksRevokedBox(t *testing.T) {
	projectID := "00000000-0000-4000-8000-000000000001"
	objects := stage8Objects(contract.EventEnvelope{EventType: "box.revoked", ProjectID: &projectID, Payload: map[string]interface{}{
		"box_id": "00000000-0000-4000-8000-000000000004", "name": "retired", "status": "revoked", "version": "1",
	}})
	if len(objects) != 1 || objects[0].objectType != "box" || objects[0].status != "revoked" || objects[0].title != "retired" {
		t.Fatalf("unexpected revoked Box projection: %#v", objects)
	}
	if objects[0].sourceID != "00000000-0000-4000-8000-000000000004@"+projectID {
		t.Fatalf("Box projection key = %q", objects[0].sourceID)
	}
}

func TestStage8ProjectionRemovesUnassignedProjectBox(t *testing.T) {
	projectID := "00000000-0000-4000-8000-000000000001"
	objects := stage8Objects(contract.EventEnvelope{EventType: "box.unassigned", ProjectID: &projectID, Payload: map[string]interface{}{
		"box_id": "00000000-0000-4000-8000-000000000004", "project_id": projectID, "mode": "force",
	}})
	if len(objects) != 1 || !objects[0].delete || objects[0].status != "unassigned" {
		t.Fatalf("unexpected unassigned projection: %#v", objects)
	}
}
