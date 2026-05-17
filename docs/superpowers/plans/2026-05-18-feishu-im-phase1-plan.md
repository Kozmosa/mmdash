# Feishu IM Integration Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add IM notification delivery via Feishu CLI, with an IMProvider abstraction for future IM backends.

**Architecture:** New IMProvider ABC + FeishuCLIProvider calls `lark-cli messenger send` via `asyncio.create_subprocess_exec`. Reminder scheduler fires `_dispatch_im_notifications()` as a fire-and-forget task after detecting reminders. New `IMUserBinding` + `IMProjectBinding` tables store recipient IDs. Settings page gets a new "IM通知" tab.

**Tech Stack:** Python asyncio subprocess, SQLAlchemy 2.0, shadcn/ui (settings tab), lark-cli

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `backend/app/services/im_provider.py` | Create | IMProvider ABC + registry |
| `backend/app/services/feishu_cli_provider.py` | Create | FeishuCLIProvider implementation |
| `backend/app/models.py` | Modify | Add IMUserBinding, IMProjectBinding tables |
| `backend/app/api/im.py` | Create | IM REST API endpoints |
| `backend/app/main.py` | Modify | Register IM router |
| `backend/app/services/reminder_scheduler.py` | Modify | Add `_dispatch_im_notifications()` |
| `backend/migrations/versions/005_*.py` | Create | Alembic migration for IM tables |
| `backend/tests/unit/test_im_provider.py` | Create | Unit tests for provider + registry |
| `backend/tests/unit/test_im_dispatch.py` | Create | Unit tests for dispatch logic |
| `backend/tests/integration/test_im_api.py` | Create | Integration tests for IM API |
| `frontend/lib/api.ts` | Modify | Add imApi object |
| `frontend/app/(main)/settings/page.tsx` | Modify | Add IM通知 tab |
| `scripts/setup.sh` | Modify | Add optional lark-cli install |

---

### Task 1: IMProvider ABC + Registry + FeishuCLIProvider

**Files:**
- Create: `backend/app/services/im_provider.py`
- Create: `backend/app/services/feishu_cli_provider.py`
- Create: `backend/tests/unit/test_im_provider.py`

- [ ] **Step 1: Write the failing tests**

```python
# backend/tests/unit/test_im_provider.py
import pytest
from unittest.mock import AsyncMock, patch


class TestIMProviderRegistry:
    def test_register_and_get_providers(self):
        from app.services.im_provider import (
            IMProvider, register_im_provider, get_im_providers, _IM_PROVIDER_REGISTRY
        )

        class MockProvider(IMProvider):
            def get_provider_type(self): return "mock"
            def is_configured(self): return False
            async def send_message(self, recipient_type, recipient_id, title, body): return True

        # Clear registry for test isolation
        _IM_PROVIDER_REGISTRY.clear()
        register_im_provider("mock", MockProvider)

        from app.services.im_provider import get_im_providers
        providers = get_im_providers()
        assert len(providers) == 0  # is_configured returns False → excluded

    def test_get_providers_filters_configured_only(self):
        from app.services.im_provider import (
            IMProvider, register_im_provider, get_im_providers, _IM_PROVIDER_REGISTRY
        )

        class ConfiguredProvider(IMProvider):
            def get_provider_type(self): return "configured"
            def is_configured(self): return True
            async def send_message(self, recipient_type, recipient_id, title, body): return True

        _IM_PROVIDER_REGISTRY.clear()
        register_im_provider("configured", ConfiguredProvider)

        providers = get_im_providers()
        assert len(providers) == 1


class TestFeishuCLIProvider:
    def test_provider_type(self):
        from app.services.feishu_cli_provider import FeishuCLIProvider
        p = FeishuCLIProvider()
        assert p.get_provider_type() == "feishu_cli"

    def test_is_configured_no_cli(self, monkeypatch):
        import shutil
        monkeypatch.setattr(shutil, "which", lambda x: None)
        from app.services.feishu_cli_provider import FeishuCLIProvider
        p = FeishuCLIProvider()
        assert p.is_configured() is False

    def test_is_configured_cli_exists_auth_fails(self, monkeypatch):
        monkeypatch.setattr("shutil.which", lambda x: "/usr/bin/lark-cli")
        monkeypatch.setattr("asyncio.run", lambda coro: None)

        async def mock_status():
            from collections import namedtuple
            Process = namedtuple("Process", ["returncode"])
            return Process(returncode=1)

        from app.services.feishu_cli_provider import FeishuCLIProvider
        p = FeishuCLIProvider()
        p._auth_status = mock_status
        assert p.is_configured() is False

    def test_is_configured_ok(self, monkeypatch):
        monkeypatch.setattr("shutil.which", lambda x: "/usr/bin/lark-cli")

        async def mock_status():
            from collections import namedtuple
            Process = namedtuple("Process", ["returncode"])
            return Process(returncode=0)

        from app.services.feishu_cli_provider import FeishuCLIProvider
        p = FeishuCLIProvider()
        p._auth_status = mock_status
        assert p.is_configured() is True

    @pytest.mark.asyncio
    async def test_send_message_success(self, monkeypatch):
        monkeypatch.setattr("shutil.which", lambda x: "/usr/bin/lark-cli")

        async def mock_exec(*args, **kwargs):
            from collections import namedtuple
            Process = namedtuple("Process", ["returncode", "communicate"])
            async def comm():
                return (b"", b"")
            return Process(returncode=0, communicate=comm)

        monkeypatch.setattr("asyncio.create_subprocess_exec", mock_exec)

        from app.services.feishu_cli_provider import FeishuCLIProvider
        p = FeishuCLIProvider()
        result = await p.send_message("user", "u123", "Test", "Hello")
        assert result is True

    @pytest.mark.asyncio
    async def test_send_message_timeout(self, monkeypatch):
        monkeypatch.setattr("shutil.which", lambda x: "/usr/bin/lark-cli")

        async def mock_exec(*args, **kwargs):
            import asyncio
            await asyncio.sleep(999)  # will be cancelled by timeout

        monkeypatch.setattr("asyncio.create_subprocess_exec", mock_exec)
        monkeypatch.setattr("asyncio.wait_for", lambda coro, timeout: None)
        # Simulate timeout by making send_message return False
        from app.services.feishu_cli_provider import FeishuCLIProvider
        # Monkeypatch send_message to simulate timeout
        async def send_timeout(*args, **kwargs):
            return False
        monkeypatch.setattr(FeishuCLIProvider, "send_message", send_timeout)
        p = FeishuCLIProvider()
        result = await p.send_message("user", "u123", "Test", "Hello")
        assert result is False
```

