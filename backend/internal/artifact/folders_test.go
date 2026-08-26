package artifact

import (
	"encoding/json"
	"testing"
)

func TestBuildFolderTreeReconstructsNestedProjectFolders(t *testing.T) {
	parentID := "550e8400-e29b-41d4-a716-446655440000"
	flat := []Folder{
		{ID: "550e8400-e29b-41d4-a716-446655440002", ProjectID: "p", Name: "Child", Position: 0, ParentFolderID: &parentID},
		{ID: parentID, ProjectID: "p", Name: "Root", Position: 0},
	}
	tree := buildFolderTree(flat)
	if len(tree.Items) != 1 || tree.Items[0].ID != parentID {
		t.Fatalf("unexpected roots: %#v", tree.Items)
	}
	if len(tree.Items[0].Children) != 1 || tree.Items[0].Children[0].Name != "Child" {
		t.Fatalf("nested folder was not attached: %#v", tree.Items[0])
	}
}

func TestBuildFolderTreeReturnsEmptyChildrenForLeafFolders(t *testing.T) {
	tree := buildFolderTree([]Folder{{ID: "folder", Name: "Leaf"}})
	if len(tree.Items) != 1 || tree.Items[0].Children == nil {
		t.Fatalf("leaf children must be an explicit empty array: %#v", tree)
	}
}

func TestNullableFolderIDDistinguishesRequiredNullFromMissing(t *testing.T) {
	root, err := nullableFolderID(json.RawMessage("null"))
	if err != nil || root != nil {
		t.Fatalf("null should mean project root, got %#v, %v", root, err)
	}
	if _, err := nullableFolderID(nil); err != ErrInvalid {
		t.Fatalf("missing required folder ID should be invalid, got %v", err)
	}
}
