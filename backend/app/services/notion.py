import json
from typing import Any

import httpx
from app.core.config import get_settings

settings = get_settings()


async def exchange_code_for_token(code: str) -> dict:
    """Exchange Notion OAuth code for access token."""
    async with httpx.AsyncClient() as client:
        resp = await client.post(
            "https://api.notion.com/v1/oauth/token",
            auth=(settings.NOTION_CLIENT_ID, settings.NOTION_CLIENT_SECRET),
            json={
                "grant_type": "authorization_code",
                "code": code,
                "redirect_uri": settings.NOTION_REDIRECT_URI,
            },
            headers={
                "Content-Type": "application/json",
            },
        )
        resp.raise_for_status()
        return resp.json()


async def search_accessible_pages(access_token: str) -> list[dict]:
    """Search for pages the integration can access. Returns list of {id, title}."""
    async with httpx.AsyncClient() as client:
        resp = await client.post(
            "https://api.notion.com/v1/search",
            headers={
                "Authorization": f"Bearer {access_token}",
                "Notion-Version": "2022-06-28",
                "Content-Type": "application/json",
            },
            json={
                "filter": {"property": "object", "value": "page"},
                "page_size": 50,
            },
        )
        resp.raise_for_status()
        data = resp.json()
        pages = []
        for result in data.get("results", []):
            title_parts = []
            for prop_name, prop_value in result.get("properties", {}).items():
                if prop_value.get("type") == "title":
                    title_parts.extend(
                        t.get("plain_text", "") for t in prop_value.get("title", [])
                    )
            pages.append({
                "id": result["id"],
                "title": "".join(title_parts) or "(untitled)",
            })
        return pages


NOTION_HEADERS = {"Notion-Version": "2022-06-28", "Content-Type": "application/json"}

import re


def _parse_inline_rich_text(text: str) -> list[dict]:
    """Parse a single line of markdown into Notion rich_text objects with inline formatting."""
    if not text:
        return []

    patterns = [
        # (<name>, <regex>, <annotations dict>, <is_equation>)
        ("bold_italic", r"\*\*\*(.+?)\*\*\*", {"bold": True, "italic": True}, False),
        ("bold", r"\*\*(.+?)\*\*", {"bold": True}, False),
        ("italic", r"(?<!\*)\*(?!\*)(.+?)(?<!\*)\*(?!\*)", {"italic": True}, False),
        ("code", r"`(.+?)`", {"code": True}, False),
        ("link", r"\[(.+?)\]\((.+?)\)", {}, False),
        ("inline_math", r"\$(.+?)\$", {}, True),
    ]

    tokens = []
    remaining = text
    while remaining:
        earliest = None
        earliest_info = None
        for name, pattern, annotations, is_equation in patterns:
            m = re.search(pattern, remaining)
            if m and (earliest is None or m.start() < earliest):
                earliest = m.start()
                earliest_info = (m, annotations, name, is_equation)

        if earliest is None or earliest > 0:
            prefix = remaining[:earliest] if earliest is not None else remaining
            if prefix:
                tokens.append({"type": "text", "text": {"content": prefix}})
            if earliest is None:
                break

        m, annotations, name, is_equation = earliest_info

        if is_equation:
            tokens.append({
                "type": "equation",
                "equation": {"expression": m.group(1)},
            })
        elif name == "link":
            label = m.group(1)
            url = m.group(2)
            tokens.append({"type": "text", "text": {"content": label, "link": {"url": url}}})
        else:
            tokens.append({
                "type": "text",
                "text": {"content": m.group(1)},
                "annotations": annotations,
            })
        remaining = remaining[m.end():]

    return tokens or [{"type": "text", "text": {"content": ""}}]


def _rich_text_block(block_type: str, text: str) -> dict:
    """Create a block with inline-formatted rich text."""
    return {
        "object": "block",
        "type": block_type,
        block_type: {"rich_text": _parse_inline_rich_text(text)},
    }


