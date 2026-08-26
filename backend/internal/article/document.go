package article

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"sort"
	"strings"
	"time"
)

type idGenerator interface{ New() (string, error) }

// NormalizeDocument enforces the stable top-level ArticleBlock identity used
// by Tiptap UniqueID and derives the Markdown/ArticleBlock projections from
// the same authoritative JSON snapshot.
func NormalizeDocument(document map[string]interface{}, generator idGenerator, actorKind string, provenance map[string]interface{}, now time.Time) (string, []Block, error) {
	if document == nil || document["type"] != "doc" {
		return "", nil, ErrInvalid
	}
	content, ok := interfaceSlice(document["content"])
	if !ok {
		content = []interface{}{}
		document["content"] = content
	}
	blocks := make([]Block, 0, len(content))
	for index, raw := range content {
		node, ok := raw.(map[string]interface{})
		if !ok {
			return "", nil, ErrInvalid
		}
		attrs := object(node["attrs"])
		blockID, _ := attrs["id"].(string)
		if strings.TrimSpace(blockID) == "" {
			if generator == nil {
				return "", nil, ErrInvalid
			}
			generated, err := generator.New()
			if err != nil {
				return "", nil, err
			}
			blockID = generated
			attrs["id"] = generated
			node["attrs"] = attrs
		}
		nodeType, _ := node["type"].(string)
		if nodeType == "" {
			return "", nil, ErrInvalid
		}
		sanitizeDocumentNode(node)
		attrs = object(node["attrs"])
		text := plainText(node)
		tag := tagForActor(actorKind, attrs)
		attrs["tag"] = tag
		blockProvenance := cloneObject(provenance)
		if existing, ok := attrs["provenance"].(map[string]interface{}); ok {
			for key, value := range existing {
				blockProvenance[key] = value
			}
		}
		attrs["provenance"] = blockProvenance
		blocks = append(blocks, Block{Attrs: attrs, BlockID: blockID, NodeType: nodeType, Ordinal: index, Provenance: blockProvenance, Tag: tag, Text: text, UpdatedAt: now})
	}
	markdown, err := renderDocumentContent(content)
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(strings.Join(markdown, "\n\n")) + trailingNewline(markdown), blocks, nil
}

// renderDocumentContent keeps legacy sibling table captions compatible while
// the current editor stores the caption on the table node itself. A document
// briefly containing both forms during migration must render only one caption.
func renderDocumentContent(content []interface{}) ([]string, error) {
	markdown := make([]string, 0, len(content))
	for index := 0; index < len(content); index++ {
		node, ok := content[index].(map[string]interface{})
		if !ok {
			return nil, ErrInvalid
		}
		nodeType, _ := node["type"].(string)
		if nodeType == "tableCaption" {
			caption := renderTableCaption(node)
			if index+1 < len(content) {
				next, nextOK := content[index+1].(map[string]interface{})
				nextType, _ := next["type"].(string)
				if nextOK && nextType == "table" {
					table, err := renderBlock(next)
					if err != nil {
						return nil, err
					}
					if table != "" {
						// The bound caption is authoritative once present on the
						// table; the sibling exists only for old snapshots.
						boundCaption := markdownCaption(stringAttr(object(next["attrs"]), "caption"))
						if caption != "" && boundCaption == "" {
							markdown = append(markdown, caption+"\n\n"+table)
						} else {
							markdown = append(markdown, table)
						}
						index++
						continue
					}
				}
			}
			if caption != "" {
				markdown = append(markdown, caption)
			}
			continue
		}
		rendered, err := renderBlock(node)
		if err != nil {
			return nil, err
		}
		if rendered != "" {
			markdown = append(markdown, rendered)
		}
	}
	return markdown, nil
}

