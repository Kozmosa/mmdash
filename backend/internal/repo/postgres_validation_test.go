package repo

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSyncWorkspacesAcceptsOneRequestedBranch(t *testing.T) {
	sha := strings.Repeat("a", 40)
	tree := strings.Repeat("b", 40)
	result := SyncResult{Workspaces: []SyncedWorkspace{{
		Workspace: WorkspaceArticle, Status: WorkspaceReady,
		HeadCommitSHA: sha, TreeSHA: tree,
	}}}
	items, expected, err := validateSyncWorkspaces(
		SyncClaim{Workspaces: []WorkspaceKind{WorkspaceArticle}}, result,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || len(expected) != 1 || expected[0] != WorkspaceArticle {
		t.Fatalf("partial sync was not preserved: %#v %#v", items, expected)
	}
}

func TestValidateSyncWorkspacesRequiresAllBranchesForInitialSync(t *testing.T) {
	sha := strings.Repeat("a", 40)
	tree := strings.Repeat("b", 40)
	result := SyncResult{Initial: true, Workspaces: []SyncedWorkspace{{
		Workspace: WorkspaceArticle, Status: WorkspaceReady,
		HeadCommitSHA: sha, TreeSHA: tree,
	}}}
	if _, _, err := validateSyncWorkspaces(
		SyncClaim{Workspaces: []WorkspaceKind{WorkspaceArticle}}, result,
	); !errors.Is(err, ErrInvalid) {
		t.Fatalf("initial partial sync was accepted: %v", err)
	}
}
