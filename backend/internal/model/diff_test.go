package model

import (
	"strings"
	"testing"
)

func TestCharacterDiffGroupsContiguousUnicodeEdits(t *testing.T) {
	from := Snapshot{SnapshotSummary: SnapshotSummary{ID: "from", QuestionID: "question"}, Blocks: []Block{{ID: "paragraph", Type: "paragraph", Text: "模型假设人口稳定增长"}}}
	to := Snapshot{SnapshotSummary: SnapshotSummary{ID: "to", QuestionID: "question"}, Blocks: []Block{{ID: "paragraph", Type: "paragraph", Text: "模型核心假设人口快速增长"}}}

	diff := CompareSnapshots(from, to)
	if diff.Blocks[0].Change != "modified" || diff.Blocks[0].Block.Text != "模型核心假设人口快速增长" {
		t.Fatalf("diff did not retain the target block structure: %#v", diff.Blocks[0])
	}
	operations := diff.Blocks[0].Operations
	if len(operations) != 6 {
		t.Fatalf("expected six grouped operations, got %#v", operations)
	}
	if operations[0] != (DiffOperation{Kind: "unchanged", Text: "模型"}) || operations[1] != (DiffOperation{Kind: "added", Text: "核心"}) || operations[2] != (DiffOperation{Kind: "unchanged", Text: "假设人口"}) || operations[3] != (DiffOperation{Kind: "deleted", Text: "稳定"}) || operations[4] != (DiffOperation{Kind: "added", Text: "快速"}) || operations[5] != (DiffOperation{Kind: "unchanged", Text: "增长"}) {
		t.Fatalf("unexpected character operations: %#v", operations)
	}
}

func TestCharacterDiffDoesNotUseVisualLines(t *testing.T) {
	before := "同一段文字不会因为页面宽度变化而产生差异"
	operations := characterDiff(before, before)
	if len(operations) != 1 || operations[0].Kind != "unchanged" || operations[0].Text != before {
		t.Fatalf("unexpected unchanged diff: %#v", operations)
	}
}

func TestLargeCharacterDiffUsesBoundedGroupedMiddle(t *testing.T) {
	before := "开头" + strings.Repeat("甲", 3_000) + "结尾"
	after := "开头" + strings.Repeat("乙", 3_000) + "结尾"
	operations := characterDiff(before, after)
	if len(operations) != 4 || operations[1].Kind != "deleted" || len([]rune(operations[1].Text)) != 3_000 || operations[2].Kind != "added" || len([]rune(operations[2].Text)) != 3_000 {
		t.Fatalf("unexpected bounded diff shape: %#v", operations)
	}
}