// ReconcileBlockTags preserves review decisions for unchanged stable blocks
// and turns an edited draft block into a revision. Client-provided tag and
// provenance attributes are never trusted as the authoritative review state.
func ReconcileBlockTags(document map[string]interface{}, previous Draft, blocks []Block, actorKind string, provenance map[string]interface{}, now time.Time) []Block {
	previousNodes := blockNodesByID(previous.TiptapJSON)
	currentNodes := blockNodesByID(document)
	previousBlocks := make(map[string]Block, len(previous.Blocks))
	for _, block := range previous.Blocks {
		previousBlocks[block.BlockID] = block
	}
	for index := range blocks {
		block := &blocks[index]
		prior, existed := previousBlocks[block.BlockID]
		same := existed && semanticNodeHash(previousNodes[block.BlockID]) == semanticNodeHash(currentNodes[block.BlockID])
		if same {
			block.Tag = tagForActor("", map[string]interface{}{"tag": prior.Tag})
			block.Provenance = cloneObject(prior.Provenance)
			block.UpdatedAt = prior.UpdatedAt
		} else {
			block.Tag = actorTag(actorKind, existed)
			block.Provenance = cloneObject(provenance)
			block.UpdatedAt = now
		}
		block.Attrs["tag"] = block.Tag
		block.Attrs["provenance"] = block.Provenance
	}
	return blocks
}

func actorTag(actorKind string, revision bool) string {
	prefix := "human"
	if actorKind == "ai" {
		prefix = "ai"
	}
	if revision {
		return prefix + "_revision"
	}
	return prefix + "_draft"
}

func blockNodesByID(document map[string]interface{}) map[string]map[string]interface{} {
	result := map[string]map[string]interface{}{}
	content, _ := interfaceSlice(document["content"])
	for _, raw := range content {
		node, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := object(node["attrs"])["id"].(string)
		if id != "" {
			result[id] = node
		}
	}
	return result
}

func semanticNodeHash(node map[string]interface{}) [32]byte {
	if node == nil {
		return [32]byte{}
	}
	encoded, _ := json.Marshal(node)
	var clean map[string]interface{}
	_ = json.Unmarshal(encoded, &clean)
	attrs := object(clean["attrs"])
	delete(attrs, "tag")
	delete(attrs, "provenance")
	clean["attrs"] = attrs
	encoded, _ = json.Marshal(clean)
	return sha256.Sum256(encoded)
}

func chapterHeadingFingerprint(block Block) string {
	attrs := cloneObject(block.Attrs)
	delete(attrs, "id")
	delete(attrs, "tag")
	delete(attrs, "provenance")
	encoded, _ := json.Marshal(map[string]interface{}{
		"node_type": block.NodeType,
		"text":      block.Text,
		"attrs":     attrs,
	})
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest)
}

func headingBlocksByID(blocks []Block) map[string]Block {
	result := make(map[string]Block)
	for _, block := range blocks {
		if block.NodeType == "heading" && block.BlockID != "" {
			result[block.BlockID] = block
		}
	}
	return result
}

func tagForActor(actorKind string, attrs map[string]interface{}) string {
	if existing, ok := attrs["tag"].(string); ok {
		switch existing {
		case "ai_draft", "human_draft", "ai_revision", "human_revision", "reviewed":
			return existing
		}
	}
	if actorKind == "ai" {
		return "ai_draft"
	}
	return "human_draft"
}

