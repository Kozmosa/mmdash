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
    parent_page_id: str | None = None


class UpdateContentRequest(BaseModel):
    title: str | None = None
    markdown: str | None = None
    blocks: list[dict] | None = None


def _get_binding(db: Session, user_id: str, team_id: str | None = None) -> ProviderBinding:
    if team_id:
        binding = (
            db.query(ProviderBinding)
            .filter(
                ProviderBinding.team_id == team_id,
                ProviderBinding.provider_type.in_(DOCUMENT_PROVIDER_TYPES),
            )
            .order_by(ProviderBinding.created_at.desc())
            .first()
        )
        if binding:
            return binding
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

    binding = _get_binding(db, current_user.id, project.team_id)
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

    # Merge unsaved draft if newer than provider content
    from app.services.cache import get_draft_markdown
    draft = get_draft_markdown(project_id)
    if draft and draft != markdown:
        markdown = draft

    return {
        "page_id": result["page_id"],
        "markdown": markdown,
        "blocks": blocks,
        "from_cache": result.get("from_cache", False),
        "has_draft": bool(draft and draft != _content_to_markdown(content)),
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


@router.put("/{project_id}/cache")
async def cache_model_draft(
    project_id: str,
    body: UpdateContentRequest,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    """Auto-save markdown draft to server-side cache (does not write to document provider)."""
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")

    from app.services.cache import set_draft_markdown
    set_draft_markdown(project_id, body.markdown or "")
    return {"status": "cached"}


@router.post("/{project_id}/content")
async def update_model_content(
    project_id: str,
    request: Request,
    body: UpdateContentRequest,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    """Explicit save: reads markdown (from body or draft cache) and writes to document provider."""
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    if not project.model_data_page_id:
        raise HTTPException(status_code=400, detail="No model page linked to this project")

    # Use provided markdown or fall back to draft cache
    markdown = body.markdown
    if not markdown:
        from app.services.cache import get_draft_markdown
        markdown = get_draft_markdown(project_id) or ""

    binding = _get_binding(db, current_user.id, project.team_id)
    provider = get_provider(binding.provider_type)
    credentials = json.loads(binding.credentials)
    token = _extract_bearer_token(request)
    if token:
        credentials["_token"] = token

    content = {"markdown": markdown}
    if body.title:
        content["title"] = body.title
    if body.blocks:
        content["blocks"] = body.blocks

    try:
        result = await provider.update_page_content(project.model_data_page_id, content, credentials)
    except NotImplementedError:
        raise HTTPException(status_code=400, detail="Current document provider does not support content updates")
    except HTTPException:
        raise
    except Exception as e:
        import logging
        logging.getLogger("mmdash.model").exception("Failed to update content via %s", binding.provider_type)
        raise HTTPException(status_code=500, detail=f"Failed to update content: {str(e)}")

    # Invalidate caches
    from app.services.cache import invalidate_page, delete_draft_markdown, invalidate_llm_cache
    invalidate_page(binding.provider_type, project.model_data_page_id)
    delete_draft_markdown(project_id)
    invalidate_llm_cache(project_id)

    blocks = result.get("blocks", [])
    result_md = _content_to_markdown(result)
    return {
        "page_id": result["page_id"],
        "title": result.get("title", ""),
        "markdown": result_md,
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

    binding = _get_binding(db, current_user.id, project.team_id)
    provider = get_provider(binding.provider_type)
    credentials = json.loads(binding.credentials)
    token = _extract_bearer_token(request)
    if token:
        credentials["_token"] = token

    parent_page_id = body.parent_page_id or project.base_data_page_id
    try:
        result = await provider.create_page(body.title, "", credentials, parent_page_id)
    except NotImplementedError:
        raise HTTPException(status_code=400, detail="Current document provider does not support creating pages")
    except ValueError as e:
        raise HTTPException(status_code=400, detail=str(e))
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
async def get_symbols(project_id: str, request: Request, refresh: bool = False, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    result = await get_model_content(project_id, request, current_user, db)
    markdown = result.get("markdown", "")
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    from app.services.cache import get_cached_llm_result, set_cached_llm_result
    if not refresh:
        cached = get_cached_llm_result(project_id, "symbols", markdown)
        if cached is not None:
            return {"symbols": json.loads(cached), "disclaimer": "仅供参考", "cached": True}
    symbols = await analyze_symbols_with_configured_model(markdown, project, current_user, db)
    set_cached_llm_result(project_id, "symbols", markdown, json.dumps(symbols))
    return {"symbols": symbols, "disclaimer": "仅供参考", "cached": False}


@router.get("/{project_id}/analyze/structure")
async def get_structure(project_id: str, request: Request, refresh: bool = False, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    result = await get_model_content(project_id, request, current_user, db)
    markdown = result.get("markdown", "")
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    from app.services.cache import get_cached_llm_result, set_cached_llm_result
    if not refresh:
        cached = get_cached_llm_result(project_id, "structure", markdown)
        if cached is not None:
            return {"structure": json.loads(cached), "disclaimer": "仅供参考", "cached": True}
    structure = await analyze_structure_with_configured_model(markdown, project, current_user, db)
    set_cached_llm_result(project_id, "structure", markdown, json.dumps(structure))
    return {"structure": structure, "disclaimer": "仅供参考", "cached": False}


@router.post("/{project_id}/analyze/formula")
async def explain_formula_endpoint(project_id: str, formula: str, request: Request, refresh: bool = False, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    result = await get_model_content(project_id, request, current_user, db)
    markdown = result.get("markdown", "")
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    from app.services.cache import get_cached_llm_result, set_cached_llm_result
    cache_content = f"{formula}\n{markdown[:2000]}"
    if not refresh:
        cached = get_cached_llm_result(project_id, "formula", cache_content)
        if cached is not None:
            return {"explanation": cached, "disclaimer": "仅供参考", "cached": True}
    explanation = await explain_formula_with_configured_model(formula, markdown[:2000], project, current_user, db)
    set_cached_llm_result(project_id, "formula", cache_content, explanation)
    return {"explanation": explanation, "disclaimer": "仅供参考", "cached": False}


@router.get("/{project_id}/analyze/errors")
async def get_errors(project_id: str, request: Request, refresh: bool = False, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    result = await get_model_content(project_id, request, current_user, db)
    markdown = result.get("markdown", "")
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    from app.services.cache import get_cached_llm_result, set_cached_llm_result
    if not refresh:
        cached = get_cached_llm_result(project_id, "errors", markdown)
        if cached is not None:
            return {"errors": json.loads(cached), "disclaimer": "仅供参考", "cached": True}
    errors = await find_errors_with_configured_model(markdown, project, current_user, db)
    set_cached_llm_result(project_id, "errors", markdown, json.dumps(errors))
    return {"errors": errors, "disclaimer": "仅供参考", "cached": False}
