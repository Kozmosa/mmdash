import difflib
import json
from fastapi import APIRouter, Depends, HTTPException, Request
from sqlalchemy.orm import Session

from app.database import get_db
from app.models import Project, TeamMember, ProviderBinding, ModelSnapshot, User
from app.api.auth import get_current_user
from app.services.document_provider import DocumentProvider
from app.services.document_provider import get_provider
from app.services.cache import get_cached_page
from app.services.markdown_blocks import content_to_markdown

router = APIRouter()


def _build_diff_chunks(base_text: str, compare_text: str) -> list[dict]:
    base_lines = (base_text or "").splitlines()
    compare_lines = (compare_text or "").splitlines()
    matcher = difflib.SequenceMatcher(None, base_lines, compare_lines)
    chunks: list[dict] = []
    for tag, i1, i2, j1, j2 in matcher.get_opcodes():
        if tag == "equal":
            if i1 != i2:
                chunks.append({"type": "context", "lines": base_lines[i1:i2]})
        elif tag == "delete":
            chunks.append({"type": "delete", "lines": base_lines[i1:i2]})
        elif tag == "insert":
            chunks.append({"type": "insert", "lines": compare_lines[j1:j2]})
        elif tag == "replace":
            chunks.append({"type": "replace", "before": base_lines[i1:i2], "after": compare_lines[j1:j2]})
    return chunks


def _get_binding(db: Session, user_id: str) -> ProviderBinding:
    binding = db.query(ProviderBinding).filter(ProviderBinding.user_id == user_id).first()
    if not binding:
        raise HTTPException(status_code=400, detail="Please bind a document provider first")
    return binding


def _extract_bearer_token(request: Request) -> str:
    auth = request.headers.get("Authorization", "")
    if auth.lower().startswith("bearer "):
        return auth[7:]
    return ""


def _ensure_team_member(project: Project, current_user: User, db: Session):
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")


def _provider_supports_write(provider) -> bool:
    method = getattr(type(provider), "update_page_content", None)
    if method is None:
        return callable(getattr(provider, "update_page_content", None))
    return method is not DocumentProvider.update_page_content


async def _fetch_current_markdown(project: Project, binding: ProviderBinding, token: str = "", strict: bool = False) -> str:
    """Fetch current markdown from the document provider."""
    if not project.model_data_page_id:
        return ""
    provider = get_provider(binding.provider_type)
    credentials = json.loads(binding.credentials)
    if token:
        credentials["_token"] = token

    cached = get_cached_page(binding.provider_type, project.model_data_page_id)
    if cached:
        return content_to_markdown(cached)

    try:
        content = await provider.fetch_page_content(project.model_data_page_id, credentials)
        return content_to_markdown(content)
    except HTTPException:
        if strict:
            raise
        return ""
    except Exception as e:
        if strict:
            raise HTTPException(status_code=500, detail=f"Failed to fetch current model content before rollback: {str(e)}")
        return ""