func renderBlock(node map[string]interface{}) (string, error) {
	nodeType, _ := node["type"].(string)
	attrs := object(node["attrs"])
	switch nodeType {
	case "paragraph":
		return renderInlineChildren(node), nil
	case "heading":
		level := integer(attrs["level"], 1)
		if level < 1 || level > 6 {
			level = 1
		}
		return strings.Repeat("#", level) + " " + renderInlineChildren(node), nil
	case "bulletList":
		return renderList(node, false), nil
	case "orderedList":
		return renderList(node, true), nil
	case "blockquote":
		value := renderChildren(node)
		lines := strings.Split(value, "\n")
		for index := range lines {
			lines[index] = "> " + lines[index]
		}
		return strings.Join(lines, "\n"), nil
	case "codeBlock":
		language, _ := attrs["language"].(string)
		return "```" + safeFenceInfo(language) + "\n" + plainText(node) + "\n```", nil
	case "table":
		return renderTable(node), nil
	case "horizontalRule":
		return "---", nil
	case "hardBreak":
		return "  \n", nil
	case "mathBlock", "blockMath":
		return "$$\n" + stringAttr(attrs, "latex") + "\n$$", nil
	case "image":
		return "![" + escapeMarkdown(stringAttr(attrs, "alt")) + "](" + safeImageTarget(stringAttr(attrs, "src")) + ")", nil
	case "articleImage":
		value := "![" + escapeMarkdown(stringAttr(attrs, "alt")) + "](" + safeImageTarget(stringAttr(attrs, "src")) + ")"
		if caption := markdownCaption(stringAttr(attrs, "caption")); caption != "" {
			value += "\n\n" + caption
		}
		return value, nil
	case "articleImageGroup":
		return renderImageGroup(node), nil
	case "tableCaption":
		return renderTableCaption(node), nil
	case "artifactReference":
		artifactID := stringAttr(attrs, "artifactId")
		if artifactID == "" {
			artifactID = stringAttr(attrs, "objectId")
		}
		if strings.HasPrefix(stringAttr(attrs, "mimeType"), "image/") {
			target := fmt.Sprintf("mmdash://artifact/%s/versions/%s", safeID(artifactID), safeID(stringAttr(attrs, "versionId")))
			value := "![" + escapeMarkdown(stringAttr(attrs, "title")) + "](" + target + ")"
			if caption := markdownCaption(stringAttr(attrs, "caption")); caption != "" {
				value += "\n\n" + caption
			}
			return value, nil
		}
		return fmt.Sprintf("[Artifact %s@%s](mmdash://artifact/%s/versions/%s)", escapeMarkdown(stringAttr(attrs, "title")), escapeMarkdown(stringAttr(attrs, "versionId")), safeID(artifactID), safeID(stringAttr(attrs, "versionId"))), nil
	case "experimentResult":
		return fmt.Sprintf("[Experiment result %s@%s](mmdash://experiment/%s/results/%s)", escapeMarkdown(stringAttr(attrs, "title")), escapeMarkdown(stringAttr(attrs, "versionId")), safeID(stringAttr(attrs, "experimentId")), safeID(stringAttr(attrs, "versionId"))), nil
	case "modelReference":
		return fmt.Sprintf("[Model %s@%s](mmdash://model/%s/snapshots/%s)", escapeMarkdown(stringAttr(attrs, "title")), escapeMarkdown(stringAttr(attrs, "versionId")), safeID(stringAttr(attrs, "objectId")), safeID(stringAttr(attrs, "versionId"))), nil
	default:
		return renderInlineChildren(node), nil
	}
}

func renderList(node map[string]interface{}, ordered bool) string {
	items, _ := interfaceSlice(node["content"])
	lines := []string{}
	for index, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		prefix := "- "
		if ordered {
			prefix = fmt.Sprintf("%d. ", index+1)
		}
		value := strings.TrimSpace(renderChildren(item))
		value = strings.ReplaceAll(value, "\n", "\n  ")
		lines = append(lines, prefix+value)
	}
	return strings.Join(lines, "\n")
}

func renderChildren(node map[string]interface{}) string {
	children, _ := interfaceSlice(node["content"])
	values := make([]string, 0, len(children))
	for _, raw := range children {
		child, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		value, _ := renderBlock(child)
		if value != "" {
			values = append(values, value)
		}
	}
	return strings.Join(values, "\n\n")
}