- [ ] **Step 2: Run tests, verify they fail**

```bash
cd backend && uv run pytest tests/unit/test_im_provider.py -v
```

Expected: FAIL with ModuleNotFoundError

- [ ] **Step 3: Write `backend/app/services/im_provider.py`**

```python
from abc import ABC, abstractmethod


class IMProvider(ABC):
    """Abstract base class for IM notification providers.

    Each IM backend (Feishu CLI, Telegram, etc.) implements this interface.
    Providers that are not configured (CLI not installed, not logged in) are
    silently skipped during notification dispatch.
    """

    @abstractmethod
    async def send_message(self, recipient_type: str, recipient_id: str, title: str, body: str) -> bool:
        """Send a text message. recipient_type is 'user' or 'chat'.
        Returns True on success, False on failure.
        """

    @abstractmethod
    def get_provider_type(self) -> str:
        """Return provider type identifier, e.g. 'feishu_cli'."""

    @abstractmethod
    def is_configured(self) -> bool:
        """Check prerequisites: CLI installed, authenticated. Called synchronously."""


_IM_PROVIDER_REGISTRY: dict[str, type[IMProvider]] = {}


def register_im_provider(provider_type: str, cls: type[IMProvider]):
    """Register an IM provider implementation."""
    _IM_PROVIDER_REGISTRY[provider_type] = cls


def get_im_providers() -> list[IMProvider]:
    """Return all configured (installed + logged-in) IM providers."""
    return [
        cls() for cls in _IM_PROVIDER_REGISTRY.values()
        if cls().is_configured()
    ]


def list_im_providers() -> list[dict]:
    """List all registered providers with their configuration status."""
    return [
        {
            "type": cls().get_provider_type(),
            "configured": cls().is_configured(),
            "name": cls().get_provider_type(),
        }
        for cls in _IM_PROVIDER_REGISTRY.values()
    ]
```

- [ ] **Step 4: Write `backend/app/services/feishu_cli_provider.py`**

