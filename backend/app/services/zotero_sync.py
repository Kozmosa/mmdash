import json
import asyncio
import logging
import re
from datetime import datetime

import httpx
from sqlalchemy.orm import Session

from app.database import SessionLocal
from app.models import Citation, ZoteroConfig
from app.services.bibtex import item_type_to_bibtex

logger = logging.getLogger(__name__)

_stop_event = asyncio.Event()

ZOTERO_API_BASE = "https://api.zotero.org"


def format_authors(creators: list[dict]) -> str:
    authors = []
    for c in creators:
        if c.get("creatorType") == "author":
            if c.get("lastName") and c.get("firstName"):
                authors.append(f"{c['lastName']}, {c['firstName']}")
            elif c.get("lastName"):
                authors.append(c["lastName"])
            elif c.get("name"):
                authors.append(c["name"])
    return " and ".join(authors)


def extract_year(date_str: str | None) -> int | None:
    if not date_str:
        return None
    match = re.search(r"\b(19|20)\d{2}\b", date_str)
    return int(match.group(0)) if match else None


def parse_zotero_item(item: dict) -> dict:
    data = item.get("data", {})
    return {
        "zotero_item_key": item.get("key"),
        "zotero_version": item.get("version"),
        "title": data.get("title", ""),
        "authors": format_authors(data.get("creators", [])),
        "journal": data.get("publicationTitle") or data.get("proceedingsTitle") or data.get("bookTitle"),
        "year": extract_year(data.get("date")),
        "volume": data.get("volume"),
        "issue": data.get("issue"),
        "pages": data.get("pages"),
        "doi": data.get("DOI"),
        "url": data.get("url"),
        "abstract": data.get("abstractNote"),
        "bibtex_type": item_type_to_bibtex(data.get("itemType", "")),
        "source": "zotero",
        "extra_data": json.dumps(item),
    }


async def fetch_zotero_items(config: ZoteroConfig) -> tuple[list[dict], int]:
    url = f"{ZOTERO_API_BASE}/{config.library_type}s/{config.library_id}/items"
    headers = {"Zotero-API-Key": config.api_key}
    params = {"since": config.last_sync_version or 0, "format": "json", "v": 3, "limit": 100}

    all_items = []
    last_version = config.last_sync_version or 0

    async with httpx.AsyncClient(timeout=30.0) as client:
        start = 0
        while True:
            params["start"] = start
            response = await client.get(url, headers=headers, params=params)
            response.raise_for_status()

            items = response.json()
            if not items:
                break

            all_items.extend(items)
            last_version = int(response.headers.get("Last-Modified-Version", last_version))
            start += len(items)

    return all_items, last_version


async def sync_zotero_for_project(project_id: str, db: Session) -> dict:
    config = db.query(ZoteroConfig).filter(ZoteroConfig.project_id == project_id).first()
    if not config:
        return {"status": "no_config"}

    if config.last_sync_status == "syncing":
        return {"status": "syncing"}

    config.last_sync_status = "syncing"
    config.last_sync_error = None
    db.commit()

    try:
        items, last_version = await fetch_zotero_items(config)

        new_count = 0
        updated_count = 0

        for item in items:
            parsed = parse_zotero_item(item)
            existing = db.query(Citation).filter(
                Citation.project_id == project_id,
                Citation.zotero_item_key == parsed["zotero_item_key"],
            ).first()

            if existing:
                bibtex_key = existing.bibtex_key
                for key, value in parsed.items():
                    setattr(existing, key, value)
                existing.bibtex_key = bibtex_key
                updated_count += 1
            else:
                citation = Citation(
                    project_id=project_id,
                    user_id=config.project.owner_id,
                    **parsed,
                )
                db.add(citation)
                new_count += 1

        config.last_sync_version = last_version
        config.last_sync_at = datetime.utcnow()
        config.last_sync_status = "idle"
        config.last_sync_error = None
        db.commit()

        return {"status": "success", "new": new_count, "updated": updated_count}

    except Exception as e:
        logger.exception(f"Zotero sync failed for project {project_id}")
        config.last_sync_status = "error"
        config.last_sync_error = str(e)[:500]
        db.commit()
        raise


async def run_sync_for_all_projects():
    with SessionLocal() as db:
        configs = db.query(ZoteroConfig).filter(ZoteroConfig.last_sync_status != "syncing").all()
        for config in configs:
            try:
                await sync_zotero_for_project(config.project_id, db)
            except Exception:
                logger.exception(f"Background sync failed for project {config.project_id}")


async def start_sync_scheduler():
    while not _stop_event.is_set():
        try:
            await asyncio.wait_for(_stop_event.wait(), timeout=900)
        except asyncio.TimeoutError:
            await run_sync_for_all_projects()


def stop_sync_scheduler():
    _stop_event.set()
