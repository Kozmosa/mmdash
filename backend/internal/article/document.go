package article

import (
	"encoding/json"
	"fmt"
	"html"
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
	markdown := make([]string, 0, len(content))
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
		rendered, err := renderBlock(node)
		if err != nil {
			return "", nil, err
		}
		if rendered != "" {
			markdown = append(markdown, rendered)
		}
	}
	return strings.TrimSpace(strings.Join(markdown, "\n\n")) + trailingNewline(markdown), blocks, nil
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
	case "horizontalRule":
		return "---", nil
	case "hardBreak":
		return "  \n", nil
	case "mathBlock":
		return "$$\n" + stringAttr(attrs, "latex") + "\n$$", nil
	case "image":
		return "![" + escapeMarkdown(stringAttr(attrs, "alt")) + "](" + safeImageTarget(stringAttr(attrs, "src")) + ")", nil
	case "artifactReference":
		return fmt.Sprintf("[Artifact %s@%s](mmdash://artifact/%s/versions/%s)", escapeMarkdown(stringAttr(attrs, "title")), escapeMarkdown(stringAttr(attrs, "versionId")), safeID(stringAttr(attrs, "artifactId")), safeID(stringAttr(attrs, "versionId"))), nil
	case "experimentResult":
		return fmt.Sprintf("[Experiment result %s@%s](mmdash://experiment/%s/results/%s)", escapeMarkdown(stringAttr(attrs, "title")), escapeMarkdown(stringAttr(attrs, "versionId")), safeID(stringAttr(attrs, "experimentId")), safeID(stringAttr(attrs, "versionId"))), nil
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
		case "mathInline":
			value = "$" + stringAttr(attrs, "latex") + "$"
		case "citation":
			value = "[@" + safeCitationKey(stringAttr(attrs, "citationKey")) + "]"
		default:
			value = renderInlineChildren(child)
		}
		value = applyMarks(value, child["marks"])
		result.WriteString(value)
	}
	return result.String()
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
	if strings.HasPrefix(value, "mmdash://artifact/") {
		return strings.ReplaceAll(value, ")", "%29")
	}
	return "about:blank"
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