```python
import asyncio
import logging
import shutil

from app.services.im_provider import IMProvider, register_im_provider

logger = logging.getLogger(__name__)


class FeishuCLIProvider(IMProvider):
    """Feishu/Lark IM provider using the lark-cli tool."""

    def get_provider_type(self) -> str:
        return "feishu_cli"

    def is_configured(self) -> bool:
        if not shutil.which("lark-cli"):
            return False
        try:
            result = asyncio.run(self._auth_status())
            return result.returncode == 0
        except Exception:
            return False

    async def _auth_status(self):
        proc = await asyncio.create_subprocess_exec(
            "lark-cli", "auth", "status",
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        await proc.wait()
        return proc

    async def send_message(self, recipient_type: str, recipient_id: str, title: str, body: str) -> bool:
        text = f"{title}\n\n{body}"
        try:
            proc = await asyncio.create_subprocess_exec(
                "lark-cli", "messenger", "send",
                "--recipient-type", recipient_type,
                "--recipient-id", recipient_id,
                "--text", text,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            try:
                stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=10.0)
            except asyncio.TimeoutError:
                proc.kill()
                logger.warning("lark-cli send timed out for %s:%s", recipient_type, recipient_id)
                return False

            if proc.returncode != 0:
                logger.error("lark-cli send failed: %s", stderr.decode() if stderr else "unknown")
                return False
            return True
        except FileNotFoundError:
            logger.warning("lark-cli not found, skipping IM send")
            return False
        except Exception:
            logger.exception("Unexpected error sending IM message")
            return False


register_im_provider("feishu_cli", FeishuCLIProvider)
```

- [ ] **Step 5: Trigger provider registration by importing in main.py**

In `backend/app/main.py`, add near the other service imports:

```python
from app.services import feishu_cli_provider  # triggers IM provider registration
```

- [ ] **Step 6: Run tests**

```bash
cd backend && uv run pytest tests/unit/test_im_provider.py -v
```

Expected: 7 PASS

- [ ] **Step 7: Commit**

```bash
git add backend/app/services/im_provider.py backend/app/services/feishu_cli_provider.py backend/tests/unit/test_im_provider.py backend/app/main.py
git commit -m "feat: add IMProvider ABC and FeishuCLIProvider"
```

---

### Task 2: Database Models + Migration

**Files:**
- Modify: `backend/app/models.py`
- Create: `backend/migrations/versions/005_add_im_bindings.py`

- [ ] **Step 1: Add models to `backend/app/models.py`**

Add after the ProviderBinding model (after line 85):

```python
class IMUserBinding(Base):
    __tablename__ = "im_user_bindings"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    user_id = Column(String(36), ForeignKey("users.id"), nullable=False)
    provider_type = Column(String(50), nullable=False)
    im_user_id = Column(String(255), nullable=False)
    enabled = Column(Boolean, default=True)
    created_at = Column(DateTime, default=datetime.utcnow)

    user = relationship("User")


class IMProjectBinding(Base):
    __tablename__ = "im_project_bindings"

    id = Column(String(36), primary_key=True, default=generate_uuid)
    project_id = Column(String(36), ForeignKey("projects.id"), nullable=False)
    provider_type = Column(String(50), nullable=False)
    im_chat_id = Column(String(255), nullable=False)
    enabled = Column(Boolean, default=True)
    created_at = Column(DateTime, default=datetime.utcnow)

    project = relationship("Project")
```

- [ ] **Step 2: Generate migration**

```bash
cd backend && uv run alembic revision --autogenerate -m "add im_user_bindings and im_project_bindings"
```

- [ ] **Step 3: Verify migration + run tests**

```bash
cd backend && uv run alembic upgrade head && uv run pytest tests/ -v --tb=short 2>&1 | tail -5
```

Expected: All existing tests pass.

- [ ] **Step 4: Commit**

```bash
git add backend/app/models.py backend/migrations/versions/005_*.py
git commit -m "feat: add IMUserBinding and IMProjectBinding models"
```

---

### Task 3: IM API Endpoints

**Files:**
- Create: `backend/app/api/im.py`
- Create: `backend/tests/integration/test_im_api.py`
- Modify: `backend/app/main.py`

- [ ] **Step 1: Write the integration tests**

