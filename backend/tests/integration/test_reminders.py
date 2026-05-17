from datetime import datetime, timedelta
from app.models import Todo, TimelineEvent


# ─── Auth checks ─────────────────────────────────────────────────────────────

def test_pending_requires_auth(client):
    response = client.get("/api/reminders/proj-1/pending")
    assert response.status_code == 401


def test_ack_requires_auth(client):
    response = client.post("/api/reminders/proj-1/ack", json={"type": "todo", "ids": ["x"]})
    assert response.status_code == 401


# ─── Happy path ──────────────────────────────────────────────────────────────

def test_pending_returns_unacked(auth_client, test_user, team, project, db):
    now = datetime.utcnow()
    todo = Todo(
        id="todo-rem-1", project_id=project.id, user_id=test_user.id,
        content="test", reminder_enabled=True, reminder_at=now + timedelta(seconds=10),
        reminder_detected=True, reminder_acked=False,
    )
    event = TimelineEvent(
        id="evt-rem-1", project_id=project.id, user_id=test_user.id,
        title="test", start_time=now,
        reminder_enabled=True, reminder_minutes_before=5,
        reminder_detected=True, reminder_acked=False,
    )
    db.add_all([todo, event])
    db.commit()

    response = auth_client.get(f"/api/reminders/{project.id}/pending")
    assert response.status_code == 200
    data = response.json()
    assert len(data["todos"]) == 1
    assert data["todos"][0]["id"] == "todo-rem-1"
    assert data["todos"][0]["content"] == "test"
    assert data["todos"][0]["due_date"] is None
    assert len(data["events"]) == 1
    assert data["events"][0]["id"] == "evt-rem-1"
    assert data["events"][0]["title"] == "test"


def test_ack_todo_sets_acked(auth_client, test_user, team, project, db):
    todo = Todo(
        id="todo-ack-1", project_id=project.id, user_id=test_user.id,
        content="test", reminder_enabled=True, reminder_at=datetime.utcnow(),
        reminder_detected=True, reminder_acked=False,
    )
    db.add(todo)
    db.commit()

    response = auth_client.post(
        f"/api/reminders/{project.id}/ack",
        json={"type": "todo", "ids": ["todo-ack-1"]},
    )
    assert response.status_code == 200

    response2 = auth_client.get(f"/api/reminders/{project.id}/pending")
    assert len(response2.json()["todos"]) == 0


def test_ack_event_sets_acked(auth_client, test_user, team, project, db):
    now = datetime.utcnow()
    event = TimelineEvent(
        id="evt-ack-1", project_id=project.id, user_id=test_user.id,
        title="test", start_time=now, reminder_enabled=True,
        reminder_minutes_before=5, reminder_detected=True, reminder_acked=False,
    )
    db.add(event)
    db.commit()

    response = auth_client.post(
        f"/api/reminders/{project.id}/ack",
        json={"type": "event", "ids": ["evt-ack-1"]},
    )
    assert response.status_code == 200

    response2 = auth_client.get(f"/api/reminders/{project.id}/pending")
    assert len(response2.json()["events"]) == 0


# ─── Corner cases ────────────────────────────────────────────────────────────

def test_ack_invalid_type(auth_client, test_user, team, project):
    response = auth_client.post(
        f"/api/reminders/{project.id}/ack",
        json={"type": "invalid", "ids": ["x"]},
    )
    assert response.status_code == 400


def test_ack_empty_ids(auth_client, test_user, team, project):
    """Ack with empty ids list should succeed (no-op)."""
    response = auth_client.post(
        f"/api/reminders/{project.id}/ack",
        json={"type": "todo", "ids": []},
    )
    assert response.status_code == 200
    assert response.json()["count"] == 0


def test_ack_nonexistent_id(auth_client, test_user, team, project):
    """Ack with a non-existent id should still succeed (idempotent no-op)."""
    response = auth_client.post(
        f"/api/reminders/{project.id}/ack",
        json={"type": "todo", "ids": ["nonexistent-id"]},
    )
    assert response.status_code == 200


