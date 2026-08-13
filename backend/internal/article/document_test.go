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