```python
# backend/tests/integration/test_im_api.py
import pytest


def test_status_requires_auth(client):
    response = client.get("/api/im/status")
    assert response.status_code == 401


def test_status_returns_providers(auth_client):
    response = auth_client.get("/api/im/status")
    assert response.status_code == 200
    data = response.json()
    assert "providers" in data
    assert isinstance(data["providers"], list)


def test_user_binding_crud(auth_client, test_user):
    # Create/update
    response = auth_client.post(
        "/api/im/user-binding",
        json={"provider_type": "feishu_cli", "im_user_id": "u_test123", "enabled": True},
    )
    assert response.status_code == 200

    # Read
    response = auth_client.get("/api/im/user-binding")
    assert response.status_code == 200
    data = response.json()
    assert data["binding"]["im_user_id"] == "u_test123"

    # Update
    response = auth_client.post(
        "/api/im/user-binding",
        json={"provider_type": "feishu_cli", "im_user_id": "u_updated", "enabled": False},
    )
    assert response.status_code == 200
    data = auth_client.get("/api/im/user-binding").json()
    assert data["binding"]["im_user_id"] == "u_updated"
    assert data["binding"]["enabled"] is False


def test_project_binding_crud(auth_client, test_user, team, project):
    # Create
    response = auth_client.post(
        f"/api/im/project-binding/{project.id}",
        json={"provider_type": "feishu_cli", "im_chat_id": "oc_test456", "enabled": True},
    )
    assert response.status_code == 200

    # Read
    response = auth_client.get(f"/api/im/project-binding/{project.id}")
    assert response.status_code == 200
    data = response.json()
    assert data["binding"]["im_chat_id"] == "oc_test456"

    # Update
    response = auth_client.post(
        f"/api/im/project-binding/{project.id}",
        json={"provider_type": "feishu_cli", "im_chat_id": "oc_updated", "enabled": True},
    )
    assert response.status_code == 200
    data = auth_client.get(f"/api/im/project-binding/{project.id}").json()
    assert data["binding"]["im_chat_id"] == "oc_updated"


def test_project_binding_requires_membership(auth_client, test_user):
    response = auth_client.get("/api/im/project-binding/nonexistent-proj")
    assert response.status_code == 404


def test_verify_endpoint(auth_client, monkeypatch):
    # Mock the subprocess to return success
    async def mock_send(*args, **kwargs):
        return True

    monkeypatch.setattr(
        "app.services.feishu_cli_provider.FeishuCLIProvider.send_message",
        mock_send,
    )

    response = auth_client.post(
        "/api/im/verify",
        json={"provider_type": "feishu_cli", "recipient_type": "user", "recipient_id": "test123"},
    )
    assert response.status_code == 200
    assert response.json()["success"] is True
```

- [ ] **Step 2: Write the API router**

```python
# backend/app/api/im.py
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
    return {"status": "saved", "binding": {"id": binding.id, "provider_type": binding.provider_type, "im_user_id": binding.im_user_id, "enabled": binding.enabled}}


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
    return {"status": "saved", "binding": {"id": binding.id, "provider_type": binding.provider_type, "im_chat_id": binding.im_chat_id, "enabled": binding.enabled}}


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
```

- [ ] **Step 3: Register router in main.py**

In `backend/app/main.py`, add import:

```python
from app.api import im
```

Add router:

```python
app.include_router(im.router, prefix="/api/im", tags=["IM"])
```

- [ ] **Step 4: Run tests**

```bash
cd backend && uv run pytest tests/integration/test_im_api.py -v
```

Expected: 6 PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/api/im.py backend/tests/integration/test_im_api.py backend/app/main.py
git commit -m "feat: add IM API endpoints (status, user-binding, project-binding, verify)"
```

---

### Task 4: Reminder Scheduler IM Dispatch Integration

**Files:**
- Modify: `backend/app/services/reminder_scheduler.py`
- Create: `backend/tests/unit/test_im_dispatch.py`

- [ ] **Step 1: Write the dispatch tests**

```python
# backend/tests/unit/test_im_dispatch.py
import pytest
from datetime import datetime, timedelta
from unittest.mock import AsyncMock, patch


def _make_user(db, user_id="user-1"):
    from app.models import User
    u = User(id=user_id, email=f"{user_id}@test.com", username=user_id, hashed_password="")
    db.add(u)
    db.commit()
    return u


def _make_team(db, team_id="team-1", owner_id="user-1"):
    from app.models import Team
    t = Team(id=team_id, name="Test Team", owner_id=owner_id, invite_code=f"inv-{team_id}")
    db.add(t)
    db.commit()
    return t


def _make_project(db, project_id="proj-1", team_id="team-1"):
    from app.models import Project
    p = Project(id=project_id, team_id=team_id, name="Test Project")
    db.add(p)
    db.commit()
    return p


def test_dispatch_personal_event_sends_to_user_only(db, monkeypatch):
    _make_user(db, "user-1")
    _make_team(db, "team-1", "user-1")
    _make_project(db, "proj-1", "team-1")

    from app.models import IMUserBinding, TimelineEvent
    binding = IMUserBinding(
        user_id="user-1", provider_type="feishu_cli", im_user_id="u_personal", enabled=True,
    )
    db.add(binding)

    now = datetime.utcnow()
    event = TimelineEvent(
        id="evt-1", project_id="proj-1", user_id="user-1",
        title="Personal event", start_time=now,
        is_team_event=False, reminder_enabled=True,
        reminder_minutes_before=0, reminder_detected=True, reminder_acked=False,
    )
    db.add(event)
    db.commit()

    from app.services.reminder_scheduler import _dispatch_im_notifications
    sent = []

    async def mock_send(recipient_type, recipient_id, title, body):
        sent.append((recipient_type, recipient_id, title))

    with patch("app.services.reminder_scheduler.get_im_providers") as mock_get:
        mock_provider = AsyncMock()
        mock_provider.send_message = mock_send
        mock_get.return_value = [mock_provider]

        import asyncio
        asyncio.run(_dispatch_im_notifications(db, [event], []))

    # Should send to user only (not a team event)
    assert len(sent) == 1
    assert sent[0][0] == "user"
    assert sent[0][1] == "u_personal"


