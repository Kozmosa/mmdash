import os
import shutil
import uuid
from typing import List
from fastapi import APIRouter, Depends, HTTPException, UploadFile, File, Path
from fastapi.responses import FileResponse
from sqlalchemy.orm import Session

from app.database import get_db
from app.models import Project, TeamMember, Todo, ProblemFile
from app.api.auth import get_current_user
from app.models import User

router = APIRouter()

UPLOAD_DIR = "uploads"
os.makedirs(UPLOAD_DIR, exist_ok=True)


@router.post("/{project_id}/upload")
def upload_problem(
    project_id: str,
    files: List[UploadFile] = File(None),
    file: UploadFile = File(None),
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")

    # support both single-file (file) and multi-file (files) uploads
    incoming = files if files else ([file] if file else [])
    if not incoming:
        raise HTTPException(status_code=400, detail="No files provided")

    created = []
    for fobj in incoming:
        file_type = "pdf" if fobj.filename and fobj.filename.lower().endswith(".pdf") else "text"
        unique = uuid.uuid4().hex
        file_path = os.path.join(UPLOAD_DIR, f"{project_id}_{unique}_{fobj.filename or 'unknown'}")
        with open(file_path, "wb") as out_f:
            shutil.copyfileobj(fobj.file, out_f)

        extracted_text = None
        if file_type == "pdf":
            try:
                from PyPDF2 import PdfReader
                reader = PdfReader(file_path)
                extracted_text = "\n".join(page.extract_text() or "" for page in reader.pages)
            except Exception:
                extracted_text = None
        elif file_type == "text":
            try:
                with open(file_path, "r", encoding="utf-8") as t_f:
                    extracted_text = t_f.read()
            except Exception:
                extracted_text = None

        pf = ProblemFile(
            project_id=project_id,
            filename=fobj.filename or "unknown",
            file_path=file_path,
            file_type=file_type,
            extracted_text=extracted_text,
        )
        db.add(pf)
        db.commit()
        db.refresh(pf)
        created.append({"id": pf.id, "filename": pf.filename, "file_type": pf.file_type, "extracted_text": extracted_text})

    return created


@router.get("/{project_id}/problems")
def list_problems(project_id: str, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    files = db.query(ProblemFile).filter(ProblemFile.project_id == project_id).all()
    return [{"id": f.id, "filename": f.filename, "file_type": f.file_type, "uploaded_at": f.uploaded_at} for f in files]


@router.get("/{project_id}/problems/{problem_id}/download")
def download_problem(
    project_id: str,
    problem_id: str = Path(...),
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")

    pf = db.query(ProblemFile).filter(ProblemFile.id == problem_id, ProblemFile.project_id == project_id).first()
    if not pf:
        raise HTTPException(status_code=404, detail="Problem file not found")

    if not os.path.exists(pf.file_path):
        raise HTTPException(status_code=404, detail="File not found on disk")

    return FileResponse(pf.file_path, media_type="application/octet-stream", filename=pf.filename)


@router.delete("/{project_id}/problems/{problem_id}", status_code=204)
def delete_problem(
    project_id: str,
    problem_id: str,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")

    pf = db.query(ProblemFile).filter(ProblemFile.id == problem_id, ProblemFile.project_id == project_id).first()
    if not pf:
        raise HTTPException(status_code=404, detail="Problem file not found")

    if os.path.exists(pf.file_path):
        os.remove(pf.file_path)

    db.delete(pf)
    db.commit()
    return None


@router.post("/{project_id}/todos")
def create_todo(
    project_id: str,
    content: str,
    is_team_todo: bool = False,
    due_date: str = None,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    from datetime import datetime
    todo = Todo(
        project_id=project_id,
        user_id=current_user.id,
        content=content,
        is_team_todo=is_team_todo,
        due_date=datetime.fromisoformat(due_date) if due_date else None,
    )
    db.add(todo)
    db.commit()
    db.refresh(todo)
    return {"id": todo.id, "content": todo.content, "completed": todo.completed, "is_team_todo": todo.is_team_todo}


@router.get("/{project_id}/todos")
def list_todos(project_id: str, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    todos = db.query(Todo).filter(Todo.project_id == project_id).all()
    return [{"id": t.id, "content": t.content, "completed": t.completed, "is_team_todo": t.is_team_todo, "user_id": t.user_id, "due_date": t.due_date} for t in todos]


@router.put("/{project_id}/todos/{todo_id}")
def toggle_todo(project_id: str, todo_id: str, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    todo = db.query(Todo).filter(Todo.id == todo_id, Todo.project_id == project_id).first()
    if not todo:
        raise HTTPException(status_code=404, detail="Todo not found")
    todo.completed = not todo.completed
    db.commit()
    return {"id": todo.id, "completed": todo.completed}


@router.get("/{project_id}/progress")
def get_progress(project_id: str, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = db.query(TeamMember).filter(TeamMember.team_id == project.team_id, TeamMember.user_id == current_user.id).first()
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")
    todos = db.query(Todo).filter(Todo.project_id == project_id).all()
    total = len(todos)
    completed = sum(1 for t in todos if t.completed)
    team_total = sum(1 for t in todos if t.is_team_todo)
    team_completed = sum(1 for t in todos if t.is_team_todo and t.completed)
    personal_total = total - team_total
    personal_completed = completed - team_completed
    return {
        "total_todos": total,
        "completed_todos": completed,
        "completion_rate": round(completed / total * 100, 1) if total > 0 else 0,
        "team": {"total": team_total, "completed": team_completed, "rate": round(team_completed / team_total * 100, 1) if team_total > 0 else 0},
        "personal": {"total": personal_total, "completed": personal_completed, "rate": round(personal_completed / personal_total * 100, 1) if personal_total > 0 else 0},
    }
