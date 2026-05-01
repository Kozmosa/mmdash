from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from app.core.config import get_settings
from app.database import engine, Base, SessionLocal
from app.api import auth, teams, projects, home, timeline, model, model_version, git

# Import provider modules to trigger registration
from app.services import notion_provider, local_file_provider, documosa_provider

settings = get_settings()

Base.metadata.create_all(bind=engine)

# ─── One-time migration: notion_bindings → provider_bindings ─────────────────
def _migrate_notion_bindings():
    """Migrate legacy NotionBinding data to new ProviderBinding table."""
    from sqlalchemy import inspect
    inspector = inspect(engine)
    tables = inspector.get_table_names()

    if "notion_bindings" not in tables or "provider_bindings" not in tables:
        return

    db = SessionLocal()
    try:
        from app.models import NotionBinding, ProviderBinding
        # Check if migration already done
        provider_count = db.query(ProviderBinding).count()
        if provider_count > 0:
            return

        notion_bindings = db.query(NotionBinding).all()
        for nb in notion_bindings:
            pb = ProviderBinding(
                id=nb.id,
                user_id=nb.user_id,
                provider_type="notion",
                credentials=__import__("json").dumps({"access_token": nb.access_token}),
                workspace_id=nb.workspace_id,
                workspace_name=nb.workspace_name,
                created_at=nb.created_at,
            )
            db.add(pb)
        db.commit()
    except Exception:
        db.rollback()
    finally:
        db.close()


_migrate_notion_bindings()


# ─── One-time migration: populate username for existing users ─────────────────
def _migrate_usernames():
    """Populate username field for existing users from email prefix."""
    from sqlalchemy import inspect
    inspector = inspect(engine)
    tables = inspector.get_table_names()
    if "users" not in tables:
        return

    db = SessionLocal()
    try:
        from app.models import User
        users = db.query(User).filter(User.username.is_(None)).all()
        for user in users:
            prefix = user.email.split("@")[0]
            username = prefix
            suffix = 1
            while db.query(User).filter(User.username == username).first():
                username = f"{prefix}{suffix}"
                suffix += 1
            user.username = username
        db.commit()
    except Exception:
        db.rollback()
    finally:
        db.close()


_migrate_usernames()

app = FastAPI(
    title=settings.APP_NAME,
    debug=settings.DEBUG,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["http://localhost:3000"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

app.include_router(auth.router, prefix="/api/auth", tags=["认证"])
app.include_router(teams.router, prefix="/api/teams", tags=["团队"])
app.include_router(projects.router, prefix="/api/projects", tags=["项目"])
app.include_router(home.router, prefix="/api/home", tags=["主页"])
app.include_router(timeline.router, prefix="/api/timeline", tags=["时间线"])
app.include_router(model.router, prefix="/api/model", tags=["模型"])
app.include_router(model_version.router, prefix="/api/model-version", tags=["模型版本"])
app.include_router(git.router, prefix="/api/git", tags=["Git"])


@app.get("/health")
def health_check():
    return {"status": "ok"}
