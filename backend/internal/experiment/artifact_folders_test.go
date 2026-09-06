package experiment

import (
	"reflect"
	"testing"
	"time"
)

func TestExperimentArtifactFolderUsesFrozenSourceCommitAndCreationTime(t *testing.T) {
	commitSHA := "fedcba9876543210fedcba9876543210fedcba98"
	item := Experiment{
		SourceCommit: commitSHA,
		CreatedAt: time.Date(
			2026, time.September, 2, 20, 30, 45, 654321000,
			time.FixedZone("CST", 8*60*60),
		),
	}
	want := []string{
		"experiment",
		commitSHA + "_20260902T123045.654321Z",
	}
	if got := experimentArtifactFolder(item); !reflect.DeepEqual(got, want) {
		t.Fatalf("folder: got %#v want %#v", got, want)
	}
}

func TestExperimentArtifactFolderRejectsMissingSourceCommit(t *testing.T) {
	if got := experimentArtifactFolder(Experiment{
		CreatedAt: time.Date(2026, time.September, 2, 13, 14, 15, 0, time.UTC),
	}); got != nil {
		t.Fatalf("expected no managed path without a source commit, got %v", got)
	}
}