func renderInlineChildren(node map[string]interface{}) string {
	children, _ := interfaceSlice(node["content"])
	var result strings.Builder
	for _, raw := range children {
		child, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		typeName, _ := child["type"].(string)
		attrs := object(child["attrs"])
		value := ""
		switch typeName {
		case "text":
			value, _ = child["text"].(string)
			value = escapeMarkdown(value)
		case "hardBreak":
			value = "  \n"
		case "mathInline", "inlineMath":
			value = "$" + stringAttr(attrs, "latex") + "$"
		case "citation", "zoteroCitation":
			citationKey := stringAttr(attrs, "citationKey")
			if typeName == "zoteroCitation" && strings.TrimSpace(citationKey) == "" {
				citationKey = stringAttr(attrs, "itemKey")
			}
			value = "[@" + safeCitationKey(citationKey) + "]"
		default:
			value = renderInlineChildren(child)
		}
		value = applyMarks(value, child["marks"])
		result.WriteString(value)
	}
	return result.String()
}

func renderTableCaption(node map[string]interface{}) string {
	caption := markdownCaption(stringAttr(object(node["attrs"]), "caption"))
	if caption == "" {
		return ""
	}
	// Pandoc/GFM recognize the Table: paragraph as the caption belonging to
	// the immediately following pipe table. Keeping it in the same rendered
	// unit also prevents an intervening block from stealing the association.
	return "Table: " + caption
}

func markdownCaption(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
	if value == "" {
		return ""
	}
	return escapeMarkdown(value)
}

func escapeLaTeX(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		`\`, `\textbackslash{}`,
		`%`, `\%`,
		`$`, `\$`,
		`&`, `\&`,
		`#`, `\#`,
		`_`, `\_`,
		`{`, `\{`,
		`}`, `\}`,
		`~`, `\textasciitilde{}`,
		`^`, `\textasciicircum{}`,
	)
	return replacer.Replace(value)
}

func subfigureWidth(count int) string {
	switch count {
	case 1:
		return "0.98\\linewidth"
	case 2:
		return "0.48\\linewidth"
	case 3:
		return "0.31\\linewidth"
	case 4:
		return "0.23\\linewidth"
	default:
		if count <= 0 {
			return "0.98\\linewidth"
		}
		return fmt.Sprintf("%.2f\\linewidth", (1.0-0.04*float64(count-1))/float64(count))
	}
}

type imageGroupCell struct {
	target  string
	alt     string
	caption string
}

func extractImageGroupCell(node map[string]interface{}) (imageGroupCell, bool) {
	nodeType, _ := node["type"].(string)
	attrs := object(node["attrs"])
	alt := stringAttr(attrs, "alt")
	target := ""
	switch nodeType {
	case "articleImage":
		target = safeImageTarget(stringAttr(attrs, "src"))
	case "artifactReference":
		if !strings.HasPrefix(stringAttr(attrs, "mimeType"), "image/") {
			return imageGroupCell{}, false
		}
		artifactID := stringAttr(attrs, "artifactId")
		if artifactID == "" {
			artifactID = stringAttr(attrs, "objectId")
		}
		if alt == "" {
			alt = stringAttr(attrs, "title")
		}
		target = fmt.Sprintf("mmdash://artifact/%s/versions/%s", safeID(artifactID), safeID(stringAttr(attrs, "versionId")))
	default:
		return imageGroupCell{}, false
	}
	caption := stringAttr(attrs, "caption")
	return imageGroupCell{target: target, alt: alt, caption: caption}, true
}