@router.post("/{project_id}/commit")
async def commit_model(project_id: str, message: str, request: Request, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    _ensure_team_member(project, current_user, db)
    if not project.model_data_page_id:
        raise HTTPException(status_code=400, detail="No model page linked")

    binding = _get_binding(db, current_user.id)
    markdown = await _fetch_current_markdown(project, binding, _extract_bearer_token(request))

    snapshot = ModelSnapshot(
        project_id=project_id,
        user_id=current_user.id,
        commit_message=message,
        notion_page_id=project.model_data_page_id,
        snapshot_content=markdown,
    )
    db.add(snapshot)
    db.commit()
    db.refresh(snapshot)
    return {"id": snapshot.id, "message": message, "created_at": snapshot.created_at}


@router.get("/{project_id}/commits")
def list_commits(project_id: str, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    _ensure_team_member(project, current_user, db)

    commits = db.query(ModelSnapshot).filter(ModelSnapshot.project_id == project_id).order_by(ModelSnapshot.created_at.desc()).all()
    result = []
    for c in commits:
        user = db.query(User).filter(User.id == c.user_id).first()
        result.append({
            "id": c.id,
            "commit_message": c.commit_message,
            "user_email": user.email if user else "未知用户",
            "user_name": user.display_name if user else None,
            "created_at": c.created_at,
            "notion_page_id": c.notion_page_id,
        })
    return result


@router.get("/{project_id}/diff")
async def diff_commits(project_id: str, base_id: str, compare_id: str, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    _ensure_team_member(project, current_user, db)

    base = db.query(ModelSnapshot).filter(ModelSnapshot.id == base_id, ModelSnapshot.project_id == project_id).first()
    compare = db.query(ModelSnapshot).filter(ModelSnapshot.id == compare_id, ModelSnapshot.project_id == project_id).first()
    if not base or not compare:
        raise HTTPException(status_code=404, detail="Snapshot not found")

    base_user = db.query(User).filter(User.id == base.user_id).first()
    compare_user = db.query(User).filter(User.id == compare.user_id).first()

    diff = list(difflib.unified_diff(
        (base.snapshot_content or "").splitlines(keepends=True),
        (compare.snapshot_content or "").splitlines(keepends=True),
        fromfile=f"{base.commit_message} by {base_user.email if base_user else '未知'}",
        tofile=f"{compare.commit_message} by {compare_user.email if compare_user else '未知'}",
    ))
    diff_chunks = _build_diff_chunks(base.snapshot_content or "", compare.snapshot_content or "")

    return {
        "base": {"id": base.id, "message": base.commit_message, "author": base_user.email if base_user else "未知"},
        "compare": {"id": compare.id, "message": compare.commit_message, "author": compare_user.email if compare_user else "未知"},
        "diff": "".join(diff),
        "diff_chunks": diff_chunks,
    }


@router.get("/{project_id}/rollback-preview")
async def rollback_preview(project_id: str, snapshot_id: str, request: Request, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    _ensure_team_member(project, current_user, db)

    snapshot = db.query(ModelSnapshot).filter(ModelSnapshot.id == snapshot_id, ModelSnapshot.project_id == project_id).first()
    if not snapshot:
        raise HTTPException(status_code=404, detail="Snapshot not found")

    binding = db.query(ProviderBinding).filter(ProviderBinding.user_id == current_user.id).first()
    current_markdown = ""
    can_write = False
    provider_type = None
    if binding and project.model_data_page_id:
        provider_type = binding.provider_type
        provider = get_provider(binding.provider_type)
        can_write = _provider_supports_write(provider)
        current_markdown = await _fetch_current_markdown(project, binding, _extract_bearer_token(request))

    target_markdown = snapshot.snapshot_content or ""
    diff = list(difflib.unified_diff(
        current_markdown.splitlines(keepends=True),
        target_markdown.splitlines(keepends=True),
        fromfile="current",
        tofile=f"rollback:{snapshot.commit_message}",
    ))
    diff_chunks = _build_diff_chunks(current_markdown, target_markdown)
    snapshot_user = db.query(User).filter(User.id == snapshot.user_id).first()

    return {
        "snapshot": {
            "id": snapshot.id,
            "commit_message": snapshot.commit_message,
            "user_email": snapshot_user.email if snapshot_user else "未知用户",
            "created_at": snapshot.created_at,
        },
        "current": {
            "page_id": project.model_data_page_id,
            "markdown": current_markdown,
        },
        "target": {
            "page_id": snapshot.notion_page_id,
            "markdown": target_markdown,
        },
        "diff": "".join(diff),
        "diff_chunks": diff_chunks,
        "can_write": can_write,
        "provider_type": provider_type,
    }


@router.post("/{project_id}/rollback")
async def rollback_model(project_id: str, snapshot_id: str, request: Request, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    _ensure_team_member(project, current_user, db)

    snapshot = db.query(ModelSnapshot).filter(ModelSnapshot.id == snapshot_id, ModelSnapshot.project_id == project_id).first()
    if not snapshot:
        raise HTTPException(status_code=404, detail="Snapshot not found")
    if not project.model_data_page_id:
        raise HTTPException(status_code=400, detail="No model page linked")

    binding = _get_binding(db, current_user.id)
    provider = get_provider(binding.provider_type)
    if not _provider_supports_write(provider):
        raise HTTPException(status_code=400, detail="Current document provider does not support rollback writeback")

    token = _extract_bearer_token(request)
    credentials = json.loads(binding.credentials)
    if token:
        credentials["_token"] = token
    current_markdown = await _fetch_current_markdown(project, binding, token, strict=True)

    backup = ModelSnapshot(
        project_id=project_id,
        user_id=current_user.id,
        commit_message=f"Auto-backup before rollback to {snapshot.commit_message}",
        notion_page_id=project.model_data_page_id,
        snapshot_content=current_markdown,
    )
    db.add(backup)
    db.commit()
    db.refresh(backup)

    try:
        await provider.update_page_content(
            project.model_data_page_id,
            {"markdown": snapshot.snapshot_content or ""},
            credentials,
        )
    except NotImplementedError:
        raise HTTPException(status_code=400, detail="Current document provider does not support rollback writeback")
    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Failed to write rollback content: {str(e)}")

    from app.services.cache import invalidate_page
    invalidate_page(binding.provider_type, project.model_data_page_id)

    return {"status": "rollback_applied", "backup_id": backup.id, "snapshot_id": snapshot.id}
