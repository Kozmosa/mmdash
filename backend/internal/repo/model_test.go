package repo

import (
	"errors"
	"testing"
)

func TestConnectionSnapshotRequiresThreeDistinctBranches(t *testing.T) {
	valid := ConnectionSnapshot{
		CanonicalRemoteURL: "https://github.com/Kozmosa/mmdash",
		DefaultBranch:      "main",
		DisplayName:        "Kozmosa/mmdash",
		ProjectID:          "71818682-08ca-4a6f-8d28-2ca2fd05f39f",
		Provider:           ProviderGitHub,
		SettingsVersion:    1,
		Workspaces: WorkspaceMappings{
			CodeBranch: "main", ArticleBranch: "article", ResultBranch: "result",
		},
	}
	if err := validateSnapshot(valid); err != nil {
		t.Fatalf("valid snapshot: %v", err)
	}
	invalid := valid
	invalid.Workspaces.ArticleBranch = "main"
	if !errors.Is(validateSnapshot(invalid), ErrBranchMapping) {
		t.Fatal("duplicate workspace branches must be rejected")
	}
}
