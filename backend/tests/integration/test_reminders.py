from datetime import datetime, timedelta
from app.models import Todo, TimelineEvent


def test_pending_reminders_requires_auth(client):
    response = client.get("/api/reminders/proj-1/pending")
    assert response.status_code == 401


def test_get_pending_returns_unacked(auth_client, test_user, team, project, db):
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
    assert len(data["events"]) == 1
    assert data["events"][0]["id"] == "evt-rem-1"


def test_ack_sets_acked(auth_client, test_user, team, project, db):
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

    # Verify acked
    response2 = auth_client.get(f"/api/reminders/{project.id}/pending")
    assert response.status_code == 200
    data = response2.json()
    assert len(data["todos"]) == 0


def test_ack_invalid_type(auth_client, test_user, team, project):
    response = auth_client.post(
        f"/api/reminders/{project.id}/ack",
        json={"type": "invalid", "ids": ["x"]},
    )
    assert response.status_code == 400
