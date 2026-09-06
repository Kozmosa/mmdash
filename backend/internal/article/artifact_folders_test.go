package article

import (
	"reflect"
	"testing"
	"time"
)

func TestArticleBuildArtifactFolderUsesStableBuildIdentity(t *testing.T) {
	createdAt := time.Date(2026, time.September, 2, 13, 14, 15, 123456000, time.FixedZone("CST", 8*60*60))
	commitSHA := "0123456789abcdef0123456789abcdef01234567"
	tests := []struct {
		build Build
		want  []string
	}{
		{
			build: Build{BuildKind: BuildFormal, CommitSHA: commitSHA, CreatedAt: createdAt},
			want:  []string{"article", "build", commitSHA + "_20260902T051415.123456Z"},
		},
		{
			build: Build{BuildKind: BuildPreview, CreatedAt: createdAt},
			want:  []string{"article", "draft", "20260902T051415.123456Z"},
		},
		{
			build: Build{BuildKind: BuildTemplateTest, CreatedAt: createdAt},
			want:  []string{"article", "template", "20260902T051415.123456Z"},
		},
	}
	for _, test := range tests {
		if got := articleBuildArtifactFolder(test.build); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("folder for %s: got %#v want %#v", test.build.BuildKind, got, test.want)
		}
	}
}

func TestArticleBuildArtifactFolderRejectsFormalBuildWithoutCommit(t *testing.T) {
	if got := articleBuildArtifactFolder(Build{
		BuildKind: BuildFormal,
		CreatedAt: time.Date(2026, time.September, 2, 13, 14, 15, 0, time.UTC),
	}); got != nil {
		t.Fatalf("expected no managed path without a commit, got %v", got)
	}
}