func renderImageGroup(node map[string]interface{}) string {
	children, _ := interfaceSlice(node["content"])
	items := make([]imageGroupCell, 0, len(children))
	for _, raw := range children {
		child, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		item, ok := extractImageGroupCell(child)
		if ok {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return ""
	}
	columns := integer(object(node["attrs"])["columns"], 2)
	if columns < 1 {
		columns = 1
	}
	if columns > 4 {
		columns = 4
	}

	var builder strings.Builder
	builder.WriteString("\\begin{figure}[htbp]\n\\centering\n")

	var rows [][]imageGroupCell
	for start := 0; start < len(items); start += columns {
		end := start + columns
		if end > len(items) {
			end = len(items)
		}
		rows = append(rows, items[start:end])
	}

	for rowIndex, row := range rows {
		if rowIndex > 0 {
			builder.WriteString("\n\\par\\medskip\n")
		}
		rowLen := len(row)
		widthStr := subfigureWidth(rowLen)
		for itemIndex, item := range row {
			if itemIndex > 0 {
				builder.WriteString("\n\\hfill\n")
			}
			builder.WriteString(fmt.Sprintf("\\begin{subfigure}[b]{%s}\n  \\centering\n  \\includegraphics[width=\\linewidth]{%s}", widthStr, item.target))
			escapedSubCaption := escapeLaTeX(item.caption)
			if escapedSubCaption != "" {
				builder.WriteString(fmt.Sprintf("\n  \\caption{%s}", escapedSubCaption))
			}
			builder.WriteString("\n\\end{subfigure}")
		}
	}

	escapedGroupCaption := escapeLaTeX(stringAttr(object(node["attrs"]), "caption"))
	if escapedGroupCaption != "" {
		builder.WriteString(fmt.Sprintf("\n\\caption{%s}", escapedGroupCaption))
	}
	builder.WriteString("\n\\end{figure}")
	return builder.String()
}

func renderTable(node map[string]interface{}) string {
	rows, _ := interfaceSlice(node["content"])
	if len(rows) == 0 {
		return ""
	}
	values := make([][]string, 0, len(rows))
	columns := 0
	for _, rawRow := range rows {
		row, ok := rawRow.(map[string]interface{})
		if !ok {
			continue
		}
		cells, _ := interfaceSlice(row["content"])
		value := make([]string, 0, len(cells))
		for _, rawCell := range cells {
			cell, ok := rawCell.(map[string]interface{})
			if !ok {
				continue
			}
			text := strings.TrimSpace(renderChildren(cell))
			text = strings.ReplaceAll(text, "|", `\|`)
			text = strings.ReplaceAll(text, "\n", "<br>")
			value = append(value, text)
		}
		if len(value) > columns {
			columns = len(value)
		}
		values = append(values, value)
	}
	if len(values) == 0 || columns == 0 {
		return ""
	}
	line := func(cells []string) string {
		padded := make([]string, columns)
		copy(padded, cells)
		return "| " + strings.Join(padded, " | ") + " |"
	}
	separator := make([]string, columns)
	for index := range separator {
		separator[index] = "---"
	}
	lines := []string{line(values[0]), line(separator)}
	for _, row := range values[1:] {
		lines = append(lines, line(row))
	}
	table := strings.Join(lines, "\n")
	if caption := markdownCaption(stringAttr(object(node["attrs"]), "caption")); caption != "" {
		return "Table: " + caption + "\n\n" + table
	}
	return table
}

func applyMarks(value string, raw interface{}) string {
	marks, _ := interfaceSlice(raw)
	for _, markRaw := range marks {
		mark, ok := markRaw.(map[string]interface{})
		if !ok {
			continue
		}
		kind, _ := mark["type"].(string)
		switch kind {
		case "bold":
			value = "**" + value + "**"
		case "italic":
			value = "*" + value + "*"
		case "strike":
			value = "~~" + value + "~~"
		case "code":
			value = "`" + strings.ReplaceAll(value, "`", "\\`") + "`"
		case "link":
			value = "[" + value + "](" + safeTarget(stringAttr(object(mark["attrs"]), "href")) + ")"
		}
	}
	return value
}

func plainText(node map[string]interface{}) string {
	if text, ok := node["text"].(string); ok {
		return text
	}
	children, _ := interfaceSlice(node["content"])
	var result strings.Builder
	for _, raw := range children {
		if child, ok := raw.(map[string]interface{}); ok {
			result.WriteString(plainText(child))
		}
	}
	return result.String()
}

func StableJSON(value interface{}) ([]byte, error) {
	// encoding/json sorts map keys, providing deterministic Git and hash bytes.
	return json.MarshalIndent(value, "", "  ")
}

func Bibliography(references []Reference) string {
	items := append([]Reference(nil), references...)
	sort.Slice(items, func(i, j int) bool { return items[i].CitationKey < items[j].CitationKey })
	var result strings.Builder
	for _, reference := range items {
		if reference.CitationKey == "" {
			continue
		}
		entryType := "misc"
		if value, ok := reference.Metadata["bibtex_type"].(string); ok && value != "" {
			entryType = value
		}
		result.WriteString("@" + entryType + "{" + safeCitationKey(reference.CitationKey) + ",\n")
		result.WriteString("  title = {" + escapeBib(reference.Title) + "},\n")
		result.WriteString("  note = {mmdash " + escapeBib(reference.ReferenceType+":"+reference.SourceObjectID+"@"+reference.SourceVersionID) + "}\n}\n\n")
	}
	return result.String()
}

func interfaceSlice(value interface{}) ([]interface{}, bool) {
	items, ok := value.([]interface{})
	return items, ok
}
func object(value interface{}) map[string]interface{} {
	if item, ok := value.(map[string]interface{}); ok {
		return item
	}
	return map[string]interface{}{}
}
func cloneObject(value map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{}
	for key, item := range value {
		result[key] = item
	}
	return result
}

func cloneDocument(value map[string]interface{}) (map[string]interface{}, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err = json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	return result, nil
}
func integer(value interface{}, fallback int) int {
	if number, ok := value.(float64); ok {
		return int(number)
	}
	if number, ok := value.(int); ok {
		return number
	}
	return fallback
}
func stringAttr(attrs map[string]interface{}, key string) string {
	value, _ := attrs[key].(string)
	return value
}
func trailingNewline(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return "\n"
}
func escapeMarkdown(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]")
	return replacer.Replace(value)
}
func escapeBib(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\textbackslash{}"), "{", "\\{")
}
func safeTarget(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "mmdash://") {
		return strings.ReplaceAll(value, ")", "%29")
	}
	return "about:blank"
}
func safeImageTarget(value string) string {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return "about:blank"
	}
	switch parsed.Scheme {
	case "http", "https":
		if parsed.Hostname() == "" {
			return "about:blank"
		}
	case "mmdash":
		if parsed.Host != "artifact" || !strings.HasPrefix(parsed.Path, "/") || parsed.Path == "/" {
			return "about:blank"
		}
	default:
		return "about:blank"
	}
	return strings.ReplaceAll(value, ")", "%29")
}

