package agent

import "testing"

func TestParseArtifactSemanticOutputRejectsProseAndUnknownFields(t *testing.T) {
	valid, err := parseArtifactSemanticOutput(`{"description":"A fixed table.","recommended_usage":["Cite in Methods"]}`)
	if err != nil || valid.Description != "A fixed table." {
		t.Fatalf("valid output: %#v, %v", valid, err)
	}
	for _, output := range []string{
		"```json\n{\"description\":\"x\",\"recommended_usage\":[\"y\"]}\n```",
		`{"description":"x","recommended_usage":["y"],"secret":"no"}`,
		`{"description":"","recommended_usage":[]}`,
	} {
		if _, err := parseArtifactSemanticOutput(output); err == nil {
			t.Fatalf("expected rejection for %q", output)
		}
	}
}
