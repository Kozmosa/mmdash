package article

import (
	"strings"
	"testing"
	"time"
)

type sequentialIDs struct{ next int }

func (ids *sequentialIDs) New() (string, error) {
	ids.next++
	return "block-" + string(rune('0'+ids.next)), nil
}

func TestNormalizeDocumentCreatesStableBlocksAndCanonicalMarkdown(t *testing.T) {
	now := time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC)
	document := map[string]interface{}{
		"type": "doc",
		"content": []interface{}{
			map[string]interface{}{"type": "heading", "attrs": map[string]interface{}{"level": float64(2)}, "content": []interface{}{map[string]interface{}{"type": "text", "text": "Method"}}},
			map[string]interface{}{"type": "paragraph", "attrs": map[string]interface{}{"id": "kept"}, "content": []interface{}{
				map[string]interface{}{"type": "text", "text": "See "},
				map[string]interface{}{"type": "citation", "attrs": map[string]interface{}{"citationKey": "Smith2026"}},
				map[string]interface{}{"type": "text", "text": " and "},
				map[string]interface{}{"type": "mathInline", "attrs": map[string]interface{}{"latex": "x^2"}},
			}},
			map[string]interface{}{"type": "artifactReference", "attrs": map[string]interface{}{"artifactId": "artifact-1", "versionId": "version-2", "title": "plot"}},
		},
	}

	markdown, blocks, err := NormalizeDocument(document, &sequentialIDs{}, "ai", map[string]interface{}{"agent_id": "agent-1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if markdown != "## Method\n\nSee [@Smith2026] and $x^2$\n\n[Artifact plot@version-2](mmdash://artifact/artifact-1/versions/version-2)\n" {
		t.Fatalf("unexpected markdown:\n%s", markdown)
	}
	if len(blocks) != 3 || blocks[0].BlockID != "block-1" || blocks[1].BlockID != "kept" || blocks[2].BlockID != "block-2" {
		t.Fatalf("stable block ids were not preserved/generated: %#v", blocks)
	}
	for _, block := range blocks {
		if block.Tag != "ai_draft" || block.Provenance["agent_id"] != "agent-1" || !block.UpdatedAt.Equal(now) {
			t.Fatalf("block provenance/tag mismatch: %#v", block)
		}
	}
}

