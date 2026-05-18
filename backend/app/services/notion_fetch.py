import httpx

from app.services.cache import get_cached_notion_page, set_cached_notion_page
from app.services.notion import _CLIENT_TIMEOUT, _get_all_children


async def fetch_notion_page_content(page_id: str, access_token: str) -> dict:
    """Fetch Notion page content (blocks) with caching and full pagination."""
    cached = get_cached_notion_page(page_id)
    if cached:
        return cached

    blocks = await _get_all_children(page_id, access_token)

    result = {
        "page_id": page_id,
        "blocks": blocks,
    }
    set_cached_notion_page(page_id, result)
    return result


async def fetch_notion_page_metadata(page_id: str, access_token: str) -> dict:
    """Fetch Notion page metadata."""
    async with httpx.AsyncClient(timeout=_CLIENT_TIMEOUT) as client:
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
        elif block_type == "table":
            # Real Notion table blocks have nested table_row children that
            # aren't fetched by the flat block list.  Render as a placeholder
            # so the table isn't silently lost.
            md_lines.append("*(table)*")
        elif block_type == "table_row":
            cells = block.get("table_row", {}).get("cells", [])
            row = " | ".join(_extract_rich_text(cell) for cell in cells)
            md_lines.append(f"| {row} |")
    return "\n\n".join(md_lines)


def _extract_rich_text(rich_text: list) -> str:
    """Extract plain text from a Notion rich_text array.

    Tries multiple paths because the ``or``-chain pattern
    (``plain_text or text.content or ""``) silently produces
    empty strings when both fields are falsy, which can happen
    for edge-case block types or API version skew.
    """
    parts = []
    for t in rich_text:
        rt_type = t.get("type", "")

        if rt_type == "equation":
            expr = t.get("equation", {}).get("expression", "")
            parts.append(f"${expr}$")
            continue

        # 1) Prefer plain_text (Notion fills it on every text-bearing RT)
        text = t.get("plain_text")
        if text:
            pass
        # 2) Fall back to text.content for mentions / edge cases
        elif isinstance(t.get("text"), dict):
            text = t["text"].get("content") or ""
        # 3) Last resort: check href for link-type RTs
        elif t.get("href"):
            text = t["href"]
        else:
            text = ""

        if not text:
            _logger.warning("NOTION-READ-DIAG: RT item yielded no text: type=%s keys=%s raw=%s",
                            rt_type, list(t.keys()), _json.dumps(t, ensure_ascii=False)[:300])
            continue

        ann = t.get("annotations") or {}
        if ann.get("code"):
            text = f"`{text}`"
        if ann.get("bold") and ann.get("italic"):
            text = f"***{text}***"
        elif ann.get("bold"):
            text = f"**{text}**"
        elif ann.get("italic"):
            text = f"*{text}*"
        if ann.get("strikethrough"):
            text = f"~~{text}~~"
        # link can be null in Notion API responses — get() returns None, not default
        txt_obj = t.get("text")
        if isinstance(txt_obj, dict):
            link_obj = txt_obj.get("link")
            if isinstance(link_obj, dict):
                link_url = link_obj.get("url")
                if link_url:
                    text = f"[{text}]({link_url})"
        parts.append(text)
    return "".join(parts)
