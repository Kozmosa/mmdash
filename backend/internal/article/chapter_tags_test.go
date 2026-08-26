package article

import (
	"testing"
	"time"
)

func TestReconcileChapterTagStatePreservesUnchangedReview(t *testing.T) {
	block := Block{
		Attrs:    map[string]interface{}{"id": "heading-1", "level": float64(2)},
		BlockID:  "heading-1",
		NodeType: "heading",
		Text:     "Results",
	}
	tag := ChapterTag{
		HeadingBlockID:     block.BlockID,
		HeadingBlockType:   block.NodeType,
		HeadingFingerprint: chapterHeadingFingerprint(block),
		Status:             ChapterTagReviewed,
		ReviewedBy:         stringPtr("reviewer-1"),
		ReviewedAt:         timePtr(time.Unix(1, 0)),
	}

	status, reason, nodeType, changed := reconcileChapterTagState(tag, block, true)
	if changed || status != ChapterTagReviewed || reason != "" || nodeType != "heading" {
		t.Fatalf("unchanged reviewed chapter was altered: status=%s reason=%s type=%s changed=%v", status, reason, nodeType, changed)
	}
}

func TestReconcileChapterTagStateMarksHeadingContentChangeNeedsReview(t *testing.T) {
	old := Block{Attrs: map[string]interface{}{"id": "heading-1", "level": float64(2)}, BlockID: "heading-1", NodeType: "heading", Text: "Results"}
	current := Block{Attrs: map[string]interface{}{"id": "heading-1", "level": float64(2)}, BlockID: "heading-1", NodeType: "heading", Text: "Updated results"}
	tag := ChapterTag{HeadingBlockID: old.BlockID, HeadingBlockType: old.NodeType, HeadingFingerprint: chapterHeadingFingerprint(old), Status: ChapterTagReviewed}

	status, reason, nodeType, changed := reconcileChapterTagState(tag, current, true)
	if !changed || status != ChapterTagNeedsReview || reason != "heading_content_changed" || nodeType != "heading" {
		t.Fatalf("content change did not invalidate review: status=%s reason=%s type=%s changed=%v", status, reason, nodeType, changed)
	}
}

func TestReconcileChapterTagStatePromotesDraftHeadingWithoutPretendingItWasReviewed(t *testing.T) {
	old := Block{Attrs: map[string]interface{}{"id": "heading-1", "level": float64(2)}, BlockID: "heading-1", NodeType: "heading"}
	current := Block{Attrs: map[string]interface{}{"id": "heading-1", "level": float64(2)}, BlockID: "heading-1", NodeType: "heading", Text: "Results"}
	tag := ChapterTag{HeadingBlockID: old.BlockID, HeadingBlockType: old.NodeType, HeadingFingerprint: chapterHeadingFingerprint(old), Status: ChapterTagUnedited}

	status, reason, nodeType, changed := reconcileChapterTagState(tag, current, true)
	if !changed || status != ChapterTagUnreviewed || reason != "" || nodeType != "heading" {
		t.Fatalf("draft heading did not become unreviewed: status=%s reason=%s type=%s changed=%v", status, reason, nodeType, changed)
	}
}

func TestReconcileChapterTagStateMarksTypeAndIdentityChangesNeedsReview(t *testing.T) {
	tag := ChapterTag{HeadingBlockID: "heading-1", HeadingBlockType: "heading", HeadingFingerprint: "old", Status: ChapterTagReviewed}
	typeChanged := Block{BlockID: "heading-1", NodeType: "paragraph", Text: "Results"}
	status, reason, nodeType, changed := reconcileChapterTagState(tag, typeChanged, false)
	if !changed || status != ChapterTagNeedsReview || reason != "heading_missing_or_id_changed" || nodeType != "heading" {
		t.Fatalf("missing heading did not preserve the old binding: status=%s reason=%s type=%s changed=%v", status, reason, nodeType, changed)
	}

	status, reason, nodeType, changed = reconcileChapterTagState(tag, typeChanged, true)
	if !changed || status != ChapterTagNeedsReview || reason != "heading_type_changed" || nodeType != "paragraph" {
		t.Fatalf("type change did not invalidate review: status=%s reason=%s type=%s changed=%v", status, reason, nodeType, changed)
	}
}

func stringPtr(value string) *string     { return &value }
func timePtr(value time.Time) *time.Time { return &value }
