"""Normalize Core-exported Notion pages into immutable Model snapshots."""

from __future__ import annotations

import asyncio
import hashlib
import json
from collections.abc import Mapping, Sequence
from typing import Any, Protocol
from urllib.parse import unquote, urlsplit

from mmdash_worker.jobs.handlers import HandlerContext, HandlerError

MAX_RESULT_BLOCKS = 20_000
MAX_TEXT_CHARS = 4_000_000


class ModelExportClient(Protocol):
    def get_model_notion_export(self, job_id: str) -> dict[str, Any]: ...


class ModelNotionHandler:
    """Fetches secret-free Notion exports from Core and normalizes them."""

    def __init__(self, client: ModelExportClient) -> None:
        self.client = client

    async def __call__(
        self,
        context: HandlerContext,
        payload: Mapping[str, Any],
    ) -> Mapping[str, Any]:
        del payload
        return await asyncio.to_thread(self._run, context)

    def _run(self, context: HandlerContext) -> Mapping[str, Any]:
        if context.cancellation_requested:
            raise HandlerError("JOB_CANCELLED", "Model synchronization was cancelled")
        exported = self.client.get_model_notion_export(context.job_id)
        mode = _required_string(exported, "mode")
        sync_id = _required_string(exported, "sync_id")
        pages = exported.get("pages")
        if not isinstance(pages, list) or len(pages) > 1_000:
            raise HandlerError("MODEL_NOTION_INVALID_EXPORT", "Notion export is invalid")
        if mode == "discover":
            return _discover_result(sync_id, _required_string(exported, "root_title"), pages)
        if mode != "snapshot":
            raise HandlerError("MODEL_NOTION_INVALID_EXPORT", "Notion export mode is invalid")
        question_id = _required_string(exported, "question_id")
        return _snapshot_result(sync_id, question_id, pages)


def _discover_result(sync_id: str, root_title: str, pages: Sequence[Any]) -> dict[str, Any]:
    discovered: list[dict[str, Any]] = []
    for raw in pages:
        page = _mapping(raw)
        page_id = _required_string(page, "page_id")
        parent = page.get("parent_page_id")
        if parent is not None and not isinstance(parent, str):
            raise HandlerError("MODEL_NOTION_INVALID_EXPORT", "Notion parent page is invalid")
        blocks = page.get("blocks")
        discovered.append(
            {
                "page_id": page_id,
                "parent_page_id": parent or None,
                "title": _required_string(page, "title")[:255],
                "url": _required_string(page, "url"),
                "depth": _bounded_int(page.get("depth"), 1, 64),
                "has_children": _contains_child_page(blocks),
            }
        )
    return {
        "mode": "discover",
        "sync_id": sync_id,
        "root_title": root_title[:255],
        "pages": discovered,
    }


def _snapshot_result(
    sync_id: str,
    question_id: str,
    pages: Sequence[Any],
) -> dict[str, Any]:
    if not pages:
        raise HandlerError("MODEL_NOTION_EMPTY", "Notion model page is empty")
    first = _mapping(pages[0])
    title = _required_string(first, "title")[:255]
    blocks: list[dict[str, Any]] = []
    outline: list[dict[str, Any]] = []
    media: list[dict[str, Any]] = []
    markdown_parts: list[str] = []
    text_parts: list[str] = []
    for page_index, raw_page in enumerate(pages):
        page = _mapping(raw_page)
        if page_index > 0:
            page_title = _required_string(page, "title")[:255]
            level = min(3, max(1, _bounded_int(page.get("depth"), 1, 64)))
            page_block = {
                "block_id": f"page:{_required_string(page, 'page_id')}",
                "type": f"heading_{level}",
                "text": page_title,
                "level": level,
                "rich_text": [{"text": page_title}],
                "children": [],
            }
            blocks.append(page_block)
            outline.append(
                {"block_id": page_block["block_id"], "title": page_title, "level": level}
            )
            markdown_parts.append("#" * level + " " + page_title)
            text_parts.append(page_title)
        raw_blocks = page.get("blocks")
        if not isinstance(raw_blocks, list):
            raise HandlerError("MODEL_NOTION_INVALID_EXPORT", "Notion blocks are invalid")
        for raw_block in raw_blocks:
            normalized = _normalize_block(_mapping(raw_block), outline, media)
            if normalized is None:
                continue
            blocks.append(normalized)
            markdown = _block_markdown_tree(normalized)
            if markdown:
                markdown_parts.append(markdown)
            text_parts.extend(_block_texts(normalized))
            if _count_blocks(blocks) > MAX_RESULT_BLOCKS:
                raise HandlerError("MODEL_NOTION_TOO_LARGE", "Notion model has too many blocks")
    content_text = "\n".join(text_parts)
    if len(content_text) > MAX_TEXT_CHARS:
        raise HandlerError("MODEL_NOTION_TOO_LARGE", "Notion model text is too large")
    canonical = _canonical_blocks(blocks)
    canonical_media = _canonical_media(media)
    content_hash = hashlib.sha256(
        json.dumps(
            {"title": title, "blocks": canonical, "media": canonical_media},
            ensure_ascii=False,
            sort_keys=True,
            separators=(",", ":"),
        ).encode()
    ).hexdigest()
    for item in media:
        item.pop("_temporary", None)
        item.pop("_revision", None)
    summary = " ".join(content_text.split())[:500]
    return {
        "mode": "snapshot",
        "sync_id": sync_id,
        "question_id": question_id,
        "title": title,
        "content_hash": content_hash,
        "summary": summary,
        "outline": outline,
        "blocks": blocks,
        "content_markdown": "\n\n".join(markdown_parts),
        "content_text": content_text,
        "media": media,
    }


