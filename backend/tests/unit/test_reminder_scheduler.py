import pytest
from datetime import datetime, timedelta
from app.services.reminder_scheduler import _check_reminders, DETECTION_WINDOW_SECONDS, INTERVAL_SECONDS


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


# ─── Basic detection ────────────────────────────────────────────────────────

def test_detects_todo_in_forward_window(db):
    now = datetime.utcnow()
    _make_todo(db, reminder_enabled=True, reminder_at=now + timedelta(seconds=10), reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 1
    assert todos[0].reminder_detected is True


def test_detects_event_in_forward_window(db):
    now = datetime.utcnow()
    _make_event(db, reminder_enabled=True, reminder_minutes_before=5,
                start_time=now + timedelta(minutes=5, seconds=10), reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(events) == 1
    assert events[0].reminder_detected is True


# ─── Boundary: forward edge ──────────────────────────────────────────────────

def test_todo_at_exactly_now(db):
    now = datetime.utcnow()
    _make_todo(db, reminder_enabled=True, reminder_at=now, reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 1  # now <= reminder_at


def test_todo_at_exactly_window_end(db):
    now = datetime.utcnow()
    window_end = now + timedelta(seconds=DETECTION_WINDOW_SECONDS)
    _make_todo(db, reminder_enabled=True, reminder_at=window_end, reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 1  # reminder_at <= window_end


def test_event_computed_time_at_exactly_now(db):
    now = datetime.utcnow()
    _make_event(db, reminder_enabled=True, reminder_minutes_before=5,
                start_time=now + timedelta(minutes=5), reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(events) == 1


def test_todo_just_outside_forward_window(db):
    now = datetime.utcnow()
    just_outside = now + timedelta(seconds=DETECTION_WINDOW_SECONDS + 5)
    _make_todo(db, reminder_enabled=True, reminder_at=just_outside, reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 0


# ─── Boundary: backward window cushion ──────────────────────────────────────

def test_todo_in_backward_cushion(db):
    """A reminder 10s in the past should still be detected (backward cushion)."""
    now = datetime.utcnow()
    _make_todo(db, reminder_enabled=True, reminder_at=now - timedelta(seconds=10), reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 1  # caught by window_start = now - 30s


def test_todo_at_backward_boundary(db):
    """reminder_at exactly at window_start (30s ago) should be detected."""
    now = datetime.utcnow()
    window_start = now - timedelta(seconds=INTERVAL_SECONDS)
    _make_todo(db, reminder_enabled=True, reminder_at=window_start, reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 1


def test_todo_just_outside_backward_window(db):
    """reminder_at 31s ago should NOT be detected."""
    now = datetime.utcnow()
    just_outside = now - timedelta(seconds=INTERVAL_SECONDS + 1)
    _make_todo(db, reminder_enabled=True, reminder_at=just_outside, reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 0


# ─── Skip logic ─────────────────────────────────────────────────────────────

def test_skips_already_detected_todo(db):
    now = datetime.utcnow()
    _make_todo(db, reminder_enabled=True, reminder_at=now, reminder_detected=True)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 0


def test_skips_already_detected_event(db):
    now = datetime.utcnow()
    _make_event(db, reminder_enabled=True, reminder_minutes_before=5,
                start_time=now + timedelta(minutes=5), reminder_detected=True)
    events, todos = _check_reminders(db, now=now)
    assert len(events) == 0


def test_skips_disabled_reminder(db):
    now = datetime.utcnow()
    _make_todo(db, reminder_enabled=False, reminder_at=now, reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 0


def test_skips_event_without_minutes(db):
    now = datetime.utcnow()
    _make_event(db, reminder_enabled=True, reminder_minutes_before=None,
                start_time=now + timedelta(minutes=5), reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(events) == 0


def test_skips_far_future_todo(db):
    now = datetime.utcnow()
    _make_todo(db, reminder_enabled=True, reminder_at=now + timedelta(hours=2), reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 0


# ─── Multi-item detection ───────────────────────────────────────────────────

def test_detects_multiple_todos_in_same_tick(db):
    now = datetime.utcnow()
    _make_todo(db, reminder_enabled=True, reminder_at=now + timedelta(seconds=5), reminder_detected=False)
    _make_todo(db, reminder_enabled=True, reminder_at=now + timedelta(seconds=15), reminder_detected=False)
    _make_todo(db, reminder_enabled=True, reminder_at=now + timedelta(seconds=25), reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 3
    for t in todos:
        assert t.reminder_detected is True


def test_detects_both_event_and_todo_in_same_tick(db):
    now = datetime.utcnow()
    _make_todo(db, reminder_enabled=True, reminder_at=now + timedelta(seconds=10), reminder_detected=False)
    _make_event(db, reminder_enabled=True, reminder_minutes_before=5,
                start_time=now + timedelta(minutes=5, seconds=10), reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 1
    assert len(events) == 1


# ─── Edge data ───────────────────────────────────────────────────────────────

def test_large_reminder_minutes_before(db):
    """Very large reminder_minutes_before (7 days) still works."""
    now = datetime.utcnow()
    _make_event(db, reminder_enabled=True, reminder_minutes_before=7 * 24 * 60,
                start_time=now + timedelta(days=7, seconds=10), reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(events) == 1


def test_zero_reminder_minutes(db):
    """reminder_minutes_before=0 means fire at start_time exactly."""
    now = datetime.utcnow()
    _make_event(db, reminder_enabled=True, reminder_minutes_before=0,
                start_time=now + timedelta(seconds=10), reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(events) == 1


def test_todo_without_due_date_but_with_reminder_at(db):
    """reminder_at can be set even if due_date is null (should still fire)."""
    now = datetime.utcnow()
    _make_todo(db, reminder_enabled=True, due_date=None,
               reminder_at=now + timedelta(seconds=5), reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 1


def test_mixed_detected_and_undetected(db):
    """Only undetected reminders among mixed batch get detected."""
    now = datetime.utcnow()
    t1 = _make_todo(db, reminder_enabled=True, reminder_at=now + timedelta(seconds=5), reminder_detected=False)
    t2 = _make_todo(db, reminder_enabled=True, reminder_at=now + timedelta(seconds=5), reminder_detected=True)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 1
    assert todos[0].id == t1.id


# ─── Commit behavior ────────────────────────────────────────────────────────

def test_no_commit_when_nothing_detected(db):
    """When no reminders are due, no commit should be issued (no exception either)."""
    now = datetime.utcnow()
    _make_todo(db, reminder_enabled=True, reminder_at=now + timedelta(hours=5), reminder_detected=False)
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 0
    assert len(events) == 0