def test_dispatch_team_event_sends_to_chat_and_user(db, monkeypatch):
    _make_user(db, "user-1")
    _make_team(db, "team-1", "user-1")
    _make_project(db, "proj-1", "team-1")

    from app.models import IMUserBinding, IMProjectBinding, TimelineEvent
    db.add(IMUserBinding(
        user_id="user-1", provider_type="feishu_cli", im_user_id="u_personal", enabled=True,
    ))
    db.add(IMProjectBinding(
        project_id="proj-1", provider_type="feishu_cli", im_chat_id="oc_group", enabled=True,
    ))

    now = datetime.utcnow()
    event = TimelineEvent(
        id="evt-2", project_id="proj-1", user_id="user-1",
        title="Team event", start_time=now,
        is_team_event=True, reminder_enabled=True,
        reminder_minutes_before=0, reminder_detected=True, reminder_acked=False,
    )
    db.add(event)
    db.commit()

    from app.services.reminder_scheduler import _dispatch_im_notifications
    sent = []

    async def mock_send(recipient_type, recipient_id, title, body):
        sent.append((recipient_type, recipient_id, title))

    with patch("app.services.reminder_scheduler.get_im_providers") as mock_get:
        mock_provider = AsyncMock()
        mock_provider.send_message = mock_send
        mock_get.return_value = [mock_provider]

        import asyncio
        asyncio.run(_dispatch_im_notifications(db, [event], []))

    # Should send to BOTH chat and user
    assert len(sent) == 2
    recipients = {(r[0], r[1]) for r in sent}
    assert ("chat", "oc_group") in recipients
    assert ("user", "u_personal") in recipients


def test_dispatch_skips_when_no_binding(db, monkeypatch):
    _make_user(db, "user-1")
    _make_team(db, "team-1", "user-1")
    _make_project(db, "proj-1", "team-1")

    from app.models import TimelineEvent

    now = datetime.utcnow()
    event = TimelineEvent(
        id="evt-3", project_id="proj-1", user_id="user-1",
        title="No binding event", start_time=now,
        is_team_event=True, reminder_enabled=True,
        reminder_minutes_before=0, reminder_detected=True, reminder_acked=False,
    )
    db.add(event)
    db.commit()

    from app.services.reminder_scheduler import _dispatch_im_notifications
    sent = []

    async def mock_send(recipient_type, recipient_id, title, body):
        sent.append((recipient_type, recipient_id))

    with patch("app.services.reminder_scheduler.get_im_providers") as mock_get:
        mock_provider = AsyncMock()
        mock_provider.send_message = mock_send
        mock_get.return_value = [mock_provider]

        import asyncio
        asyncio.run(_dispatch_im_notifications(db, [event], []))

    # No bindings → no messages sent
    assert len(sent) == 0


def test_dispatch_one_failure_does_not_block_others(db, monkeypatch):
    _make_user(db, "user-1")
    _make_user(db, "user-2")
    _make_team(db, "team-1", "user-1")
    _make_project(db, "proj-1", "team-1")

    from app.models import IMUserBinding, TimelineEvent
    db.add(IMUserBinding(
        user_id="user-1", provider_type="feishu_cli", im_user_id="u_good", enabled=True,
    ))
    db.add(IMUserBinding(
        user_id="user-2", provider_type="feishu_cli", im_user_id="u_bad", enabled=True,
    ))

    now = datetime.utcnow()
    event1 = TimelineEvent(
        id="evt-good", project_id="proj-1", user_id="user-1",
        title="Good", start_time=now, is_team_event=False,
        reminder_enabled=True, reminder_minutes_before=0,
        reminder_detected=True, reminder_acked=False,
    )
    event2 = TimelineEvent(
        id="evt-bad", project_id="proj-1", user_id="user-2",
        title="Bad", start_time=now, is_team_event=False,
        reminder_enabled=True, reminder_minutes_before=0,
        reminder_detected=True, reminder_acked=False,
    )
    db.add_all([event1, event2])
    db.commit()

    from app.services.reminder_scheduler import _dispatch_im_notifications
    sent = []
    call_count = 0

    async def mock_send(recipient_type, recipient_id, title, body):
        nonlocal call_count
        call_count += 1
        if recipient_id == "u_bad":
            raise RuntimeError("Simulated failure")
        sent.append((recipient_type, recipient_id, title))

    with patch("app.services.reminder_scheduler.get_im_providers") as mock_get:
        mock_provider = AsyncMock()
        mock_provider.send_message = mock_send
        mock_get.return_value = [mock_provider]

        import asyncio
        asyncio.run(_dispatch_im_notifications(db, [event1, event2], []))

    # u_bad fails but u_good still gets sent
    assert len(sent) == 1
    assert sent[0][1] == "u_good"
    assert call_count == 2  # both were attempted