func TestNormalizeDocumentRejectsUnsafeTargetsAndEscapesText(t *testing.T) {
	document := map[string]interface{}{"type": "doc", "content": []interface{}{
		map[string]interface{}{"type": "paragraph", "attrs": map[string]interface{}{"id": "p1"}, "content": []interface{}{
			map[string]interface{}{"type": "text", "text": "*[literal]*", "marks": []interface{}{map[string]interface{}{"type": "link", "attrs": map[string]interface{}{"href": "javascript:alert(1)"}}}},
		}},
	}}
	markdown, _, err := NormalizeDocument(document, nil, "human", map[string]interface{}{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markdown, "about:blank") || strings.Contains(markdown, "javascript:") || !strings.Contains(markdown, `\*\[literal\]\*`) {
		t.Fatalf("unsafe or unstable markdown: %q", markdown)
	}
}

func TestBibliographyFreezesStableVersionPointers(t *testing.T) {
	value := Bibliography([]Reference{
		{CitationKey: "zeta", ReferenceType: "zotero", SourceObjectID: "item", SourceVersionID: "7", Title: "Z"},
		{CitationKey: "alpha", ReferenceType: "experiment_result", SourceObjectID: "run", SourceVersionID: "v2", Title: "A"},
	})
	if strings.Index(value, "@misc{alpha") > strings.Index(value, "@misc{zeta") || !strings.Contains(value, "experiment_result:run@v2") || !strings.Contains(value, "zotero:item@7") {
		t.Fatalf("bibliography is not deterministic/frozen:\n%s", value)
	}
}

func TestNormalizeDocumentRendersGFMTableCodeAndOfficialMathNodes(t *testing.T) {
	text := func(value string) map[string]interface{} {
		return map[string]interface{}{"type": "text", "text": value}
	}
	paragraph := func(content ...interface{}) map[string]interface{} {
		return map[string]interface{}{"type": "paragraph", "content": content}
	}
	cell := func(kind string, content ...interface{}) map[string]interface{} {
		return map[string]interface{}{"type": kind, "content": []interface{}{paragraph(content...)}}
	}
	document := map[string]interface{}{"type": "doc", "content": []interface{}{
		map[string]interface{}{"type": "table", "attrs": map[string]interface{}{"id": "table-1"}, "content": []interface{}{
			map[string]interface{}{"type": "tableRow", "content": []interface{}{
				cell("tableHeader", text("Name")),
				cell("tableHeader", text("Value")),
			}},
			map[string]interface{}{"type": "tableRow", "content": []interface{}{
				cell("tableCell", text("A|B")),
				cell("tableCell", map[string]interface{}{"type": "inlineMath", "attrs": map[string]interface{}{"latex": "x^2"}}),
			}},
		}},
		map[string]interface{}{"type": "codeBlock", "attrs": map[string]interface{}{"id": "code-1", "language": "python"}, "content": []interface{}{text("print('ok')")}},
		map[string]interface{}{"type": "blockMath", "attrs": map[string]interface{}{"id": "math-1", "latex": "\\sum_i x_i"}},
	}}

	markdown, _, err := NormalizeDocument(document, nil, "human", map[string]interface{}{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	want := "| Name | Value |\n| --- | --- |\n| A\\|B | $x^2$ |\n\n```python\nprint('ok')\n```\n\n$$\n\\sum_i x_i\n$$\n"
	if markdown != want {
		t.Fatalf("unexpected rich markdown:\n%s", markdown)
	}
}

func TestReconcileBlockTagsPreservesReviewAndMarksEditedBlocksAsRevisions(t *testing.T) {
	oldTime := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	now := oldTime.Add(time.Hour)
	previousJSON := map[string]interface{}{"type": "doc", "content": []interface{}{
		map[string]interface{}{"type": "paragraph", "attrs": map[string]interface{}{"id": "stable"}, "content": []interface{}{map[string]interface{}{"type": "text", "text": "unchanged"}}},
		map[string]interface{}{"type": "paragraph", "attrs": map[string]interface{}{"id": "edited"}, "content": []interface{}{map[string]interface{}{"type": "text", "text": "before"}}},
	}}
	currentJSON := map[string]interface{}{"type": "doc", "content": []interface{}{
		map[string]interface{}{"type": "paragraph", "attrs": map[string]interface{}{"id": "stable", "tag": "human_draft"}, "content": []interface{}{map[string]interface{}{"type": "text", "text": "unchanged"}}},
		map[string]interface{}{"type": "paragraph", "attrs": map[string]interface{}{"id": "edited", "tag": "reviewed"}, "content": []interface{}{map[string]interface{}{"type": "text", "text": "after"}}},
	}}
	_, current, err := NormalizeDocument(currentJSON, nil, "human", map[string]interface{}{"session_id": "current"}, now)
	if err != nil {
		t.Fatal(err)
	}
	previous := Draft{TiptapJSON: previousJSON, Blocks: []Block{
		{BlockID: "stable", Tag: "reviewed", UpdatedAt: oldTime, Provenance: map[string]interface{}{"reviewed_by": "user-1"}},
		{BlockID: "edited", Tag: "reviewed", UpdatedAt: oldTime, Provenance: map[string]interface{}{"reviewed_by": "user-1"}},
	}}
	result := ReconcileBlockTags(currentJSON, previous, current, "human", map[string]interface{}{"session_id": "current"}, now)
	if result[0].Tag != "reviewed" || !result[0].UpdatedAt.Equal(oldTime) || result[0].Provenance["reviewed_by"] != "user-1" {
		t.Fatalf("unchanged review was not preserved: %#v", result[0])
	}
	if result[1].Tag != "human_revision" || !result[1].UpdatedAt.Equal(now) || result[1].Provenance["session_id"] != "current" {
		t.Fatalf("edited reviewed block was not reclassified: %#v", result[1])
	}
}
