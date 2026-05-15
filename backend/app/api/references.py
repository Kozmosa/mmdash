from datetime import datetime

from fastapi import APIRouter, Depends, HTTPException, Form
from pydantic import BaseModel
from sqlalchemy.orm import Session

from app.database import get_db
from app.models import Project, TeamMember, User, Citation, ZoteroConfig
from app.api.auth import get_current_user
from app.services.bibtex import generate_bibtex_batch
from app.services.zotero_sync import sync_zotero_for_project

router = APIRouter()


class ExportRequest(BaseModel):
    ids: list[str] | None = None


def _check_project_access(project_id: str, user: User, db: Session):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(
        TeamMember.team_id == project.team_id,
        TeamMember.user_id == user.id,
    ).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    return project


# ─── Zotero config routes (must come before /{project_id}/{citation_id}) ─────

@router.get("/{project_id}/zotero-config")
def get_zotero_config(
    project_id: str,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)
    config = db.query(ZoteroConfig).filter(ZoteroConfig.project_id == project_id).first()
    if not config:
        raise HTTPException(status_code=404, detail="Zotero config not found")
    return {
        "library_id": config.library_id,
        "library_type": config.library_type,
        "api_key_masked": config.api_key[:4] + "****" if config.api_key else "",
        "last_sync_at": config.last_sync_at.isoformat() if config.last_sync_at else None,
        "last_sync_status": config.last_sync_status,
        "last_sync_error": config.last_sync_error,
    }


@router.post("/{project_id}/zotero-config")
def set_zotero_config(
    project_id: str,
    api_key: str = Form(...),
    library_id: str = Form(...),
    library_type: str = Form("user"),
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)

    config = db.query(ZoteroConfig).filter(ZoteroConfig.project_id == project_id).first()
    if config:
        config.api_key = api_key
        config.library_id = library_id
        config.library_type = library_type
    else:
        config = ZoteroConfig(
            project_id=project_id,
            api_key=api_key,
            library_id=library_id,
            library_type=library_type,
        )
        db.add(config)

    db.commit()
    db.refresh(config)
    return {
        "library_id": config.library_id,
        "library_type": config.library_type,
        "api_key_masked": config.api_key[:4] + "****" if config.api_key else "",
    }


@router.delete("/{project_id}/zotero-config")
def delete_zotero_config(
    project_id: str,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)

    config = db.query(ZoteroConfig).filter(ZoteroConfig.project_id == project_id).first()
    if config:
        db.delete(config)
        db.commit()
    return {"status": "deleted"}


# ─── Sync routes ──────────────────────────────────────────────────────────────

@router.post("/{project_id}/sync")
async def sync_zotero(
    project_id: str,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)
    return await sync_zotero_for_project(project_id, db, user_id=current_user.id)


@router.get("/{project_id}/sync-status")
def get_sync_status(
    project_id: str,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)
    config = db.query(ZoteroConfig).filter(ZoteroConfig.project_id == project_id).first()
    if not config:
        raise HTTPException(status_code=404, detail="Zotero config not found")
    return {
        "last_sync_at": config.last_sync_at.isoformat() if config.last_sync_at else None,
        "last_sync_status": config.last_sync_status,
        "last_sync_error": config.last_sync_error,
        "last_sync_version": config.last_sync_version,
    }


# ─── Export route ─────────────────────────────────────────────────────────────

@router.post("/{project_id}/export")
def export_bibtex(
    project_id: str,
    body: ExportRequest,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)

    query = db.query(Citation).filter(Citation.project_id == project_id)
    if body.ids:
        query = query.filter(Citation.id.in_(body.ids))

    citations = query.all()

    entries = [
        {
            "bibtex_type": c.bibtex_type,
            "bibtex_key": c.bibtex_key,
            "title": c.title,
            "authors": c.authors,
            "journal": c.journal,
            "year": c.year,
            "volume": c.volume,
            "issue": c.issue,
            "pages": c.pages,
            "doi": c.doi,
            "url": c.url,
        }
        for c in citations
    ]
    content = generate_bibtex_batch(entries)
    from fastapi.responses import PlainTextResponse
    return PlainTextResponse(content, media_type="text/plain; charset=utf-8")


# ─── Citation CRUD routes ─────────────────────────────────────────────────────

