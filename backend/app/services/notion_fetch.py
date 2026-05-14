import httpx
from app.services.cache import get_cached_notion_page, set_cached_notion_page


async def fetch_notion_page_content(page_id: str, access_token: str) -> dict:
    """Fetch Notion page content (blocks) with caching."""
    cached = get_cached_notion_page(page_id)
    if cached:
        return cached

    async with httpx.AsyncClient() as client:
        # Fetch page blocks
        blocks_resp = await client.get(
            f"https://api.notion.com/v1/blocks/{page_id}/children",
            headers={
                "Authorization": f"Bearer {access_token}",
                "Notion-Version": "2022-06-28",
            },
        )
        blocks_resp.raise_for_status()
        blocks_data = blocks_resp.json()

        result = {
            "page_id": page_id,
            "blocks": blocks_data.get("results", []),
        }
        set_cached_notion_page(page_id, result)
        return result


async def fetch_notion_page_metadata(page_id: str, access_token: str) -> dict:
    """Fetch Notion page metadata."""
    async with httpx.AsyncClient() as client:
        resp = await client.get(
            f"https://api.notion.com/v1/pages/{page_id}",
            headers={
                "Authorization": f"Bearer {access_token}",
                "Notion-Version": "2022-06-28",
            },
        )
        resp.raise_for_status()
        return resp.json()


def notion_blocks_to_markdown(blocks: list) -> str:
    """Convert Notion blocks to Markdown string."""
    md_lines = []
    for block in blocks:
        block_type = block.get("type")
        if block_type == "paragraph":
            text = _extract_rich_text(block.get("paragraph", {}).get("rich_text", []))
            md_lines.append(text)
        elif block_type == "heading_1":
            text = _extract_rich_text(block.get("heading_1", {}).get("rich_text", []))
            md_lines.append(f"# {text}")
        elif block_type == "heading_2":
            text = _extract_rich_text(block.get("heading_2", {}).get("rich_text", []))
            md_lines.append(f"## {text}")
        elif block_type == "heading_3":
            text = _extract_rich_text(block.get("heading_3", {}).get("rich_text", []))
            md_lines.append(f"### {text}")
        elif block_type == "bulleted_list_item":
            text = _extract_rich_text(block.get("bulleted_list_item", {}).get("rich_text", []))
            md_lines.append(f"- {text}")
        elif block_type == "numbered_list_item":
            text = _extract_rich_text(block.get("numbered_list_item", {}).get("rich_text", []))
            md_lines.append(f"1. {text}")
        elif block_type == "code":
            text = _extract_rich_text(block.get("code", {}).get("rich_text", []))
            lang = block.get("code", {}).get("language", "")
            if lang == "markdown":
                # Preserved markdown content (tables, etc.) — render as-is
                md_lines.append(text)
            else:
                md_lines.append(f"```{lang}\n{text}\n```")
        elif block_type == "equation":
            text = block.get("equation", {}).get("expression", "")
            md_lines.append(f"$$ {text} $$")
        elif block_type == "quote":
            text = _extract_rich_text(block.get("quote", {}).get("rich_text", []))
            md_lines.append(f"> {text}")
        elif block_type == "divider":
            md_lines.append("---")
    return "\n\n".join(md_lines)


def _extract_rich_text(rich_text: list) -> str:
    parts = []
    for t in rich_text:
        if t.get("type") == "equation":
            parts.append(f"${t.get('equation', {}).get('expression', '')}$")
        elif t.get("plain_text"):
            parts.append(t["plain_text"])
        elif t.get("text", {}).get("content"):
            parts.append(t["text"]["content"])
    return "".join(parts)
