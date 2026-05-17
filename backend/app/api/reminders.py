from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel
from sqlalchemy.orm import Session

from app.database import get_db
from app.models import Project, TeamMember, TimelineEvent, Todo
from app.api.auth import get_current_user
from app.models import User

router = APIRouter()


class AckRequest(BaseModel):
    type: str  # "event" or "todo"
    ids: list[str]


@router.get("/{project_id}/pending")
def get_pending_reminders(
    project_id: str,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = (
        db.query(TeamMember)
        .filter(
            TeamMember.team_id == project.team_id,
            TeamMember.user_id == current_user.id,
        )
        .first()
    )
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")

    events = (
        db.query(TimelineEvent)
        .filter(
            TimelineEvent.project_id == project_id,
            TimelineEvent.reminder_detected == True,
            TimelineEvent.reminder_acked == False,
        )
        .all()
    )
    todos = (
        db.query(Todo)
        .filter(
            Todo.project_id == project_id,
            Todo.reminder_detected == True,
            Todo.reminder_acked == False,
        )
        .all()
    )

    return {
        "events": [
            {
                "id": e.id,
                "title": e.title,
                "description": e.description,
                "start_time": e.start_time.isoformat() if e.start_time else None,
            }
            for e in events
        ],
        "todos": [
            {
                "id": t.id,
                "content": t.content,
                "due_date": t.due_date.isoformat() if t.due_date else None,
            }
            for t in todos
        ],
    }


@router.post("/{project_id}/ack")
def ack_reminders(
    project_id: str,
    body: AckRequest,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = (
        db.query(TeamMember)
        .filter(
            TeamMember.team_id == project.team_id,
            TeamMember.user_id == current_user.id,
        )
        .first()
    )
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")

    if body.type == "event":
        db.query(TimelineEvent).filter(
            TimelineEvent.id.in_(body.ids),
            TimelineEvent.project_id == project_id,
        ).update({"reminder_acked": True}, synchronize_session=False)
    elif body.type == "todo":
        db.query(Todo).filter(
            Todo.id.in_(body.ids),
            Todo.project_id == project_id,
        ).update({"reminder_acked": True}, synchronize_session=False)
    else:
        raise HTTPException(status_code=400, detail="Invalid type, must be 'event' or 'todo'")

    db.commit()
    return {"status": "acked", "type": body.type, "count": len(body.ids)}
