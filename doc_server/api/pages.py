import os
import json
import uuid
import re
from datetime import datetime
from pathlib import Path
from typing import Optional

from fastapi import APIRouter, Depends, HTTPException, Header
from pydantic import BaseModel

from doc_server.core.config import get_settings

router = APIRouter()
settings = get_settings()


def _get_data_dir() -> Path:
    return Path(settings.DATA_DIR)


def _ensure_data_dir():
    _get_data_dir().mkdir(parents=True, exist_ok=True)


def _page_path(page_id: str) -> Path:
    # Sanitize page_id to prevent directory traversal
    safe_id = re.sub(r"[^a-zA-Z0-9._-]", "_", page_id)
    return _get_data_dir() / f"{safe_id}.json"


def _generate_page_id() -> str:
    return f"{uuid.uuid4()}.md"


def _parse_markdown_to_blocks(content: str) -> list[dict]:
    """Simple markdown parser that produces Notion-compatible block structures."""
    blocks = []
    lines = content.split("\n")
    i = 0
    while i < len(lines):
        line = lines[i]
        stripped = line.strip()

        if not stripped:
            i += 1
            continue

        if stripped.startswith("# "):
            blocks.append({
                "type": "heading_1",
                "content": stripped[2:].strip(),
            })
        elif stripped.startswith("## "):
            blocks.append({
                "type": "heading_2",
                "content": stripped[3:].strip(),
            })
        elif stripped.startswith("### "):
            blocks.append({
                "type": "heading_3",
                "content": stripped[4:].strip(),
            })
        elif stripped.startswith("```"):
            # Code block
            lang = stripped[3:].strip()
            code_lines = []
            i += 1
            while i < len(lines) and not lines[i].strip().startswith("```"):
                code_lines.append(lines[i])
                i += 1
            blocks.append({
                "type": "code",
                "content": "\n".join(code_lines),
                "language": lang,
            })
        elif stripped.startswith("- ") or stripped.startswith("* "):
            blocks.append({
                "type": "bulleted_list_item",
                "content": stripped[2:].strip(),
            })
        elif re.match(r"^\d+\.\s+", stripped):
            blocks.append({
                "type": "numbered_list_item",
                "content": re.sub(r"^\d+\.\s+", "", stripped).strip(),
            })
        elif stripped.startswith("> "):
            blocks.append({
                "type": "quote",
                "content": stripped[2:].strip(),
            })
        elif stripped == "---" or stripped == "***":
            blocks.append({
                "type": "divider",
                "content": "",
            })
        elif stripped.startswith("$$") and stripped.endswith("$$"):
            blocks.append({
                "type": "equation",
                "content": stripped[2:-2].strip(),
            })
        else:
            blocks.append({
                "type": "paragraph",
                "content": stripped,
            })
        i += 1

    return blocks


def _load_page(page_id: str) -> Optional[dict]:
    path = _page_path(page_id)
    if not path.exists():
        return None
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def _save_page(page_id: str, data: dict):
    _ensure_data_dir()
    path = _page_path(page_id)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)


async def verify_api_key(X_API_Key: Optional[str] = Header(None)):
    if not settings.API_KEY:
        return None
    if X_API_Key != settings.API_KEY:
        raise HTTPException(status_code=401, detail="Invalid API key")
    return X_API_Key


# ─── Schemas ────────────────────────────────────────────────────────────────

class PageCreate(BaseModel):
    title: str
    content: str = ""


class PageUpdate(BaseModel):
    title: Optional[str] = None
    content: Optional[str] = None


class PageResponse(BaseModel):
    page_id: str
    title: str
    content: str
    blocks: list[dict]
    created_at: str
    updated_at: str


class PageListItem(BaseModel):
    page_id: str
    title: str
    updated_at: str


# ─── Endpoints ───────────────────────────────────────────────────────────────

@router.get("/pages", response_model=list[PageListItem])
def list_pages(_: str = Depends(verify_api_key)):
    _ensure_data_dir()
    results = []
    for path in _get_data_dir().glob("*.json"):
        page_id = path.stem
        data = _load_page(page_id)
        if data:
            results.append(PageListItem(
                page_id=data["page_id"],
                title=data.get("title", ""),
                updated_at=data.get("updated_at", ""),
            ))
    return sorted(results, key=lambda x: x.updated_at, reverse=True)


@router.post("/pages", response_model=dict)
def create_page(data: PageCreate, _: str = Depends(verify_api_key)):
    page_id = _generate_page_id()
    now = datetime.utcnow().isoformat()
    blocks = _parse_markdown_to_blocks(data.content)
    page_data = {
        "page_id": page_id,
        "title": data.title,
        "content": data.content,
        "blocks": blocks,
        "created_at": now,
        "updated_at": now,
    }
    _save_page(page_id, page_data)
    return {"page_id": page_id, **page_data}


@router.get("/pages/{page_id}", response_model=PageResponse)
def get_page(page_id: str, _: str = Depends(verify_api_key)):
    data = _load_page(page_id)
    if not data:
        raise HTTPException(status_code=404, detail="Page not found")
    return PageResponse(**data)


@router.put("/pages/{page_id}", response_model=PageResponse)
def update_page(page_id: str, update: PageUpdate, _: str = Depends(verify_api_key)):
    data = _load_page(page_id)
    if not data:
        raise HTTPException(status_code=404, detail="Page not found")

    if update.title is not None:
        data["title"] = update.title
    if update.content is not None:
        data["content"] = update.content
        data["blocks"] = _parse_markdown_to_blocks(update.content)

    data["updated_at"] = datetime.utcnow().isoformat()
    _save_page(page_id, data)
    return PageResponse(**data)


@router.delete("/pages/{page_id}")
def delete_page(page_id: str, _: str = Depends(verify_api_key)):
    path = _page_path(page_id)
    if not path.exists():
        raise HTTPException(status_code=404, detail="Page not found")
    path.unlink()
    return {"status": "deleted", "page_id": page_id}
