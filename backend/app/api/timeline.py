from datetime import datetime
from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy.orm import Session

from app.database import get_db
from app.models import Project, TeamMember, TimelineEvent
from app.api.auth import get_current_user
from app.models import User

router = APIRouter()


@router.post("/{project_id}/events")
def create_event(
    project_id: str,
    title: str,
    description: str = "",
    start_time: str = "",
    end_time: str = "",
    is_team_event: bool = False,
    reminder_enabled: bool = False,
    reminder_minutes_before: int = None,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    event = TimelineEvent(
        project_id=project_id,
        user_id=current_user.id,
        title=title,
        description=description,
        start_time=datetime.fromisoformat(start_time) if start_time else datetime.utcnow(),
        end_time=datetime.fromisoformat(end_time) if end_time else None,
        is_team_event=is_team_event,
        reminder_enabled=reminder_enabled,
        reminder_minutes_before=reminder_minutes_before,
    )
    db.add(event)
    db.commit()
    db.refresh(event)
    return {
        "id": event.id,
        "title": event.title,
        "description": event.description,
        "start_time": event.start_time,
        "end_time": event.end_time,
        "is_team_event": event.is_team_event,
        "reminder_enabled": event.reminder_enabled,
        "reminder_minutes_before": event.reminder_minutes_before,
    }


@router.get("/{project_id}/events")
def list_events(project_id: str, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    events = db.query(TimelineEvent).filter(TimelineEvent.project_id == project_id).order_by(TimelineEvent.start_time).all()
    return [{
        "id": e.id,
        "title": e.title,
        "description": e.description,
        "start_time": e.start_time.isoformat() if e.start_time else None,
        "end_time": e.end_time.isoformat() if e.end_time else None,
        "is_team_event": e.is_team_event,
        "user_id": e.user_id,
        "reminder_enabled": e.reminder_enabled,
        "reminder_minutes_before": e.reminder_minutes_before,
    } for e in events]


@router.delete("/{project_id}/events/{event_id}")
def delete_event(project_id: str, event_id: str, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    event = db.query(TimelineEvent).filter(TimelineEvent.id == event_id, TimelineEvent.project_id == project_id).first()
    if not event:
        raise HTTPException(status_code=404, detail="Event not found")
    if event.user_id != current_user.id:
        raise HTTPException(status_code=403, detail="Can only delete your own events")
    db.delete(event)
    db.commit()
    return {"status": "deleted"}