def test_pending_empty_when_none_detected(auth_client, test_user, team, project, db):
    """When no reminders are in detected state, pending returns empty."""
    todo = Todo(
        id="todo-undetected", project_id=project.id, user_id=test_user.id,
        content="test", reminder_enabled=True, reminder_at=datetime.utcnow(),
        reminder_detected=False, reminder_acked=False,
    )
    db.add(todo)
    db.commit()

    response = auth_client.get(f"/api/reminders/{project.id}/pending")
    assert response.status_code == 200
    data = response.json()
    assert len(data["todos"]) == 0
    assert len(data["events"]) == 0


def test_pending_excludes_acked(auth_client, test_user, team, project, db):
    """Already-acked reminders don't appear in pending."""
    todo = Todo(
        id="todo-acked", project_id=project.id, user_id=test_user.id,
        content="test", reminder_enabled=True, reminder_at=datetime.utcnow(),
        reminder_detected=True, reminder_acked=True,
    )
    db.add(todo)
    db.commit()

    response = auth_client.get(f"/api/reminders/{project.id}/pending")
    assert response.status_code == 200
    assert len(response.json()["todos"]) == 0


def test_pending_only_shows_detected(auth_client, test_user, team, project, db):
    """Only reminder_detected=True items appear in pending, regardless of enabled status."""
    todo_detected = Todo(
        id="todo-detected", project_id=project.id, user_id=test_user.id,
        content="test", reminder_enabled=True, reminder_at=datetime.utcnow(),
        reminder_detected=True, reminder_acked=False,
    )
    todo_not_detected = Todo(
        id="todo-not-detected", project_id=project.id, user_id=test_user.id,
        content="test", reminder_enabled=True, reminder_at=datetime.utcnow(),
        reminder_detected=False, reminder_acked=False,
    )
    db.add_all([todo_detected, todo_not_detected])
    db.commit()

    response = auth_client.get(f"/api/reminders/{project.id}/pending")
    assert response.status_code == 200
    data = response.json()
    assert len(data["todos"]) == 1
    assert data["todos"][0]["id"] == "todo-detected"


def test_ack_idempotent(auth_client, test_user, team, project, db):
    """Acking twice should succeed (idempotent)."""
    todo = Todo(
        id="todo-idem", project_id=project.id, user_id=test_user.id,
        content="test", reminder_enabled=True, reminder_at=datetime.utcnow(),
        reminder_detected=True, reminder_acked=False,
    )
    db.add(todo)
    db.commit()

    r1 = auth_client.post(
        f"/api/reminders/{project.id}/ack",
        json={"type": "todo", "ids": ["todo-idem"]},
    )
    assert r1.status_code == 200
    # Second ack
    r2 = auth_client.post(
        f"/api/reminders/{project.id}/ack",
        json={"type": "todo", "ids": ["todo-idem"]},
    )
    assert r2.status_code == 200  # No 404 or 500


def test_ack_batch_multiple_ids(auth_client, test_user, team, project, db):
    """Ack multiple ids in one request."""
    todos = [
        Todo(id=f"batch-{i}", project_id=project.id, user_id=test_user.id,
             content=f"test-{i}", reminder_enabled=True,
             reminder_at=datetime.utcnow(), reminder_detected=True, reminder_acked=False)
        for i in range(3)
    ]
    db.add_all(todos)
    db.commit()

    response = auth_client.post(
        f"/api/reminders/{project.id}/ack",
        json={"type": "todo", "ids": ["batch-0", "batch-1", "batch-2"]},
    )
    assert response.status_code == 200
    assert response.json()["count"] == 3

    # All should be gone from pending
    pending = auth_client.get(f"/api/reminders/{project.id}/pending")
    assert len(pending.json()["todos"]) == 0


def test_pending_requires_team_membership(auth_client, test_user, team, project, db):
    """If user is not a team member of the project's team, return 403."""
    # Create another team that test_user is NOT in, and a project in it
    from app.models import Team, Project as ProjectModel
    import uuid
    other_team = Team(id=f"other-{uuid.uuid4().hex[:8]}", name="Other", owner_id="other-owner", invite_code="other-invite")
    db.add(other_team)
    other_project = ProjectModel(id=f"proj-other-{uuid.uuid4().hex[:8]}", team_id=other_team.id, name="Other Project")
    db.add(other_project)
    db.commit()

    response = auth_client.get(f"/api/reminders/{other_project.id}/pending")
    assert response.status_code == 403


def test_pending_nonexistent_project(auth_client):
    response = auth_client.get("/api/reminders/nonexistent-proj/pending")
    assert response.status_code == 404
