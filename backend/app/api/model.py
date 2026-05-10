import json
from pydantic import BaseModel

from fastapi import APIRouter, Depends, HTTPException, Request, Response
from sqlalchemy.orm import Session

from app.database import get_db
from app.models import Project, TeamMember, ProviderBinding
from app.api.auth import get_current_user
from app.models import User
from app.services.document_provider import get_provider
from app.services.cache import get_cached_page, set_cached_page
from app.services.markdown_blocks import content_to_markdown as _content_to_markdown
from app.services.model_analysis import (
    analyze_structure_with_configured_model,
    analyze_symbols_with_configured_model,
    explain_formula_with_configured_model,
    find_errors_with_configured_model,
)

router = APIRouter()
DOCUMENT_PROVIDER_TYPES = ("notion", "local_file")


class CreatePageRequest(BaseModel):
    title: str


class UpdateContentRequest(BaseModel):
    title: str | None = None
    markdown: str | None = None
    blocks: list[dict] | None = None


def _get_binding(db: Session, user_id: str) -> ProviderBinding:
    binding = (
        db.query(ProviderBinding)
        .filter(
            ProviderBinding.user_id == user_id,
            ProviderBinding.provider_type.in_(DOCUMENT_PROVIDER_TYPES),
        )
        .order_by(ProviderBinding.created_at.desc())
        .first()
    )
    if not binding:
        raise HTTPException(status_code=400, detail="Please bind a document provider first")
    return binding


def _extract_bearer_token(request: Request) -> str:
    auth = request.headers.get("Authorization", "")
    if auth.lower().startswith("bearer "):
        return auth[7:]
    return ""


async def _fetch_model_content(project_id: str, current_user: User, db: Session, token: str = "") -> dict:
    """Fetch model content from the configured document provider."""
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    if not project.model_data_page_id:
        raise HTTPException(status_code=400, detail="No model page linked to this project")

    binding = _get_binding(db, current_user.id)
    provider = get_provider(binding.provider_type)
    credentials = json.loads(binding.credentials)
    if token:
        credentials["_token"] = token

    # Try cache first
    cached = get_cached_page(binding.provider_type, project.model_data_page_id)
    if cached:
        return {"page_id": project.model_data_page_id, "content": cached, "from_cache": True}

    try:
        content = await provider.fetch_page_content(project.model_data_page_id, credentials)
        set_cached_page(binding.provider_type, project.model_data_page_id, content)
        return {"page_id": project.model_data_page_id, "content": content}
    except Exception as e:
        # Fallback to cache if available
        cached = get_cached_page(binding.provider_type, project.model_data_page_id)
        if cached:
            return {"page_id": project.model_data_page_id, "content": cached, "from_cache": True}
        raise HTTPException(status_code=500, detail=f"Failed to fetch content: {str(e)}")


@router.get("/{project_id}/content")
async def get_model_content(project_id: str, request: Request, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    token = _extract_bearer_token(request)
    result = await _fetch_model_content(project_id, current_user, db, token)
    content = result["content"]
    blocks = content.get("blocks", [])
    markdown = _content_to_markdown(content)
    return {
        "page_id": result["page_id"],
        "markdown": markdown,
        "blocks": blocks,
        "from_cache": result.get("from_cache", False),
    }


@router.get("/{project_id}/export/md")
async def export_markdown(project_id: str, request: Request, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    result = await get_model_content(project_id, request, current_user, db)
    md_content = result.get("markdown", "")
    return Response(content=md_content, media_type="text/markdown", headers={"Content-Disposition": f"attachment; filename=model_{project_id}.md"})


@router.post("/{project_id}/link")
def link_model_page(project_id: str, page_id: str, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    project.model_data_page_id = page_id
    db.commit()
    return {"status": "linked", "model_data_page_id": page_id}


@router.post("/{project_id}/content")
async def update_model_content(
    project_id: str,
    request: Request,
    body: UpdateContentRequest,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    if not project.model_data_page_id:
        raise HTTPException(status_code=400, detail="No model page linked to this project")

    binding = _get_binding(db, current_user.id)
    provider = get_provider(binding.provider_type)
    credentials = json.loads(binding.credentials)
    token = _extract_bearer_token(request)
    if token:
        credentials["_token"] = token

    content = body.model_dump(exclude_none=True)
    try:
        result = await provider.update_page_content(project.model_data_page_id, content, credentials)
    except NotImplementedError:
        raise HTTPException(status_code=400, detail="Current document provider does not support content updates")
    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to update content: {str(e)}")

    # Invalidate cache
    from app.services.cache import invalidate_page
    invalidate_page(binding.provider_type, project.model_data_page_id)

    blocks = result.get("blocks", [])
    markdown = _content_to_markdown(result)
    return {
        "page_id": result["page_id"],
        "title": result.get("title", ""),
        "markdown": markdown,
        "blocks": blocks,
    }


@router.post("/{project_id}/create-page")
async def create_and_bind_model_page(
    project_id: str,
    request: Request,
    body: CreatePageRequest,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")

    binding = _get_binding(db, current_user.id)
    provider = get_provider(binding.provider_type)
    credentials = json.loads(binding.credentials)
    token = _extract_bearer_token(request)
    if token:
        credentials["_token"] = token

    try:
        result = await provider.create_page(body.title, "", credentials)
    except NotImplementedError:
        raise HTTPException(status_code=400, detail="Current document provider does not support creating pages")
    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to create document: {str(e)}")

    project.model_data_page_id = result["page_id"]
    db.commit()

    return {
        "status": "created",
        "page_id": result["page_id"],
        "title": result.get("title", body.title),
    }


@router.get("/{project_id}/analyze/symbols")
async def get_symbols(project_id: str, request: Request, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    result = await get_model_content(project_id, request, current_user, db)
    markdown = result.get("markdown", "")
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    symbols = await analyze_symbols_with_configured_model(markdown, project, current_user, db)
    return {"symbols": symbols, "disclaimer": "仅供参考"}


@router.get("/{project_id}/analyze/structure")
async def get_structure(project_id: str, request: Request, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    result = await get_model_content(project_id, request, current_user, db)
    markdown = result.get("markdown", "")
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    structure = await analyze_structure_with_configured_model(markdown, project, current_user, db)
    return {"structure": structure, "disclaimer": "仅供参考"}


@router.post("/{project_id}/analyze/formula")
async def explain_formula_endpoint(project_id: str, formula: str, request: Request, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    result = await get_model_content(project_id, request, current_user, db)
    markdown = result.get("markdown", "")
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    explanation = await explain_formula_with_configured_model(formula, markdown[:2000], project, current_user, db)
    return {"explanation": explanation, "disclaimer": "仅供参考"}


@router.get("/{project_id}/analyze/errors")
async def get_errors(project_id: str, request: Request, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    result = await get_model_content(project_id, request, current_user, db)
    markdown = result.get("markdown", "")
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    errors = await find_errors_with_configured_model(markdown, project, current_user, db)
    return {"errors": errors, "disclaimer": "仅供参考"}