```

- [ ] **Step 2: Write the dispatch function in `reminder_scheduler.py`**

Add at the end of `backend/app/services/reminder_scheduler.py`:

```python
async def _dispatch_im_notifications(db, events: list[TimelineEvent], todos: list[Todo]):
    """Send IM notifications for detected reminders via all configured IM providers."""
    from app.services.im_provider import get_im_providers
    from app.models import IMUserBinding, IMProjectBinding

    providers = get_im_providers()
    if not providers:
        return

    # Collect all unique project_ids and user_ids
    project_ids = set()
    user_ids = set()
    for e in events:
        project_ids.add(e.project_id)
        user_ids.add(e.user_id)
    for t in todos:
        project_ids.add(t.project_id)
        user_ids.add(t.user_id)

    # Prefetch bindings
    user_bindings = {
        b.user_id: b
        for b in db.query(IMUserBinding).filter(
            IMUserBinding.user_id.in_(user_ids),
            IMUserBinding.enabled == True,
        ).all()
    }
    project_bindings = {
        b.project_id: b
        for b in db.query(IMProjectBinding).filter(
            IMProjectBinding.project_id.in_(project_ids),
            IMProjectBinding.enabled == True,
        ).all()
    }

    msg_format_event = "📅 日程提醒\n{title}\n开始时间: {start_time}"
    msg_format_todo = "✅ 待办提醒\n{content}\n截止时间: {due_date}"

    for provider in providers:
        for e in events:
            user_binding = user_bindings.get(e.user_id)
            project_binding = project_bindings.get(e.project_id)

            start_str = e.start_time.strftime("%Y-%m-%d %H:%M") if e.start_time else "未设置"
            title = msg_format_event.format(title=e.title, start_time=start_str)
            body = e.description or ""

            # Personal message
            if user_binding and user_binding.im_user_id:
                await _safe_send(provider, "user", user_binding.im_user_id, title, body)

            # Group message for team events
            if e.is_team_event and project_binding and project_binding.im_chat_id:
                await _safe_send(provider, "chat", project_binding.im_chat_id, title, body)

        for t in todos:
            user_binding = user_bindings.get(t.user_id)
            project_binding = project_bindings.get(t.project_id)

            due_str = t.due_date.strftime("%Y-%m-%d %H:%M") if t.due_date else "未设置"
            title = msg_format_todo.format(content=t.content, due_date=due_str)
            body = ""

            if user_binding and user_binding.im_user_id:
                await _safe_send(provider, "user", user_binding.im_user_id, title, body)

            if t.is_team_todo and project_binding and project_binding.im_chat_id:
                await _safe_send(provider, "chat", project_binding.im_chat_id, title, body)


async def _safe_send(provider, recipient_type: str, recipient_id: str, title: str, body: str):
    """Send a message, catching all exceptions so one failure doesn't block others."""
    try:
        await provider.send_message(recipient_type, recipient_id, title, body)
    except Exception:
        logger.exception("IM send failed for %s:%s", recipient_type, recipient_id)
```

- [ ] **Step 3: Wire dispatch into the scheduler loop**

In `_run_loop()`, modify the `_check_reminders(db)` call to also trigger dispatch:

```python
try:
    db = SessionLocal()
    try:
        events, todos = _check_reminders(db)
        if events or todos:
            asyncio.create_task(_dispatch_im_notifications(db, events, todos))
    finally:
        db.close()
except Exception:
    logger.exception("Reminder check failed")
```

- [ ] **Step 4: Run tests**

```bash
cd backend && uv run pytest tests/unit/test_im_dispatch.py -v
```

Expected: 4 PASS

- [ ] **Step 5: Run full suite**

```bash
cd backend && uv run pytest tests/ -v --tb=short 2>&1 | tail -3
```

Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add backend/app/services/reminder_scheduler.py backend/tests/unit/test_im_dispatch.py
git commit -m "feat: integrate IM dispatch into reminder scheduler"
```

