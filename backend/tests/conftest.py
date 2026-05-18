import os
import sys

import pytest
from fastapi.testclient import TestClient
from app.core.config import get_settings


def requires_api_key():
    """Check if OPENAI_API_KEY is configured. Returns True if available."""
    return bool(get_settings().OPENAI_API_KEY)


# Auto-skip marker: @pytest.mark.requires_api
# Tests decorated with this skip automatically when OPENAI_API_KEY is empty.
# Usage: @pytest.mark.requires_api
# Run: uv run pytest          → skips API-dependent tests
# Run: OPENAI_API_KEY=sk-xxx uv run pytest  → runs them
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker
from sqlalchemy.pool import StaticPool

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from app.database import Base, get_db
from app.api.auth import get_password_hash

TEST_DATABASE_URL = "sqlite:///:memory:"

test_engine = create_engine(
    TEST_DATABASE_URL,
    connect_args={"check_same_thread": False},
    poolclass=StaticPool,
)
TestingSessionLocal = sessionmaker(autocommit=False, autoflush=False, bind=test_engine)

# Replace the app's engine and session maker so imports use test DB
import app.database as _db_module
_db_module.engine = test_engine
_db_module.SessionLocal = TestingSessionLocal

# Now import app.main — Base.metadata.create_all will use test_engine
from app.main import app
from app.models import User, Team, TeamMember, Project, Todo, TimelineEvent, NotionBinding, ProviderBinding


def override_get_db():
    db = TestingSessionLocal()
    try:
        yield db
    finally:
        db.close()


app.dependency_overrides[get_db] = override_get_db


@pytest.fixture(scope="function")
def db():
    Base.metadata.create_all(bind=test_engine)
    session = TestingSessionLocal()
    yield session
    session.close()
    Base.metadata.drop_all(bind=test_engine)


@pytest.fixture(scope="function")
def client(db):
    def _override_get_db():
        try:
            yield db
        finally:
            pass

    app.dependency_overrides[get_db] = _override_get_db
    with TestClient(app) as c:
        yield c
    app.dependency_overrides[get_db] = override_get_db


def create_test_user(db, email="test@example.com", password="testpass123", display_name="Test User"):
    user = User(
        email=email,
        hashed_password=get_password_hash(password),
        display_name=display_name,
    )
    db.add(user)
    db.commit()
    db.refresh(user)
    return user


def login_user(client, email="test@example.com", password="testpass123"):
    response = client.post(
        "/api/auth/login",
        data={"username": email, "password": password},
        headers={"Content-Type": "application/x-www-form-urlencoded"},
    )
    assert response.status_code == 200, response.text
    return response.json()["access_token"]


def auth_headers(token):
    return {"Authorization": f"Bearer {token}"}


@pytest.fixture
def test_user(db):
    return create_test_user(db)


@pytest.fixture
def auth_client(client, test_user):
    token = login_user(client, test_user.email, "testpass123")
    client.headers.update(auth_headers(token))
    return client


@pytest.fixture
def team(db, test_user):
    team = Team(name="Test Team", owner_id=test_user.id, invite_code="invite123")
    db.add(team)
    db.commit()
    db.refresh(team)
    member = TeamMember(team_id=team.id, user_id=test_user.id, role="owner")
    db.add(member)
    db.commit()
    return team


@pytest.fixture
def project(db, team):
    project = Project(
        team_id=team.id,
        name="Test Project",
        description="A test project",
    )
    db.add(project)
    db.commit()
    db.refresh(project)
    return project


@pytest.fixture
def notion_binding(db, test_user):
    binding = NotionBinding(
        user_id=test_user.id,
        access_token="notion_token_123",
        workspace_id="ws_123",
        workspace_name="Test Workspace",
    )
    db.add(binding)
    db.commit()
    db.refresh(binding)
    return binding


@pytest.fixture
def provider_binding(db, test_user):
    binding = ProviderBinding(
        user_id=test_user.id,
        provider_type="notion",
        credentials='{"access_token": "notion_token_123"}',
        workspace_id="ws_123",
        workspace_name="Test Workspace",
    )
    db.add(binding)
    db.commit()
    db.refresh(binding)
    return binding


# ─── Conditional test execution ──────────────────────────────────────────────

def pytest_collection_modifyitems(config, items):
    """Auto-skip @pytest.mark.requires_api tests when OPENAI_API_KEY is empty."""
    if get_settings().OPENAI_API_KEY:
        return
    skip_api = pytest.mark.skip(reason="OPENAI_API_KEY not configured")
    for item in items:
        if "requires_api" in item.keywords:
            item.add_marker(skip_api)
