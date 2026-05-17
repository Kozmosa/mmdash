import pytest
from datetime import datetime, timedelta
from unittest.mock import AsyncMock, patch


def _make_user(db, user_id="user-1"):
    from app.models import User
    u = User(id=user_id, email=f"{user_id}@test.com", username=user_id, hashed_password="x", display_name=user_id)
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
    _make_team(db)
    _make_project(db)

    from app.models import IMUserBinding, TimelineEvent
    db.add(IMUserBinding(
        id="bind-1", user_id="user-1", provider_type="feishu_cli", im_user_id="u_personal", enabled=True,
    ))
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

    with patch("app.services.im_provider.get_im_providers") as mock_get:
        mock_provider = AsyncMock()
        mock_provider.send_message = mock_send
        mock_get.return_value = [mock_provider]
        import asyncio
        asyncio.run(_dispatch_im_notifications(db, [event], []))

    assert len(sent) == 1
    assert sent[0][0] == "user"
    assert sent[0][1] == "u_personal"


def test_dispatch_team_event_sends_to_chat_and_user(db, monkeypatch):
    _make_user(db, "user-1")
    _make_team(db)
    _make_project(db)

    from app.models import IMUserBinding, IMProjectBinding, TimelineEvent
    db.add(IMUserBinding(
        id="bind-1", user_id="user-1", provider_type="feishu_cli", im_user_id="u_personal", enabled=True,
    ))
    db.add(IMProjectBinding(
        id="pb-1", project_id="proj-1", provider_type="feishu_cli", im_chat_id="oc_group", enabled=True,
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

    with patch("app.services.im_provider.get_im_providers") as mock_get:
        mock_provider = AsyncMock()
        mock_provider.send_message = mock_send
        mock_get.return_value = [mock_provider]
        import asyncio
        asyncio.run(_dispatch_im_notifications(db, [event], []))

    assert len(sent) == 2
    recipients = {(r[0], r[1]) for r in sent}
    assert ("chat", "oc_group") in recipients
    assert ("user", "u_personal") in recipients


def test_dispatch_skips_when_no_binding(db, monkeypatch):
    _make_user(db, "user-1")
    _make_team(db)
    _make_project(db)

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

    with patch("app.services.im_provider.get_im_providers") as mock_get:
        mock_provider = AsyncMock()
        mock_provider.send_message = mock_send
        mock_get.return_value = [mock_provider]
        import asyncio
        asyncio.run(_dispatch_im_notifications(db, [event], []))

    assert len(sent) == 0


def test_dispatch_one_failure_does_not_block_others(db, monkeypatch):
    _make_user(db, "user-1")
    _make_user(db, "user-2")
    _make_team(db)
    _make_project(db)

    from app.models import IMUserBinding, TimelineEvent
    db.add(IMUserBinding(
        id="bind-1", user_id="user-1", provider_type="feishu_cli", im_user_id="u_good", enabled=True,
    ))
    db.add(IMUserBinding(
        id="bind-2", user_id="user-2", provider_type="feishu_cli", im_user_id="u_bad", enabled=True,
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

    async def mock_send(recipient_type, recipient_id, title, body):
        if recipient_id == "u_bad":
            raise RuntimeError("Simulated failure")
        sent.append((recipient_type, recipient_id, title))

    with patch("app.services.im_provider.get_im_providers") as mock_get:
        mock_provider = AsyncMock()
        mock_provider.send_message = mock_send
        mock_get.return_value = [mock_provider]
        import asyncio
        asyncio.run(_dispatch_im_notifications(db, [event1, event2], []))

    assert len(sent) == 1
    assert sent[0][1] == "u_good"