def _normalize_block(
    block: Mapping[str, Any],
    outline: list[dict[str, Any]],
    media: list[dict[str, Any]],
) -> dict[str, Any] | None:
    block_id = _required_string(block, "id")
    block_type = _required_string(block, "type")
    data = block.get(block_type)
    if not isinstance(data, Mapping):
        data = {}
    rich = _rich_text(data.get("rich_text"))
    text = _rich_text_value(rich)
    normalized: dict[str, Any] = {
        "block_id": block_id,
        "type": block_type,
        "text": text,
        "rich_text": rich,
        "children": [],
    }
    if block_type.startswith("heading_"):
        level = int(block_type[-1])
        normalized["level"] = level
        outline.append({"block_id": block_id, "title": text, "level": level})
    elif block_type == "code":
        normalized["language"] = str(data.get("language", "plain text"))[:100]
    elif block_type in {"equation"}:
        expression = str(data.get("expression", ""))
        normalized["expression"] = expression
        normalized["text"] = expression
    elif block_type == "to_do":
        normalized["checked"] = bool(data.get("checked", False))
    elif block_type == "table_row":
        cells = data.get("cells")
        if isinstance(cells, list):
            rich_cells = [_rich_text(cell) for cell in cells]
            row = [_rich_text_value(cell) for cell in rich_cells]
            normalized["rows"] = [row]
            normalized["cells"] = rich_cells
            normalized["text"] = "\t".join(row)
    elif block_type in {"bookmark", "link_preview"}:
        target_url = data.get("url")
        if not isinstance(target_url, str) or not target_url.startswith(("https://", "http://")):
            raise HandlerError("MODEL_NOTION_LINK_INVALID", "Notion link card URL is invalid")
        caption = "".join(item["text"] for item in _rich_text(data.get("caption")))
        normalized["url"] = target_url
        normalized["caption"] = caption
        normalized["text"] = caption or target_url
    elif block_type in {"image", "file", "pdf"}:
        source_url = _file_url(data)
        if not source_url:
            raise HandlerError("MODEL_NOTION_MEDIA_INVALID", "Notion media URL is missing")
        filename = _media_filename(data, source_url, block_type)
        mime_type = {"image": "image/*", "pdf": "application/pdf"}.get(
            block_type, "application/octet-stream"
        )
        caption = "".join(item["text"] for item in _rich_text(data.get("caption")))
        normalized["caption"] = caption
        media.append(
            {
                "source_block_id": block_id,
                "url": source_url,
                "filename": filename,
                "mime_type": mime_type,
                "_temporary": data.get("type") == "file",
                "_revision": str(block.get("last_edited_time", "")),
            }
        )
    children = block.get("children")
    if isinstance(children, list):
        for child in children:
            nested = _normalize_block(_mapping(child), outline, media)
            if nested is not None:
                normalized["children"].append(nested)
    if block_type in {"child_page", "child_database"}:
        return None
    return normalized


def _rich_text(raw: Any) -> list[dict[str, Any]]:
    if not isinstance(raw, list):
        return []
    result: list[dict[str, Any]] = []
    for item_raw in raw:
        item = _mapping(item_raw)
        text = item.get("plain_text")
        if not isinstance(text, str):
            continue
        annotations = item.get("annotations")
        if not isinstance(annotations, Mapping):
            annotations = {}
        value: dict[str, Any] = {"text": text}
        if item.get("type") == "equation":
            equation = item.get("equation")
            if isinstance(equation, Mapping) and isinstance(equation.get("expression"), str):
                value["expression"] = str(equation["expression"])
        for key in ("bold", "italic", "strikethrough", "underline", "code"):
            if annotations.get(key) is True:
                value[key] = True
        color = annotations.get("color")
        if isinstance(color, str) and color != "default":
            value["color"] = color
        href = item.get("href")
        if isinstance(href, str) and href.startswith(("https://", "http://")):
            value["href"] = href
        result.append(value)
    return result


