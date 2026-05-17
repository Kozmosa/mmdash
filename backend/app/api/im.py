from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel
from sqlalchemy.orm import Session

from app.database import get_db
from app.models import Project, TeamMember, IMUserBinding, IMProjectBinding, User
from app.api.auth import get_current_user
from app.services.im_provider import list_im_providers, get_im_providers

router = APIRouter()


class UserBindingRequest(BaseModel):
    provider_type: str
    im_user_id: str
    enabled: bool = True


class ProjectBindingRequest(BaseModel):
    provider_type: str
    im_chat_id: str
    enabled: bool = True


class VerifyRequest(BaseModel):
    provider_type: str
    recipient_type: str
    recipient_id: str


@router.get("/status")
def get_status(current_user: User = Depends(get_current_user)):
    return {"providers": list_im_providers()}


@router.get("/user-binding")
def get_user_binding(current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    binding = db.query(IMUserBinding).filter(
        IMUserBinding.user_id == current_user.id,
    ).first()
    if not binding:
        return {"binding": None}
    return {
        "binding": {
            "id": binding.id,
            "provider_type": binding.provider_type,
            "im_user_id": binding.im_user_id,
            "enabled": binding.enabled,
        }
    }


@router.post("/user-binding")
def save_user_binding(
    body: UserBindingRequest,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    binding = db.query(IMUserBinding).filter(
        IMUserBinding.user_id == current_user.id,
        IMUserBinding.provider_type == body.provider_type,
    ).first()

    if binding:
        binding.im_user_id = body.im_user_id
        binding.enabled = body.enabled
    else:
        binding = IMUserBinding(
            user_id=current_user.id,
            provider_type=body.provider_type,
            im_user_id=body.im_user_id,
            enabled=body.enabled,
        )
        db.add(binding)

    db.commit()
    db.refresh(binding)
    return {
        "status": "saved",
        "binding": {
            "id": binding.id,
            "provider_type": binding.provider_type,
            "im_user_id": binding.im_user_id,
            "enabled": binding.enabled,
        },
    }


@router.get("/project-binding/{project_id}")
def get_project_binding(
    project_id: str,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(
        TeamMember.team_id == project.team_id,
        TeamMember.user_id == current_user.id,
    ).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")

    binding = db.query(IMProjectBinding).filter(
        IMProjectBinding.project_id == project_id,
    ).first()
    if not binding:
        return {"binding": None}
    return {
        "binding": {
            "id": binding.id,
            "provider_type": binding.provider_type,
            "im_chat_id": binding.im_chat_id,
            "enabled": binding.enabled,
        }
    }


@router.post("/project-binding/{project_id}")
def save_project_binding(
    project_id: str,
    body: ProjectBindingRequest,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(
        TeamMember.team_id == project.team_id,
        TeamMember.user_id == current_user.id,
    ).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")

    binding = db.query(IMProjectBinding).filter(
        IMProjectBinding.project_id == project_id,
        IMProjectBinding.provider_type == body.provider_type,
    ).first()

    if binding:
        binding.im_chat_id = body.im_chat_id
        binding.enabled = body.enabled
    else:
        binding = IMProjectBinding(
            project_id=project_id,
            provider_type=body.provider_type,
            im_chat_id=body.im_chat_id,
            enabled=body.enabled,
        )
        db.add(binding)

    db.commit()
    db.refresh(binding)
    return {
        "status": "saved",
        "binding": {
            "id": binding.id,
            "provider_type": binding.provider_type,
            "im_chat_id": binding.im_chat_id,
            "enabled": binding.enabled,
        },
    }


@router.post("/verify")
async def verify(
    body: VerifyRequest,
    current_user: User = Depends(get_current_user),
):
    providers = get_im_providers()
    for p in providers:
        if p.get_provider_type() == body.provider_type:
            success = await p.send_message(
                body.recipient_type,
                body.recipient_id,
                "mmdash IM 通知验证",
                "如果您收到这条消息，说明飞书 IM 通知配置成功！",
            )
            return {"success": success}
    return {"success": False, "error": f"Provider '{body.provider_type}' not configured"}
