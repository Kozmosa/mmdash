from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy.orm import Session

from app.database import get_db
from app.models import Project, Team, TeamMember, ProviderBinding
from app.schemas.project import ProjectCreate, ProjectResponse
from app.api.auth import get_current_user
from app.models import User

router = APIRouter()

DOCUMENT_PROVIDER_TYPES = ("notion", "local_file", "documosa")


def _get_team_document_binding(db: Session, team_id: str) -> ProviderBinding | None:
    return (
        db.query(ProviderBinding)
        .filter(
            ProviderBinding.team_id == team_id,
            ProviderBinding.provider_type.in_(DOCUMENT_PROVIDER_TYPES),
        )
        .order_by(ProviderBinding.created_at.desc())
        .first()
    )


@router.post("", response_model=ProjectResponse)
def create_project(data: ProjectCreate, team_id: str, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    team = db.query(Team).filter(Team.id == team_id).first()
    if not team:
        raise HTTPException(status_code=404, detail="Team not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    binding = _get_team_document_binding(db, team_id)
    if not binding:
        raise HTTPException(status_code=400, detail="Please configure a team document provider first")
    project = Project(
        team_id=team_id,
        name=data.name,
        description=data.description,
        git_remote_url=data.git_remote_url,
    )
    db.add(project)
    db.commit()
    db.refresh(project)
    return project


@router.get("", response_model=list[ProjectResponse])
def list_projects(team_id: str, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    member = db.query(TeamMember).filter(TeamMember.team_id == team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    projects = db.query(Project).filter(Project.team_id == team_id).all()
    return projects


@router.get("/{project_id}", response_model=ProjectResponse)
def get_project(project_id: str, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    return project


@router.put("/{project_id}")
def update_project(project_id: str, data: ProjectCreate, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    project.name = data.name
    project.description = data.description
    project.git_remote_url = data.git_remote_url
    db.commit()
    db.refresh(project)
    return project
