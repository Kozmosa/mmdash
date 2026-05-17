import pytest
from datetime import datetime, timedelta


def _make_todo(db, **kwargs):
    from app.models import Todo
    import uuid
    todo = Todo(
        id=f"todo-{uuid.uuid4().hex[:8]}",
        project_id="proj-1",
        user_id="user-1",
        content="test",
        **kwargs
    )
    db.add(todo)
    db.commit()
    db.refresh(todo)
    return todo


def _make_event(db, start_time=None, **kwargs):
    from app.models import TimelineEvent
    import uuid
    if start_time is None:
        start_time = datetime.utcnow()
    event = TimelineEvent(
        id=f"evt-{uuid.uuid4().hex[:8]}",
        project_id="proj-1",
        user_id="user-1",
        title="test",
        start_time=start_time,
        **kwargs
    )
    db.add(event)
    db.commit()
    db.refresh(event)
    return event


def test_check_reminders_detects_todo_in_window(db):
    now = datetime.utcnow()
    _make_todo(db,
        reminder_enabled=True,
        reminder_at=now + timedelta(seconds=10),
        reminder_detected=False,
    )
    from app.services.reminder_scheduler import _check_reminders
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 1
    assert todos[0].reminder_detected is True


def test_check_reminders_skips_already_detected(db):
    now = datetime.utcnow()
    _make_todo(db,
        reminder_enabled=True,
        reminder_at=now - timedelta(seconds=60),
        reminder_detected=True,
    )
    from app.services.reminder_scheduler import _check_reminders
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 0


def test_check_reminders_detects_event_in_window(db):
    now = datetime.utcnow()
    _make_event(db,
        reminder_enabled=True,
        reminder_minutes_before=5,
        start_time=now + timedelta(minutes=5, seconds=10),
        reminder_detected=False,
    )
    from app.services.reminder_scheduler import _check_reminders
    events, todos = _check_reminders(db, now=now)
    assert len(events) == 1
    assert events[0].reminder_detected is True


def test_check_reminders_skips_disabled_reminder(db):
    now = datetime.utcnow()
    _make_todo(db,
        reminder_enabled=False,
        reminder_at=now + timedelta(seconds=10),
        reminder_detected=False,
    )
    from app.services.reminder_scheduler import _check_reminders
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 0


def test_check_reminders_skips_events_without_minutes(db):
    now = datetime.utcnow()
    _make_event(db,
        reminder_enabled=True,
        reminder_minutes_before=None,
        start_time=now + timedelta(minutes=5),
        reminder_detected=False,
    )
    from app.services.reminder_scheduler import _check_reminders
    events, todos = _check_reminders(db, now=now)
    assert len(events) == 0


def test_check_reminders_skips_outside_window(db):
    now = datetime.utcnow()
    _make_todo(db,
        reminder_enabled=True,
        reminder_at=now + timedelta(hours=2),  # far in future
        reminder_detected=False,
    )
    from app.services.reminder_scheduler import _check_reminders
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 0
