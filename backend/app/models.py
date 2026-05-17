import uuid
from datetime import datetime
from sqlalchemy import Column, String, DateTime, Text, ForeignKey, Boolean, Integer
from sqlalchemy.orm import relationship

from app.database import Base


def generate_uuid():
    return str(uuid.uuid4())


class User(Base):
    __tablename__ = "users"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    email = Column(String(255), unique=True, nullable=False, index=True)
    username = Column(String(100), unique=True, nullable=True, index=True)
    hashed_password = Column(String(255), nullable=False)
    display_name = Column(String(100), nullable=True)
    created_at = Column(DateTime, default=datetime.utcnow)
    updated_at = Column(DateTime, default=datetime.utcnow, onupdate=datetime.utcnow)

    teams = relationship("TeamMember", back_populates="user", cascade="all, delete-orphan")
    owned_teams = relationship("Team", back_populates="owner", foreign_keys="Team.owner_id")
    notion_bindings = relationship("NotionBinding", back_populates="user", cascade="all, delete-orphan")
    provider_bindings = relationship("ProviderBinding", back_populates="user", cascade="all, delete-orphan")


class Team(Base):
    __tablename__ = "teams"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    name = Column(String(100), nullable=False)
    owner_id = Column(String(36), ForeignKey("users.id"), nullable=False)
    invite_code = Column(String(64), unique=True, nullable=False, index=True)
    llm_prompts = Column(Text, nullable=False, default="{}")
    created_at = Column(DateTime, default=datetime.utcnow)

    owner = relationship("User", back_populates="owned_teams", foreign_keys=[owner_id])
    members = relationship("TeamMember", back_populates="team", cascade="all, delete-orphan")
    projects = relationship("Project", back_populates="team", cascade="all, delete-orphan")
    provider_bindings = relationship("ProviderBinding", back_populates="team", cascade="all, delete-orphan")


class TeamMember(Base):
    __tablename__ = "team_members"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    team_id = Column(String(36), ForeignKey("teams.id"), nullable=False)
    user_id = Column(String(36), ForeignKey("users.id"), nullable=False)
    role = Column(String(20), default="member")  # owner, admin, member
    joined_at = Column(DateTime, default=datetime.utcnow)

    team = relationship("Team", back_populates="members")
    user = relationship("User", back_populates="teams")


class NotionBinding(Base):
    __tablename__ = "notion_bindings"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    user_id = Column(String(36), ForeignKey("users.id"), nullable=False)
    access_token = Column(Text, nullable=False)
    workspace_id = Column(String(255), nullable=True)
    workspace_name = Column(String(255), nullable=True)
    created_at = Column(DateTime, default=datetime.utcnow)

    user = relationship("User", back_populates="notion_bindings")


class ProviderBinding(Base):
    __tablename__ = "provider_bindings"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    user_id = Column(String(36), ForeignKey("users.id"), nullable=False)
    team_id = Column(String(36), ForeignKey("teams.id"), nullable=True)
    provider_type = Column(String(50), nullable=False, default="notion")
    credentials = Column(Text, nullable=False)  # JSON: {"access_token": "..."} or {"api_key": "..."}
    workspace_id = Column(String(255), nullable=True)
    workspace_name = Column(String(255), nullable=True)
    created_at = Column(DateTime, default=datetime.utcnow)

    user = relationship("User", back_populates="provider_bindings")
    team = relationship("Team", back_populates="provider_bindings")


class Project(Base):
    __tablename__ = "projects"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    team_id = Column(String(36), ForeignKey("teams.id"), nullable=False)
    name = Column(String(100), nullable=False)
    description = Column(Text, nullable=True)
    base_data_page_id = Column(String(255), nullable=True)
    model_data_page_id = Column(String(255), nullable=True)
    git_remote_url = Column(String(500), nullable=True)
    created_at = Column(DateTime, default=datetime.utcnow)

    team = relationship("Team", back_populates="projects")
    todos = relationship("Todo", back_populates="project", cascade="all, delete-orphan")
    problem_files = relationship("ProblemFile", back_populates="project", cascade="all, delete-orphan")
    citations = relationship("Citation", back_populates="project", cascade="all, delete-orphan")
    zotero_config = relationship("ZoteroConfig", back_populates="project", uselist=False, cascade="all, delete-orphan")