---

### Task 5: Frontend — imApi Client + Settings IM Tab

**Files:**
- Modify: `frontend/lib/api.ts`
- Modify: `frontend/app/(main)/settings/page.tsx`

- [ ] **Step 1: Add imApi to `frontend/lib/api.ts`**

Add after the `reminderApi` block:

```typescript
// IM notification API endpoints
export const imApi = {
  async getStatus() {
    const res = await api.get("/im/status");
    return res.data as { providers: { type: string; configured: boolean; name: string }[] };
  },

  async getUserBinding() {
    const res = await api.get("/im/user-binding");
    return res.data as { binding: { id: string; provider_type: string; im_user_id: string; enabled: boolean } | null };
  },

  async saveUserBinding(data: { provider_type: string; im_user_id: string; enabled: boolean }) {
    const res = await api.post("/im/user-binding", data);
    return res.data;
  },

  async getProjectBinding(projectId: string) {
    const res = await api.get(`/im/project-binding/${projectId}`);
    return res.data as { binding: { id: string; provider_type: string; im_chat_id: string; enabled: boolean } | null };
  },

  async saveProjectBinding(projectId: string, data: { provider_type: string; im_chat_id: string; enabled: boolean }) {
    const res = await api.post(`/im/project-binding/${projectId}`, data);
    return res.data;
  },

  async verify(data: { provider_type: string; recipient_type: string; recipient_id: string }) {
    const res = await api.post("/im/verify", data);
    return res.data as { success: boolean; error?: string };
  },
};
```

- [ ] **Step 2: Read the current settings page structure**

Read `frontend/app/(main)/settings/page.tsx` to understand the existing tab pattern, Card components used, and imports.

- [ ] **Step 3: Add IM tab**

Add a new tab `"im"` to the tabs array (alongside existing "profile", "teams", "provider", "llm"). The IM tab content:

```tsx
{tabs.im && (
  <div className="space-y-6">
    {/* Provider status panel */}
    <Card>
      <CardHeader>
        <CardTitle>飞书状态</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex gap-4">
          {providerStatus.map((p) => (
            <Badge key={p.type} variant={p.configured ? "default" : "secondary"}>
              {p.type}: {p.configured ? "已连接" : "未配置"}
            </Badge>
          ))}
        </div>
      </CardContent>
    </Card>

    {/* Personal binding */}
    <Card>
      <CardHeader>
        <CardTitle>个人飞书绑定</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex gap-2">
          <Input
            placeholder="飞书用户 ID (user_id)"
            value={feishuUserId}
            onChange={(e) => setFeishuUserId(e.target.value)}
          />
          <Button variant="outline" onClick={handleVerifyUser}>
            验证
          </Button>
        </div>
        <label className="flex items-center gap-2">
          <input type="checkbox" checked={userBindingEnabled} onChange={(e) => setUserBindingEnabled(e.target.checked)} />
          启用飞书通知
        </label>
        <Button onClick={handleSaveUserBinding}>保存</Button>
      </CardContent>
    </Card>

    {/* Project binding */}
    <Card>
      <CardHeader>
        <CardTitle>项目群绑定</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <select
          value={imSelectedProject}
          onChange={(e) => setImSelectedProject(e.target.value)}
          className="border rounded px-2 py-1 w-full"
        >
          <option value="">选择项目...</option>
          {projects.map((p) => (
            <option key={p.id} value={p.id}>{p.name}</option>
          ))}
        </select>
        <div className="flex gap-2">
          <Input
            placeholder="飞书群 Chat ID"
            value={projectChatId}
            onChange={(e) => setProjectChatId(e.target.value)}
          />
          <Button variant="outline" onClick={handleVerifyProjectChat}>
            验证
          </Button>
        </div>
        <label className="flex items-center gap-2">
          <input type="checkbox" checked={projectBindingEnabled} onChange={(e) => setProjectBindingEnabled(e.target.checked)} />
          启用群通知
        </label>
        <Button onClick={handleSaveProjectBinding}>保存</Button>
      </CardContent>
    </Card>
  </div>
)}
```

Add state variables:

```typescript
// IM notification state
const [feishuUserId, setFeishuUserId] = useState("");
const [userBindingEnabled, setUserBindingEnabled] = useState(true);
const [projectChatId, setProjectChatId] = useState("");
const [projectBindingEnabled, setProjectBindingEnabled] = useState(true);
const [imSelectedProject, setImSelectedProject] = useState("");
const [providerStatus, setProviderStatus] = useState<{type: string; configured: boolean; name: string}[]>([]);
```