def _rich_text_value(parts: Sequence[Mapping[str, Any]]) -> str:
    return "".join(str(item.get("expression") or item.get("text", "")) for item in parts)


def _file_url(data: Mapping[str, Any]) -> str:
    kind = data.get("type")
    source = data.get(kind) if isinstance(kind, str) else None
    if isinstance(source, Mapping) and isinstance(source.get("url"), str):
        return str(source["url"])
    return ""


def _media_filename(data: Mapping[str, Any], source_url: str, block_type: str) -> str:
    name = data.get("name")
    if isinstance(name, str) and name.strip():
        return name.strip()[:255]
    path = unquote(urlsplit(source_url).path).rstrip("/")
    candidate = path.rsplit("/", 1)[-1] if path else ""
    if candidate:
        return candidate[:255]
    return {"image": "notion-image", "pdf": "notion-document.pdf"}.get(block_type, "notion-file")


def _block_markdown(block: Mapping[str, Any]) -> str:
    block_type = str(block.get("type", ""))
    text = str(block.get("text", ""))
    if block_type.startswith("heading_"):
        return "#" * int(block.get("level", 1)) + " " + text
    if block_type == "bulleted_list_item":
        return "- " + text
    if block_type == "numbered_list_item":
        return "1. " + text
    if block_type == "to_do":
        return f"- [{'x' if block.get('checked') else ' '}] {text}"
    if block_type == "quote":
        return "> " + text
    if block_type == "code":
        return f"```{block.get('language', '')}\n{text}\n```"
    if block_type == "equation":
        return "$$\n" + str(block.get("expression", "")) + "\n$$"
    if block_type in {"bookmark", "link_preview"}:
        url = str(block.get("url", ""))
        return f"[{block.get('caption') or url}]({url})"
    if block_type in {"image", "file", "pdf"}:
        return f"[{block.get('caption') or block_type}](artifact:{block.get('block_id')})"
    if block_type == "table":
        rows = [row for child in block.get("children", []) for row in child.get("rows", [])]
        return "\n".join("| " + " | ".join(str(cell) for cell in row) + " |" for row in rows)
    return text


def _block_markdown_tree(block: Mapping[str, Any]) -> str:
    own = _block_markdown(block)
    if block.get("type") == "table":
        return own
    children = [_block_markdown_tree(child) for child in block.get("children", [])]
    return "\n".join(value for value in [own, *children] if value)


def _block_texts(block: Mapping[str, Any]) -> list[str]:
    result = [str(block["text"])] if block.get("text") else []
    for child in block.get("children", []):
        result.extend(_block_texts(child))
    return result


def _canonical_blocks(blocks: Sequence[Mapping[str, Any]]) -> list[dict[str, Any]]:
    ignored = {"artifact_id", "artifact_version_id"}
    return [{key: value for key, value in block.items() if key not in ignored} for block in blocks]


def _canonical_media(media: Sequence[Mapping[str, Any]]) -> list[dict[str, str]]:
    result: list[dict[str, str]] = []
    for item in media:
        raw_url = str(item.get("url", ""))
        if item.get("_temporary"):
            parsed = urlsplit(raw_url)
            raw_url = parsed._replace(query="", fragment="").geturl()
        result.append(
            {
                "source_block_id": str(item.get("source_block_id", "")),
                "location": raw_url,
                "filename": str(item.get("filename", "")),
                "mime_type": str(item.get("mime_type", "")),
                "revision": str(item.get("_revision", "")),
            }
        )
    return sorted(result, key=lambda value: value["source_block_id"])


def _count_blocks(blocks: Sequence[Mapping[str, Any]]) -> int:
    return sum(1 + _count_blocks(block.get("children", [])) for block in blocks)


def _contains_child_page(raw: Any) -> bool:
    if not isinstance(raw, list):
        return False
    for value in raw:
        block = _mapping(value)
        if block.get("type") == "child_page" or _contains_child_page(block.get("children")):
            return True
    return False


def _mapping(value: Any) -> Mapping[str, Any]:
    if not isinstance(value, Mapping):
        raise HandlerError("MODEL_NOTION_INVALID_EXPORT", "Notion export contains invalid data")
    return value


def _required_string(value: Mapping[str, Any], name: str) -> str:
    result = value.get(name)
    if not isinstance(result, str) or not result.strip():
        raise HandlerError("MODEL_NOTION_INVALID_EXPORT", f"Notion {name} is invalid")
    return result.strip()


def _bounded_int(value: Any, minimum: int, maximum: int) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or value < minimum or value > maximum:
        raise HandlerError("MODEL_NOTION_INVALID_EXPORT", "Notion numeric field is invalid")
    return value