@router.get("/{project_id}")
def list_citations(
    project_id: str,
    q: str = "",
    year_from: int | None = None,
    year_to: int | None = None,
    source: str | None = None,
    sort_by: str = "created_at",
    sort_order: str = "desc",
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)

    query = db.query(Citation).filter(Citation.project_id == project_id)

    if q:
        search = f"%{q}%"
        query = query.filter(
            (Citation.title.ilike(search))
            | (Citation.authors.ilike(search))
            | (Citation.journal.ilike(search))
        )

    if year_from is not None:
        query = query.filter(Citation.year >= year_from)
    if year_to is not None:
        query = query.filter(Citation.year <= year_to)
    if source:
        query = query.filter(Citation.source == source)

    allowed_sort = {"created_at", "title", "year", "journal", "authors"}
    sort_column = getattr(Citation, sort_by, Citation.created_at) if sort_by in allowed_sort else Citation.created_at
    if sort_order == "desc":
        query = query.order_by(sort_column.desc())
    else:
        query = query.order_by(sort_column.asc())

    citations = query.all()
    return {
        "items": [
            {
                "id": c.id,
                "title": c.title,
                "authors": c.authors,
                "journal": c.journal,
                "year": c.year,
                "volume": c.volume,
                "issue": c.issue,
                "pages": c.pages,
                "doi": c.doi,
                "url": c.url,
                "abstract": c.abstract,
                "bibtex_key": c.bibtex_key,
                "bibtex_type": c.bibtex_type,
                "source": c.source,
                "zotero_item_key": c.zotero_item_key,
                "user_id": c.user_id,
                "created_at": c.created_at.isoformat() if c.created_at else None,
                "updated_at": c.updated_at.isoformat() if c.updated_at else None,
            }
            for c in citations
        ],
        "total": len(citations),
    }


@router.post("/{project_id}")
def create_citation(
    project_id: str,
    title: str = Form(...),
    authors: str = Form(""),
    journal: str = Form(""),
    year: int | None = Form(None),
    volume: str = Form(""),
    issue: str = Form(""),
    pages: str = Form(""),
    doi: str = Form(""),
    url: str = Form(""),
    abstract: str = Form(""),
    bibtex_key: str = Form(""),
    bibtex_type: str = Form("article"),
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)

    if not title or not title.strip():
        raise HTTPException(status_code=400, detail="Title is required")

    citation = Citation(
        project_id=project_id,
        user_id=current_user.id,
        title=title,
        authors=authors,
        journal=journal,
        year=year,
        volume=volume,
        issue=issue,
        pages=pages,
        doi=doi,
        url=url,
        abstract=abstract,
        bibtex_key=bibtex_key or None,
        bibtex_type=bibtex_type,
        source="manual",
    )
    db.add(citation)
    db.commit()
    db.refresh(citation)
    return {
        "id": citation.id,
        "title": citation.title,
        "authors": citation.authors,
        "journal": citation.journal,
        "year": citation.year,
        "source": citation.source,
        "user_id": citation.user_id,
        "created_at": citation.created_at.isoformat() if citation.created_at else None,
    }


@router.put("/{project_id}/{citation_id}")
def update_citation(
    project_id: str,
    citation_id: str,
    title: str | None = Form(None),
    authors: str | None = Form(None),
    journal: str | None = Form(None),
    year: int | None = Form(None),
    volume: str | None = Form(None),
    issue: str | None = Form(None),
    pages: str | None = Form(None),
    doi: str | None = Form(None),
    url: str | None = Form(None),
    abstract: str | None = Form(None),
    bibtex_key: str | None = Form(None),
    bibtex_type: str | None = Form(None),
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)

    citation = db.query(Citation).filter(
        Citation.id == citation_id,
        Citation.project_id == project_id,
    ).first()
    if not citation:
        raise HTTPException(status_code=404, detail="Citation not found")

    fields = [
        "title", "authors", "journal", "year", "volume",
        "issue", "pages", "doi", "url", "abstract", "bibtex_key", "bibtex_type",
    ]
    for field in fields:
        value = locals().get(field)
        if value is not None:
            if field == "title" and value == "":
                raise HTTPException(status_code=400, detail="Title cannot be empty")
            setattr(citation, field, value)

    citation.updated_at = datetime.utcnow()
    db.commit()
    db.refresh(citation)
    return {
        "id": citation.id,
        "title": citation.title,
        "authors": citation.authors,
        "journal": citation.journal,
        "year": citation.year,
        "bibtex_key": citation.bibtex_key,
        "bibtex_type": citation.bibtex_type,
    }


@router.delete("/{project_id}/{citation_id}")
def delete_citation(
    project_id: str,
    citation_id: str,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    _check_project_access(project_id, current_user, db)

    citation = db.query(Citation).filter(
        Citation.id == citation_id,
        Citation.project_id == project_id,
    ).first()
    if not citation:
        raise HTTPException(status_code=404, detail="Citation not found")
    if citation.user_id != current_user.id:
        raise HTTPException(status_code=403, detail="Can only delete your own citations")

    db.delete(citation)
    db.commit()
    return {"status": "deleted"}