func sanitizeTransientArtifactAttrs(nodeType string, attrs map[string]interface{}) {
	if nodeType != "artifactReference" {
		return
	}
	for _, key := range []string{"previewUrl", "preview_url", "expiresAt", "expires_at"} {
		delete(attrs, key)
	}
}

func sanitizeTransientImageAttrs(nodeType string, attrs map[string]interface{}) {
	if nodeType != "articleImage" {
		return
	}
	value, _ := attrs["src"].(string)
	parsed, err := url.Parse(value)
	if err != nil {
		delete(attrs, "src")
		return
	}
	for key := range parsed.Query() {
		switch strings.ToLower(key) {
		case "x-amz-algorithm", "x-amz-credential", "x-amz-signature", "x-amz-security-token", "signature", "token":
			delete(attrs, "src")
			return
		}
	}
}

func sanitizeDocumentNode(node map[string]interface{}) {
	nodeType, _ := node["type"].(string)
	if _, hasAttrs := node["attrs"]; hasAttrs {
		attrs := object(node["attrs"])
		sanitizeTransientArtifactAttrs(nodeType, attrs)
		sanitizeTransientImageAttrs(nodeType, attrs)
		node["attrs"] = attrs
	}
	children, _ := interfaceSlice(node["content"])
	for _, raw := range children {
		if child, ok := raw.(map[string]interface{}); ok {
			sanitizeDocumentNode(child)
		}
	}
}
func safeID(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._", r) {
			return r
		}
		return -1
	}, value)
}
func safeCitationKey(value string) string { return safeID(value) }
func safeFenceInfo(value string) string   { return safeID(value) }
func htmlText(value string) string        { return html.UnescapeString(value) }