def markdown_to_notion_blocks(markdown: str) -> list[dict]:
    """Convert a markdown string to Notion blocks with stateful, multi-line parsing."""
    blocks = []
    lines = markdown.split("\n")
    i = 0
    while i < len(lines):
        line = lines[i]

        # Equation block $$...$$
        if line.strip().startswith("$$") and line.strip() != "$$":
            expr = line.strip()[2:].strip()
            blocks.append({
                "object": "block",
                "type": "equation",
                "equation": {"expression": expr.rstrip("$").rstrip()},
            })
            i += 1
            continue
        if line.strip() == "$$":
            eq_lines = []
            i += 1
            while i < len(lines) and lines[i].strip() != "$$":
                eq_lines.append(lines[i].strip())
                i += 1
            if i < len(lines):
                i += 1  # skip closing $$
            blocks.append({
                "object": "block",
                "type": "equation",
                "equation": {"expression": "\n".join(eq_lines)},
            })
            continue

        # Table: consecutive lines starting with | (at least header + separator + one row)
        if line.strip().startswith("|") and line.strip().endswith("|"):
            table_lines = [line]
            j = i + 1
            while j < len(lines) and lines[j].strip().startswith("|"):
                table_lines.append(lines[j])
                j += 1
            if len(table_lines) >= 2:  # At least header + separator
                table_text = "\n".join(table_lines)
                blocks.append({
                    "object": "block",
                    "type": "code",
                    "code": {
                        "language": "markdown",
                        "rich_text": [{"type": "text", "text": {"content": table_text}}],
                    },
                })
                i = j
                continue
            # Fall through to paragraph if only one | line (inline math might start with |)

        # Code block
        if line.startswith("```"):
            lang = line[3:].strip()
            code_lines = []
            i += 1
            while i < len(lines) and not lines[i].startswith("```"):
                code_lines.append(lines[i])
                i += 1
            i += 1  # skip closing ```
            code_text = "\n".join(code_lines)
            blocks.append({
                "object": "block",
                "type": "code",
                "code": {
                    "language": lang or "plain text",
                    "rich_text": [{"type": "text", "text": {"content": code_text}}],
                },
            })
            continue

        # Divider
        if line.strip() == "---":
            blocks.append({"object": "block", "type": "divider", "divider": {}})
            i += 1
            continue

        # Blank line: skip, may join surrounding paragraphs
        if line.strip() == "":
            i += 1
            continue

        # Numbered list
        m = re.match(r"^(\d+)\.\s+(.+)", line)
        if m:
            blocks.append(_rich_text_block("numbered_list_item", m.group(2)))
            i += 1
            continue

        # Bulleted list
        if line.startswith("- "):
            blocks.append(_rich_text_block("bulleted_list_item", line[2:]))
            i += 1
            continue

        # Headings
        if line.startswith("### "):
            blocks.append(_rich_text_block("heading_3", line[4:]))
            i += 1
            continue
        if line.startswith("## "):
            blocks.append(_rich_text_block("heading_2", line[3:]))
            i += 1
            continue
        if line.startswith("# "):
            blocks.append(_rich_text_block("heading_1", line[2:]))
            i += 1
            continue

        # Blockquote
        if line.startswith("> "):
            quote_lines = [line[2:]]
            i += 1
            while i < len(lines) and lines[i].startswith("> "):
                quote_lines.append(lines[i][2:])
                i += 1
            blocks.append(_rich_text_block("quote", " ".join(quote_lines)))
            continue

        # Paragraph: collect consecutive non-empty, non-special lines
        para_lines = [line]
        i += 1
        while i < len(lines) and lines[i].strip() and not _is_special_line(lines[i]):
            para_lines.append(lines[i])
            i += 1
        blocks.append(_rich_text_block("paragraph", " ".join(para_lines)))

    return blocks


def _is_special_line(line: str) -> bool:
    stripped = line.strip()
    if not stripped:
        return True
    if re.match(r"^(#{1,3}\s|```|> |\- |\d+\.\s|---)", stripped):
        return True
    return False


async def clear_page_children(page_id: str, access_token: str) -> None:
    """Remove all child blocks from a page."""
    async with httpx.AsyncClient() as client:
        next_cursor = None
        while True:
            params = {"page_size": 100}
            if next_cursor:
                params["start_cursor"] = next_cursor
            resp = await client.get(
                f"https://api.notion.com/v1/blocks/{page_id}/children",
                headers={"Authorization": f"Bearer {access_token}", **NOTION_HEADERS},
                params=params,
            )
            resp.raise_for_status()
            data = resp.json()
            for block in data.get("results", []):
                try:
                    await client.delete(
                        f"https://api.notion.com/v1/blocks/{block['id']}",
                        headers={"Authorization": f"Bearer {access_token}", **NOTION_HEADERS},
                    )
                except Exception:
                    pass  # Some blocks (e.g., child databases) cannot be deleted
            if not data.get("has_more"):
                break
            next_cursor = data.get("next_cursor")


async def append_page_children(page_id: str, blocks: list[dict], access_token: str) -> dict:
    """Append blocks to a page in chunks of 100 (Notion API limit)."""
    CHUNK_SIZE = 100
    last_result = {}
    async with httpx.AsyncClient() as client:
        for i in range(0, len(blocks), CHUNK_SIZE):
            chunk = blocks[i : i + CHUNK_SIZE]
            resp = await client.patch(
                f"https://api.notion.com/v1/blocks/{page_id}/children",
                headers={"Authorization": f"Bearer {access_token}", **NOTION_HEADERS},
                json={"children": chunk},
            )
            resp.raise_for_status()
            last_result = resp.json()
    return last_result