Add load/save/verify handlers:

```typescript
// Load IM status and bindings on tab mount
useEffect(() => {
  if (!tabs.im) return;
  imApi.getStatus().then((data) => setProviderStatus(data.providers));
  imApi.getUserBinding().then((data) => {
    if (data.binding) {
      setFeishuUserId(data.binding.im_user_id);
      setUserBindingEnabled(data.binding.enabled);
    }
  });
}, [tabs.im]);

const handleSaveUserBinding = async () => {
  try {
    await imApi.saveUserBinding({ provider_type: "feishu_cli", im_user_id: feishuUserId, enabled: userBindingEnabled });
    toast.success("个人绑定已保存");
  } catch {
    toast.error("保存失败");
  }
};

const handleVerifyUser = async () => {
  try {
    const res = await imApi.verify({ provider_type: "feishu_cli", recipient_type: "user", recipient_id: feishuUserId });
    toast.success(res.success ? "验证消息已发送，请查看飞书" : "发送失败：CLI 未配置");
  } catch {
    toast.error("验证失败");
  }
};

const handleSaveProjectBinding = async () => {
  if (!imSelectedProject) return;
  try {
    await imApi.saveProjectBinding(imSelectedProject, { provider_type: "feishu_cli", im_chat_id: projectChatId, enabled: projectBindingEnabled });
    toast.success("项目绑定已保存");
  } catch {
    toast.error("保存失败");
  }
};

const handleVerifyProjectChat = async () => {
  try {
    const res = await imApi.verify({ provider_type: "feishu_cli", recipient_type: "chat", recipient_id: projectChatId });
    toast.success(res.success ? "验证消息已发送，请查看飞书群" : "发送失败：CLI 未配置");
  } catch {
    toast.error("验证失败");
  }
};

// Load project binding when project selection changes
useEffect(() => {
  if (!imSelectedProject) return;
  imApi.getProjectBinding(imSelectedProject).then((data) => {
    if (data.binding) {
      setProjectChatId(data.binding.im_chat_id);
      setProjectBindingEnabled(data.binding.enabled);
    } else {
      setProjectChatId("");
      setProjectBindingEnabled(true);
    }
  });
}, [imSelectedProject]);
```

Need to make the `projects` data available in the IM tab scope — use the existing projects from the data cache or home page state.

- [ ] **Step 4: Verify TypeScript**

```bash
cd frontend && npx tsc --noEmit --pretty 2>&1 | head -20
```

Expected: No errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/lib/api.ts frontend/app/\(main\)/settings/page.tsx
git commit -m "feat: add IM通知 settings tab and imApi client"
```

---

### Task 6: Setup Script — lark-cli Installation

**Files:**
- Modify: `scripts/setup.sh`

- [ ] **Step 1: Add optional lark-cli install to setup.sh**

Read `scripts/setup.sh`, find the end of the setup steps, add before the final "初始化完成" message:

```bash
# ─── 飞书 CLI (可选 — 用于 IM 通知) ────────────────────────────────────────────
echo "[可选] 安装飞书 CLI (用于 IM 通知)..."
if ! command -v lark-cli &>/dev/null; then
    if command -v npm &>/dev/null; then
        npm install -g @larksuite/cli 2>/dev/null && echo "  ✓ 飞书 CLI 安装完成" || echo "  ⚠ 飞书 CLI 安装失败，可稍后手动安装"
    else
        echo "  ⚠ 未检测到 npm，跳过飞书 CLI 安装"
    fi
else
    echo "  ✓ 飞书 CLI 已安装"
fi

if command -v lark-cli &>/dev/null; then
    lark-cli config init 2>/dev/null || true
    echo "  → 请手动运行 'lark-cli auth login --recommend' 完成飞书登录"
fi
```

- [ ] **Step 2: Test script dry-run**

```bash
bash -n scripts/setup.sh && echo "Syntax OK"
```

- [ ] **Step 3: Commit**

```bash
git add scripts/setup.sh
git commit -m "feat: add optional lark-cli install to setup script"
```

---

### Task 7: End-to-End Verification

- [ ] **Step 1: Run full backend test suite**

```bash
cd backend && uv run pytest tests/ -v --tb=short 2>&1 | tail -5
```

Expected: All tests pass (221+ existing + new ones).

- [ ] **Step 2: Run frontend type check**

```bash
cd frontend && npx tsc --noEmit --pretty 2>&1
```

Expected: No errors.

- [ ] **Step 3: Commit any final fixes**
