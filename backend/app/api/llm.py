from typing import Optional
import json

from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel
from sqlalchemy.orm import Session

from app.api.auth import get_current_user
from app.database import get_db
from app.models import ProviderBinding, Team, TeamMember
from app.services.llm.prompts import get_team_llm_prompts, normalize_llm_prompts, serialize_llm_prompts
from app.services.llm.factory import get_provider_for_binding

router = APIRouter()


class BindingCreate(BaseModel):
    provider_type: str
    credentials: dict
    team_id: Optional[str] = None


class SelectionRequest(BaseModel):
    binding_id: str
    selected_model: str


class PromptSettingsRequest(BaseModel):
    team_id: str
    prompts: dict


def _ensure_team_member(team_id: str, user_id: str, db: Session) -> Team:
    team = db.query(Team).filter(Team.id == team_id).first()
    if not team:
        raise HTTPException(status_code=404, detail="team not found")
    member = db.query(TeamMember).filter(
        TeamMember.team_id == team_id,
        TeamMember.user_id == user_id,
    ).first()
    if not member:
        raise HTTPException(status_code=403, detail="not a team member")
    return team


def _get_team_member(team_id: str, user_id: str, db: Session) -> TeamMember | None:
    return db.query(TeamMember).filter(
        TeamMember.team_id == team_id,
        TeamMember.user_id == user_id,
    ).first()


def _is_manager(member: TeamMember | None) -> bool:
    return bool(member and member.role in {"owner", "admin"})


@router.get("/providers")
def list_providers():
    return {"providers": ["openai", "deepseek"]}


@router.get("/models")
async def list_models(
    binding_id: Optional[str] = None,
    provider: Optional[str] = None,
    team_id: Optional[str] = None,
    current_user=Depends(get_current_user),
    db: Session = Depends(get_db),
):
    binding = None
    if binding_id:
        binding = db.query(ProviderBinding).filter(ProviderBinding.id == binding_id).first()
        if not binding:
            raise HTTPException(status_code=404, detail="binding not found")
        if binding.team_id:
            _ensure_team_member(binding.team_id, current_user.id, db)
        elif binding.user_id != current_user.id:
            raise HTTPException(status_code=403, detail="forbidden")
    elif team_id:
        _ensure_team_member(team_id, current_user.id, db)
        binding = db.query(ProviderBinding).filter(
            ProviderBinding.team_id == team_id,
            ProviderBinding.provider_type == (provider or "openai"),
        ).order_by(ProviderBinding.created_at.desc()).first()
    # If provider query param provided but no binding, we will create a provider from env
    prov = get_provider_for_binding(binding)
    models = await prov.list_models()
    return {"models": models}


@router.get("/binding/current")
def get_current_binding(
    provider_type: Optional[str] = None,
    team_id: Optional[str] = None,
    current_user=Depends(get_current_user),
    db: Session = Depends(get_db),
):
    binding = None
    if team_id:
        _ensure_team_member(team_id, current_user.id, db)
        query = db.query(ProviderBinding).filter(ProviderBinding.team_id == team_id)
        if provider_type:
            query = query.filter(ProviderBinding.provider_type == provider_type)
        binding = query.order_by(ProviderBinding.created_at.desc()).first()
    else:
        query = db.query(ProviderBinding).filter(
            ProviderBinding.user_id == current_user.id,
            ProviderBinding.team_id.is_(None),
        )
        if provider_type:
            query = query.filter(ProviderBinding.provider_type == provider_type)
        binding = query.order_by(ProviderBinding.created_at.desc()).first()

    if not binding:
        return {"binding": None}

    creds = {}
    try:
        creds = json.loads(binding.credentials or "{}")
    except Exception:
        creds = {}

    return {
        "binding": {
            "id": binding.id,
            "provider_type": binding.provider_type,
            "team_id": binding.team_id,
            "selected_model": creds.get("selected_model"),
            "has_api_key": bool(creds.get("api_key")),
        }
    }


@router.post("/bindings")
def create_binding(data: BindingCreate, current_user=Depends(get_current_user), db: Session = Depends(get_db)):
    # If team_id provided, ensure current_user is team owner
    if data.team_id:
        team = _ensure_team_member(data.team_id, current_user.id, db)
        member = _get_team_member(data.team_id, current_user.id, db)
        if not _is_manager(member):
            raise HTTPException(status_code=403, detail="only team owner or admin can create team binding")
    # remove existing binding for same user & team/provider
    old = db.query(ProviderBinding).filter(
        ProviderBinding.user_id == current_user.id,
        ProviderBinding.team_id == (data.team_id or None),
        ProviderBinding.provider_type == data.provider_type,
    ).first()
    if old:
        db.delete(old)
    binding = ProviderBinding(
        user_id=current_user.id,
        team_id=data.team_id,
        provider_type=data.provider_type,
        credentials=json.dumps(data.credentials),
    )
    db.add(binding)
    db.commit()
    db.refresh(binding)
    return {"status": "created", "binding_id": binding.id}


@router.post("/selection")
def select_model(data: SelectionRequest, current_user=Depends(get_current_user), db: Session = Depends(get_db)):
    binding = db.query(ProviderBinding).filter(ProviderBinding.id == data.binding_id).first()
    if not binding:
        raise HTTPException(status_code=404, detail="binding not found")
    # If team binding, only owner can change
    if binding.team_id:
        member = _get_team_member(binding.team_id, current_user.id, db)
        if not _is_manager(member):
            raise HTTPException(status_code=403, detail="only team owner or admin can change selection")
    # update credentials JSON with selected_model
    creds = {}
    try:
        creds = json.loads(binding.credentials or "{}")
    except Exception:
        creds = {}
    creds["selected_model"] = data.selected_model
    binding.credentials = json.dumps(creds)
    db.commit()
    return {"status": "ok", "selected_model": data.selected_model}


@router.get("/prompts")
def get_prompt_settings(team_id: str, current_user=Depends(get_current_user), db: Session = Depends(get_db)):
    member = _get_team_member(team_id, current_user.id, db)
    if not _is_manager(member):
        raise HTTPException(status_code=403, detail="only team owner or admin can view prompts")
    return {
        "team_id": team_id,
        "prompts": get_team_llm_prompts(db, team_id),
    }


@router.put("/prompts")
def update_prompt_settings(data: PromptSettingsRequest, current_user=Depends(get_current_user), db: Session = Depends(get_db)):
    member = _get_team_member(data.team_id, current_user.id, db)
    if not _is_manager(member):
        raise HTTPException(status_code=403, detail="only team owner or admin can edit prompts")

    team = db.query(Team).filter(Team.id == data.team_id).first()
    if not team:
        raise HTTPException(status_code=404, detail="team not found")

    prompts = normalize_llm_prompts(data.prompts)
    team.llm_prompts = serialize_llm_prompts(prompts)
    db.commit()
    return {"status": "ok", "team_id": data.team_id, "prompts": prompts}
