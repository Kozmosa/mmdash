package boxcontrol

import (
	"strings"
	"testing"
)

func TestValidateBoxRejectsUnsupportedRuntimeAndUnsafeLimits(t *testing.T) {
	base := Box{
		ProjectID: "project-1", Name: "box", Version: "1", Capabilities: []Capability{{Name: "sandbox", Version: "1"}},
		Runtimes: []Runtime{{Name: "local-docker", Version: "1"}}, Limits: ResourceLimits{CPUMillis: 500, MemoryBytes: 1 << 20, TimeoutSecond: 30, DiskBytes: 1 << 20, PIDs: 32, Network: "disabled"},
	}
	if err := validateBox(&base, "project-1"); err != nil {
		t.Fatalf("valid Box rejected: %v", err)
	}
	base.Runtimes[0].Name = "arbitrary-shell"
	if err := validateBox(&base, "project-1"); err == nil {
		t.Fatal("unsupported runtime accepted")
	}
	base.Runtimes[0].Name = "local-docker"
	base.Limits.Network = "public"
	if err := validateBox(&base, "project-1"); err == nil {
		t.Fatal("unsupported network policy accepted")
	}
}

func TestArtifactPointerValidationRejectsForgedMetadata(t *testing.T) {
	valid := map[string]interface{}{
		"artifact_id": "00000000-0000-4000-8000-000000000001", "version_id": "00000000-0000-4000-8000-000000000002",
		"filename": "artifact.zip", "sha256": strings.Repeat("a", 64), "size_bytes": int64(1),
	}
	if !validArtifactPointer(valid) {
		t.Fatal("valid artifact pointer rejected")
	}
	for name, value := range map[string]interface{}{
		"wrong filename": func() map[string]interface{} {
			copy := cloneMap(valid)
			copy["filename"] = "result_manifest.json"
			return copy
		}(),
		"zero size": func() map[string]interface{} { copy := cloneMap(valid); copy["size_bytes"] = int64(0); return copy }(),
		"bad hash": func() map[string]interface{} {
			copy := cloneMap(valid)
			copy["sha256"] = strings.Repeat("g", 64)
			return copy
		}(),
		"fake id": func() map[string]interface{} {
			copy := cloneMap(valid)
			copy["artifact_id"] = "artifact-1"
			return copy
		}(),
	} {
		if validArtifactPointer(value.(map[string]interface{})) {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func cloneMap(value map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{}, len(value))
	for key, item := range value {
		copy[key] = item
	}
	return copy
}
