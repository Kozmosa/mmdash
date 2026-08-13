package artifact

import (
	"errors"
	"testing"

	"github.com/mmdash/mmdash/backend/internal/jobs"
)

func TestParseSemanticResultRequiresStrictBoundedFields(t *testing.T) {
	valid := map[string]interface{}{
		"description":       "fixed experiment chart",
		"recommended_usage": []interface{}{"cite in Results", "compare against baseline"},
		"agent_session_id":  "11111111-1111-4111-8111-111111111111",
		"agent_run_id":      "22222222-2222-4222-8222-222222222222",
	}
	result, err := parseSemanticResult(valid)
	if err != nil || result.Description != "fixed experiment chart" || len(result.RecommendedUsage) != 2 {
		t.Fatalf("parse valid result: %#v, %v", result, err)
	}
	invalid := map[string]interface{}{
		"description": "", "recommended_usage": []interface{}{},
		"agent_session_id": valid["agent_session_id"], "agent_run_id": valid["agent_run_id"],
	}
	if _, err := parseSemanticResult(invalid); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatalf("expected invalid semantic result, got %v", err)
	}
}

func TestSemanticTargetPinsCurrentArtifactVersion(t *testing.T) {
	job := jobs.Job{ID: "33333333-3333-4333-8333-333333333333", JobType: semanticDescriptionJobType, ProjectID: "44444444-4444-4444-8444-444444444444", Payload: map[string]interface{}{
		"project_id":        "44444444-4444-4444-8444-444444444444",
		"artifact_id":       "55555555-5555-4555-8555-555555555555",
		"version_id":        "66666666-6666-4666-8666-666666666666",
		"agent_instance_id": "77777777-7777-4777-8777-777777777777",
	}}
	target, err := semanticTarget(job)
	if err != nil || target.VersionID != "66666666-6666-4666-8666-666666666666" {
		t.Fatalf("semantic target: %#v, %v", target, err)
	}
}
