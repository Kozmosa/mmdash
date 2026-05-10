import re


def blocks_to_markdown(blocks: list[dict]) -> str:
    """Convert provider block structures to Markdown."""
    md_lines: list[str] = []
    for block in blocks or []:
        block_type = block.get("type")
        content = block.get("content", "")

        if block_type == "paragraph":
            md_lines.append(content)
        elif block_type == "heading_1":
            md_lines.append(f"# {content}")
        elif block_type == "heading_2":
            md_lines.append(f"## {content}")
        elif block_type == "heading_3":
            md_lines.append(f"### {content}")
        elif block_type == "bulleted_list_item":
            md_lines.append(f"- {content}")
        elif block_type == "numbered_list_item":
            md_lines.append(f"1. {content}")
        elif block_type == "code":
            language = block.get("language", "")
            md_lines.append(f"```{language}\n{content}\n```")
        elif block_type == "equation":
            md_lines.append(f"$$ {content} $$")
        elif block_type == "quote":
            md_lines.append(f"> {content}")
        elif block_type == "divider":
            md_lines.append("---")

    return "\n".join(md_lines)


def content_to_markdown(content: dict) -> str:
    """Return provider markdown without losing user-authored blank lines."""
    markdown = content.get("markdown")
    if markdown is None:
        markdown = content.get("content")
    if markdown is not None:
        return markdown
    return blocks_to_markdown(content.get("blocks", []))


def markdown_to_blocks(markdown: str) -> list[dict]:
    """Parse the Markdown subset used by the model page into blocks."""
    blocks: list[dict] = []
    lines = (markdown or "").split("\n")
    i = 0

    while i < len(lines):
        line = lines[i]
        stripped = line.strip()

        if not stripped:
            i += 1
            continue

        if stripped.startswith("```"):
            language = stripped[3:].strip()
            code_lines: list[str] = []
            i += 1
            while i < len(lines) and not lines[i].strip().startswith("```"):
                code_lines.append(lines[i])
                i += 1
            blocks.append({
                "type": "code",
                "content": "\n".join(code_lines),
                "language": language,
            })
        elif stripped.startswith("# "):
            blocks.append({"type": "heading_1", "content": stripped[2:].strip()})
        elif stripped.startswith("## "):
            blocks.append({"type": "heading_2", "content": stripped[3:].strip()})
        elif stripped.startswith("### "):
            blocks.append({"type": "heading_3", "content": stripped[4:].strip()})
        elif stripped.startswith("- ") or stripped.startswith("* "):
            blocks.append({"type": "bulleted_list_item", "content": stripped[2:].strip()})
        elif re.match(r"^\d+\.\s+", stripped):
            blocks.append({
                "type": "numbered_list_item",
                "content": re.sub(r"^\d+\.\s+", "", stripped).strip(),
            })
        elif stripped.startswith("> "):
            blocks.append({"type": "quote", "content": stripped[2:].strip()})
        elif stripped in {"---", "***"}:
            blocks.append({"type": "divider", "content": ""})
        elif stripped.startswith("$$") and stripped.endswith("$$"):
            blocks.append({"type": "equation", "content": stripped[2:-2].strip()})
        else:
            blocks.append({"type": "paragraph", "content": stripped})

        i += 1

    return blocks