class Citation(Base):
    __tablename__ = "citations"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    project_id = Column(String(36), ForeignKey("projects.id"), nullable=False)
    user_id = Column(String(36), ForeignKey("users.id"), nullable=False)

    title = Column(String(500), nullable=False)
    authors = Column(Text, nullable=True)
    journal = Column(String(255), nullable=True)
    year = Column(Integer, nullable=True)
    volume = Column(String(50), nullable=True)
    issue = Column(String(50), nullable=True)
    pages = Column(String(100), nullable=True)
    doi = Column(String(255), nullable=True, index=True)
    url = Column(String(500), nullable=True)
    abstract = Column(Text, nullable=True)

    bibtex_key = Column(String(100), nullable=True)
    bibtex_type = Column(String(50), default="article")

    zotero_item_key = Column(String(50), nullable=True, index=True)
    zotero_version = Column(Integer, nullable=True)
    source = Column(String(20), default="manual")

    extra_data = Column(Text, nullable=True)

    created_at = Column(DateTime, default=datetime.utcnow)
    updated_at = Column(DateTime, default=datetime.utcnow, onupdate=datetime.utcnow)

    project = relationship("Project", back_populates="citations")
    user = relationship("User")


class ZoteroConfig(Base):
    __tablename__ = "zotero_configs"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    project_id = Column(String(36), ForeignKey("projects.id"), nullable=False, unique=True)
    api_key = Column(String(255), nullable=False)
    library_id = Column(String(50), nullable=False)
    library_type = Column(String(20), default="user")
    last_sync_version = Column(Integer, nullable=True)
    last_sync_at = Column(DateTime, nullable=True)
    last_sync_status = Column(String(20), default="idle")
    last_sync_error = Column(Text, nullable=True)
    created_at = Column(DateTime, default=datetime.utcnow)

    project = relationship("Project", back_populates="zotero_config")


class Todo(Base):
    __tablename__ = "todos"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    project_id = Column(String(36), ForeignKey("projects.id"), nullable=False)
    user_id = Column(String(36), ForeignKey("users.id"), nullable=False)
    content = Column(Text, nullable=False)
    is_team_todo = Column(Boolean, default=False)
    due_date = Column(DateTime, nullable=True)
    reminder_enabled = Column(Boolean, default=False)
    reminder_at = Column(DateTime, nullable=True)
    reminder_detected = Column(Boolean, default=False)
    reminder_acked = Column(Boolean, default=False)
    completed = Column(Boolean, default=False)
    created_at = Column(DateTime, default=datetime.utcnow)

    project = relationship("Project", back_populates="todos")
    user = relationship("User")


class ProblemFile(Base):
    __tablename__ = "problem_files"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    project_id = Column(String(36), ForeignKey("projects.id"), nullable=False)
    filename = Column(String(255), nullable=False)
    file_path = Column(String(500), nullable=False)
    file_type = Column(String(50), nullable=False)
    extracted_text = Column(Text, nullable=True)
    uploaded_at = Column(DateTime, default=datetime.utcnow)

    project = relationship("Project", back_populates="problem_files")


class TimelineEvent(Base):
    __tablename__ = "timeline_events"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    project_id = Column(String(36), ForeignKey("projects.id"), nullable=False)
    user_id = Column(String(36), ForeignKey("users.id"), nullable=False)
    title = Column(String(255), nullable=False)
    description = Column(Text, nullable=True)
    start_time = Column(DateTime, nullable=False)
    end_time = Column(DateTime, nullable=True)
    is_team_event = Column(Boolean, default=False)
    reminder_enabled = Column(Boolean, default=False)
    reminder_minutes_before = Column(Integer, nullable=True)
    reminder_detected = Column(Boolean, default=False)
    reminder_acked = Column(Boolean, default=False)
    created_at = Column(DateTime, default=datetime.utcnow)

    user = relationship("User")


class ModelSnapshot(Base):
    __tablename__ = "model_snapshots"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    project_id = Column(String(36), ForeignKey("projects.id"), nullable=False)
    user_id = Column(String(36), ForeignKey("users.id"), nullable=False)
    commit_message = Column(String(255), nullable=False)
    notion_page_id = Column(String(255), nullable=False)
    snapshot_content = Column(Text, nullable=True)
    created_at = Column(DateTime, default=datetime.utcnow)

    user = relationship("User")