async def _get_all_children(page_id: str, access_token: str) -> list[dict]:
    """Fetch all child blocks of a page (with pagination)."""
    all_children = []
    async with httpx.AsyncClient() as client:
        next_cursor = None
        while True:
            params = {"page_size": 100}
            if next_cursor:
                params["start_cursor"] = next_cursor
            resp = await client.get(
                f"https://api.notion.com/v1/blocks/{page_id}/children",
                headers={"Authorization": f"Bearer {access_token}", **NOTION_HEADERS},
                params=params,
            )
            resp.raise_for_status()
            data = resp.json()
            all_children.extend(data.get("results", []))
            if not data.get("has_more"):
                break
            next_cursor = data.get("next_cursor")
    return all_children


def _blocks_equivalent(curr: dict, desired: dict) -> bool:
    """Check if two Notion blocks have the same content (ignoring id)."""
    if curr.get("type") != desired.get("type"):
        return False
    # Compare the type-specific content dict, ignoring block ids
    block_type = curr["type"]
    curr_content = curr.get(block_type, {})
    desired_content = desired.get(block_type, {})
    return json_dumps_stable(curr_content) == json_dumps_stable(desired_content)


def json_dumps_stable(obj: dict) -> str:
    return json.dumps(obj, sort_keys=True, ensure_ascii=False)


async def update_block(block_id: str, block: dict, access_token: str) -> None:
    """Update a block's content in-place (same type required)."""
    block_type = block["type"]
    payload: dict[str, Any] = {block_type: block[block_type]}
    async with httpx.AsyncClient() as client:
        resp = await client.patch(
            f"https://api.notion.com/v1/blocks/{block_id}",
            headers={"Authorization": f"Bearer {access_token}", **NOTION_HEADERS},
            json=payload,
        )
        resp.raise_for_status()


async def delete_block(block_id: str, access_token: str) -> None:
    """Delete a single block."""
    async with httpx.AsyncClient() as client:
        try:
            resp = await client.delete(
                f"https://api.notion.com/v1/blocks/{block_id}",
                headers={"Authorization": f"Bearer {access_token}", **NOTION_HEADERS},
            )
            resp.raise_for_status()
        except Exception:
            pass  # Some blocks (e.g., child databases) cannot be deleted


async def diff_and_apply_blocks(page_id: str, desired_blocks: list[dict], access_token: str) -> None:
    """Apply block changes incrementally: PATCH unchanged types, only DELETE/APPEND diffs."""
    current_blocks = await _get_all_children(page_id, access_token)

    max_len = max(len(current_blocks), len(desired_blocks))
    for i in range(max_len):
        if i < len(current_blocks) and i < len(desired_blocks):
            curr, desired = current_blocks[i], desired_blocks[i]
            if curr["type"] == desired["type"]:
                if not _blocks_equivalent(curr, desired):
                    await update_block(curr["id"], desired, access_token)
            else:
                await delete_block(curr["id"], access_token)
                # Append the new block after the previous block (or at top if i==0)
                after_id = current_blocks[i - 1]["id"] if i > 0 else None
                await _append_after(page_id, [desired], access_token, after_id)
                # Insert into current_blocks to keep indices aligned
                current_blocks.insert(i, desired)
        elif i < len(current_blocks):
            await delete_block(current_blocks[i]["id"], access_token)
        else:
            after_id = current_blocks[-1]["id"] if current_blocks else None
            await _append_after(page_id, desired_blocks[i:], access_token, after_id)
            break


async def _append_after(page_id: str, blocks: list[dict], access_token: str, after_id: str | None) -> dict:
    """Append blocks after a specific block, or at the top if after_id is None."""
    CHUNK_SIZE = 100
    last_result = {}
    async with httpx.AsyncClient() as client:
        for i in range(0, len(blocks), CHUNK_SIZE):
            chunk = blocks[i : i + CHUNK_SIZE]
            body: dict[str, Any] = {"children": chunk}
            if after_id:
                body["after"] = after_id
            resp = await client.patch(
                f"https://api.notion.com/v1/blocks/{page_id}/children",
                headers={"Authorization": f"Bearer {access_token}", **NOTION_HEADERS},
                json=body,
            )
            resp.raise_for_status()
            last_result = resp.json()
            # For subsequent chunks, append after the last inserted block
            results = last_result.get("results", [])
            if results:
                after_id = results[-1]["id"]
    return last_result


async def create_page(parent_page_id: str, title: str, access_token: str) -> str:
    """Create a new Notion page under parent_page_id. Returns the new page id."""
    async with httpx.AsyncClient() as client:
        resp = await client.post(
            "https://api.notion.com/v1/pages",
            headers={
                "Authorization": f"Bearer {access_token}",
                "Notion-Version": "2022-06-28",
                "Content-Type": "application/json",
            },
            json={
                "parent": {"page_id": parent_page_id},
                "properties": {
                    "title": {
                        "title": [{"text": {"content": title}}]
                    }
                },
            },
        )
        resp.raise_for_status()
        data = resp.json()
        return data["id"]
