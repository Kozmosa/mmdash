package article

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

type sequentialIDs struct{ next int }

func TestScanBuildAllowsTemplateTestWithoutCommitSHA(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	item, err := scanBuild(func(dest ...interface{}) error {
		for index, target := range dest {
			switch value := target.(type) {
			case *string:
				*value = "value"
			case *sql.NullString:
				if index != 5 {
					*value = sql.NullString{String: "value", Valid: true}
				}
			case *sql.NullInt64:
				*value = sql.NullInt64{Int64: 1, Valid: true}
			case *[]byte:
				*value = []byte(`{}`)
			case *time.Time:
				*value = now
			case **time.Time:
				*value = nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.CommitSHA != "" {
		t.Fatalf("template test build should allow a NULL commit SHA, got %q", item.CommitSHA)
	}
}

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
		if block.Tag != "ai_draft" || block.Provenance["agent_id"] != "agent-1" || !block.UpdatedAt.Equal(now) || !shaPattern.MatchString(block.ContentFingerprint) {
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

func TestNormalizeDocumentRendersArticleImageTableCaptionAndZoteroCitation(t *testing.T) {
	text := func(value string) map[string]interface{} {
		return map[string]interface{}{"type": "text", "text": value}
	}
	paragraph := func(content ...interface{}) map[string]interface{} {
		return map[string]interface{}{"type": "paragraph", "content": content}
	}
	cell := func(content ...interface{}) map[string]interface{} {
		return map[string]interface{}{"type": "tableCell", "content": []interface{}{paragraph(content...)}}
	}
	citationParagraph := paragraph(
		text("参考 "),
		map[string]interface{}{"type": "zoteroCitation", "attrs": map[string]interface{}{"citationKey": "Smith2026", "itemKey": "ABC123"}},
	)
	citationParagraph["attrs"] = map[string]interface{}{"id": "paragraph-1"}
	document := map[string]interface{}{"type": "doc", "content": []interface{}{
		map[string]interface{}{"type": "tableCaption", "attrs": map[string]interface{}{"id": "caption-1", "caption": "实验结果"}},
		map[string]interface{}{"type": "table", "attrs": map[string]interface{}{"id": "table-1"}, "content": []interface{}{
			map[string]interface{}{"type": "tableRow", "content": []interface{}{cell(text("值"))}},
		}},
		map[string]interface{}{"type": "articleImage", "attrs": map[string]interface{}{"id": "image-1", "alt": "结果图", "caption": "图 1：结果", "src": "https://example.test/result.png"}},
		citationParagraph,
	}}

	markdown, _, err := NormalizeDocument(document, nil, "human", map[string]interface{}{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	want := "Table: 实验结果\n\n| 值 |\n| --- |\n\n![结果图](https://example.test/result.png)\n\n图 1：结果\n\n参考 [@Smith2026]\n"
	if markdown != want {
		t.Fatalf("unexpected article markdown:\n%s", markdown)
	}
	captionAt := strings.Index(markdown, "Table: 实验结果")
	tableAt := strings.Index(markdown, "| 值 |")
	if captionAt < 0 || tableAt < 0 || captionAt > tableAt {
		t.Fatalf("table caption is not stably rendered immediately before its table: %q", markdown)
	}
}

func TestNormalizeDocumentRendersCaptionBoundToTableWithoutDuplication(t *testing.T) {
	paragraph := func(value string) map[string]interface{} {
		return map[string]interface{}{"type": "paragraph", "content": []interface{}{map[string]interface{}{"type": "text", "text": value}}}
	}
	cell := func(value string) map[string]interface{} {
		return map[string]interface{}{"type": "tableCell", "content": []interface{}{paragraph(value)}}
	}
	table := map[string]interface{}{
		"type":    "table",
		"attrs":   map[string]interface{}{"id": "table-1", "caption": "绑定表注"},
		"content": []interface{}{map[string]interface{}{"type": "tableRow", "content": []interface{}{cell("值")}}},
	}
	document := map[string]interface{}{"type": "doc", "content": []interface{}{table}}

	markdown, _, err := NormalizeDocument(document, nil, "human", map[string]interface{}{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	want := "Table: 绑定表注\n\n| 值 |\n| --- |\n"
	if markdown != want {
		t.Fatalf("unexpected bound table caption markdown: %q", markdown)
	}

	// A legacy sibling may coexist for one synchronization tick while the
	// editor migrates it. The bound table caption remains authoritative.
	document["content"] = []interface{}{
		map[string]interface{}{"type": "tableCaption", "attrs": map[string]interface{}{"id": "legacy-caption", "caption": "旧表注"}},
		table,
	}
	markdown, _, err = NormalizeDocument(document, nil, "human", map[string]interface{}{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if markdown != want || strings.Count(markdown, "Table:") != 1 {
		t.Fatalf("bound and legacy captions rendered inconsistently: %q", markdown)
	}
}

func TestNormalizeDocumentRendersWrappingImageGroupAndSanitizesChildren(t *testing.T) {
	signedAttrs := map[string]interface{}{
		"alt": "C",
		"src": "https://objects.example/c.png?X-Amz-Signature=secret",
	}
	document := map[string]interface{}{"type": "doc", "content": []interface{}{
		map[string]interface{}{
			"type": "articleImageGroup",
			"attrs": map[string]interface{}{
				"id": "image-group-1", "caption": "组合大题注", "columns": 2,
			},
			"content": []interface{}{
				map[string]interface{}{"type": "articleImage", "attrs": map[string]interface{}{
					"alt": "A", "caption": "子图 A", "src": "https://example.test/a.png",
				}},
				map[string]interface{}{"type": "artifactReference", "attrs": map[string]interface{}{
					"alt": "B", "artifactId": "artifact-1", "caption": "子图 B", "mimeType": "image/png", "versionId": "version-2",
				}},
				map[string]interface{}{"type": "articleImage", "attrs": signedAttrs},
			},
		},
	}}

	markdown, blocks, err := NormalizeDocument(document, nil, "human", map[string]interface{}{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	want := "\\begin{figure}[htbp]\n\\centering\n\\begin{subfigure}[b]{0.48\\linewidth}\n  \\centering\n  \\includegraphics[width=\\linewidth]{https://example.test/a.png}\n  \\caption{子图 A}\n\\end{subfigure}\n\\hfill\n\\begin{subfigure}[b]{0.48\\linewidth}\n  \\centering\n  \\includegraphics[width=\\linewidth]{mmdash://artifact/artifact-1/versions/version-2}\n  \\caption{子图 B}\n\\end{subfigure}\n\\par\\medskip\n\\begin{subfigure}[b]{0.98\\linewidth}\n  \\centering\n  \\includegraphics[width=\\linewidth]{about:blank}\n\\end{subfigure}\n\\caption{组合大题注}\n\\end{figure}\n"
	if markdown != want {
		t.Fatalf("unexpected image group markdown:\n%s\nwant:\n%s", markdown, want)
	}
	if _, exists := signedAttrs["src"]; exists {
		t.Fatal("signed URL on a nested image remained in the document")
	}
	if len(blocks) != 1 || blocks[0].NodeType != "articleImageGroup" {
		t.Fatalf("image group was not kept as one top-level block: %#v", blocks)
	}
}

func TestNormalizeDocumentImageGroupPreservesReorderedSequenceAndAdaptiveWidths(t *testing.T) {
	// Test reordered images: C then A then B, with 3 columns (all in 1 row)
	document := map[string]interface{}{"type": "doc", "content": []interface{}{
		map[string]interface{}{
			"type": "articleImageGroup",
			"attrs": map[string]interface{}{
				"id": "image-group-2", "caption": "重排组合", "columns": 3,
			},
			"content": []interface{}{
				map[string]interface{}{"type": "articleImage", "attrs": map[string]interface{}{
					"alt": "C", "caption": "子图 C", "src": "https://example.test/c.png",
				}},
				map[string]interface{}{"type": "articleImage", "attrs": map[string]interface{}{
					"alt": "A", "caption": "子图 A", "src": "https://example.test/a.png",
				}},
				map[string]interface{}{"type": "articleImage", "attrs": map[string]interface{}{
					"alt": "B", "caption": "子图 B", "src": "https://example.test/b.png",
				}},
			},
		},
	}}

	markdown, _, err := NormalizeDocument(document, nil, "human", map[string]interface{}{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	want := "\\begin{figure}[htbp]\n\\centering\n\\begin{subfigure}[b]{0.31\\linewidth}\n  \\centering\n  \\includegraphics[width=\\linewidth]{https://example.test/c.png}\n  \\caption{子图 C}\n\\end{subfigure}\n\\hfill\n\\begin{subfigure}[b]{0.31\\linewidth}\n  \\centering\n  \\includegraphics[width=\\linewidth]{https://example.test/a.png}\n  \\caption{子图 A}\n\\end{subfigure}\n\\hfill\n\\begin{subfigure}[b]{0.31\\linewidth}\n  \\centering\n  \\includegraphics[width=\\linewidth]{https://example.test/b.png}\n  \\caption{子图 B}\n\\end{subfigure}\n\\caption{重排组合}\n\\end{figure}\n"
	if markdown != want {
		t.Fatalf("unexpected reordered image group markdown:\n%s\nwant:\n%s", markdown, want)
	}
}

func TestNormalizeDocumentRendersImmutableModelReference(t *testing.T) {
	document := map[string]interface{}{"type": "doc", "content": []interface{}{
		map[string]interface{}{"type": "modelReference", "attrs": map[string]interface{}{
			"id": "model-reference-1", "objectId": "question-1",
			"versionId": "snapshot-2", "title": "Q1 模型",
		}},
	}}

	markdown, _, err := NormalizeDocument(document, nil, "human", map[string]interface{}{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	want := "[Model Q1 模型@snapshot-2](mmdash://model/question-1/snapshots/snapshot-2)\n"
	if markdown != want {
		t.Fatalf("unexpected model reference markdown: %q", markdown)
	}
}

func TestNormalizeDocumentRemovesTransientArtifactPreviewAttrs(t *testing.T) {
	attrs := map[string]interface{}{
		"artifactId":  "artifact-1",
		"expiresAt":   "2026-08-13T01:00:00Z",
		"previewUrl":  "https://signed.example.test/temporary.png",
		"preview_url": "https://signed.example.test/temporary.png",
		"versionId":   "version-1",
		"mimeType":    "image/png",
		"id":          "artifact-block",
		"title":       "结果图",
		"caption":     "图 2",
	}
	document := map[string]interface{}{"type": "doc", "content": []interface{}{
		map[string]interface{}{"type": "artifactReference", "attrs": attrs},
	}}

	markdown, blocks, err := NormalizeDocument(document, nil, "human", map[string]interface{}{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"previewUrl", "preview_url", "expiresAt"} {
		if _, ok := attrs[key]; ok {
			t.Fatalf("transient artifact attr %q was retained in the document", key)
		}
		if _, ok := blocks[0].Attrs[key]; ok {
			t.Fatalf("transient artifact attr %q was retained in the block projection", key)
		}
	}
	if strings.Contains(markdown, "signed.example.test") {
		t.Fatalf("transient artifact URL leaked into markdown: %q", markdown)
	}
	if markdown != "![结果图](mmdash://artifact/artifact-1/versions/version-1)\n\n图 2\n" {
		t.Fatalf("artifact image did not serialize to its immutable resource URI: %q", markdown)
	}
}

func TestNormalizeDocumentRemovesSignedOrdinaryImageURL(t *testing.T) {
	attrs := map[string]interface{}{
		"id":  "image-1",
		"alt": "result",
		"src": "https://objects.example/result.png?X-Amz-Signature=secret",
	}
	document := map[string]interface{}{"type": "doc", "content": []interface{}{
		map[string]interface{}{"type": "articleImage", "attrs": attrs},
	}}
	markdown, blocks, err := NormalizeDocument(document, nil, "human", map[string]interface{}{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := attrs["src"]; exists {
		t.Fatal("signed image URL remained in authoritative document")
	}
	if _, exists := blocks[0].Attrs["src"]; exists {
		t.Fatal("signed image URL remained in block projection")
	}
	if strings.Contains(markdown, "secret") {
		t.Fatalf("signed query leaked to Markdown: %q", markdown)
	}
}

func TestNormalizeDocumentAllowsOnlySafeImageTargets(t *testing.T) {
	document := map[string]interface{}{"type": "doc", "content": []interface{}{
		map[string]interface{}{"type": "image", "attrs": map[string]interface{}{"id": "http", "alt": "http", "src": "http://example.test/a.png"}},
		map[string]interface{}{"type": "articleImage", "attrs": map[string]interface{}{"id": "artifact", "alt": "artifact", "src": "mmdash://artifact/artifact-1/versions/version-1"}},
		map[string]interface{}{"type": "image", "attrs": map[string]interface{}{"id": "javascript", "alt": "javascript", "src": "javascript:alert(1)"}},
		map[string]interface{}{"type": "articleImage", "attrs": map[string]interface{}{"id": "data", "alt": "data", "src": "data:image/png;base64,AAAA"}},
	}}

	markdown, _, err := NormalizeDocument(document, nil, "human", map[string]interface{}{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{"javascript:", "data:"} {
		if strings.Contains(markdown, unsafe) {
			t.Fatalf("unsafe image scheme %q was serialized: %q", unsafe, markdown)
		}
	}
	for _, expected := range []string{
		"![http](http://example.test/a.png)",
		"![artifact](mmdash://artifact/artifact-1/versions/version-1)",
		"![javascript](about:blank)",
		"![data](about:blank)",
	} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("expected safe image serialization %q in:\n%s", expected, markdown)
		}
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
