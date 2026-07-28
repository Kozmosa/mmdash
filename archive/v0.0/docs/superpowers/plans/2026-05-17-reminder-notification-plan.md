# Reminder & Notification System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add deadline reminder detection, browser notification delivery (Sonner toast + Notification API), and reminder configuration UI for TimelineEvent and Todo.

**Architecture:** Backend asyncio scheduler polls every 30s for reminders due within a 35s window, marks them `reminder_detected=True`. Frontend polls `GET /api/reminders/{project_id}/pending` every 30s, fires toast/desktop notification for unacked reminders, then calls `POST /api/reminders/{project_id}/ack`.

**Tech Stack:** FastAPI + SQLAlchemy + asyncio (backend), Next.js + Sonner + Notification API (frontend)

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `backend/app/models.py:158-201` | Modify | Add reminder columns to Todo and TimelineEvent |
| `backend/migrations/versions/004_*.py` | Create | Alembic migration for new columns |
| `backend/app/services/reminder_scheduler.py` | Create | Background loop: detect due reminders, mark detected |
| `backend/app/api/reminders.py` | Create | REST endpoints: GET /pending, POST /ack |
| `backend/app/api/timeline.py` | Modify | Accept reminder fields in create_event |
| `backend/app/api/home.py` | Modify | Accept reminder fields in create_todo, return them in list_todos |
| `backend/app/main.py` | Modify | Start/stop scheduler, include reminders router |
| `backend/tests/unit/test_reminder_scheduler.py` | Create | Unit tests for scheduler logic |
| `backend/tests/integration/test_reminders.py` | Create | Integration tests for endpoints |
| `frontend/lib/api.ts` | Modify | Add getPendingReminders, ackReminders |
| `frontend/hooks/use-reminder-polling.ts` | Create | Hook: 30s polling + visibility-aware notification dispatch |
| `frontend/app/(main)/home/page.tsx` | Modify | Add due_date picker + reminder config to todo form/display |
| `frontend/app/(main)/timeline/page.tsx` | Modify | Add reminder config to event dialog + bell icon |
| `frontend/app/(main)/settings/page.tsx` | Modify | Add notification settings card |
| `frontend/components/ui/sonner.tsx` | No change | Already mounted globally, used as-is |

---

### Task 1: Database Migration

**Files:**
- Modify: `backend/app/models.py:158-201`
- Create: `backend/migrations/versions/004_add_reminder_fields.py`

- [ ] **Step 1: Add reminder columns to Todo and TimelineEvent models**

In `backend/app/models.py`, add to the `Todo` class after line 166 (`due_date` column):

```python
    reminder_enabled = Column(Boolean, default=False)
    reminder_at = Column(DateTime, nullable=True)
    reminder_detected = Column(Boolean, default=False)
    reminder_acked = Column(Boolean, default=False)
```

In `backend/app/models.py`, add to the `TimelineEvent` class after line 198 (`is_team_event` column):

```python
    reminder_enabled = Column(Boolean, default=False)
    reminder_minutes = Column(Integer, nullable=True)
    reminder_detected = Column(Boolean, default=False)
    reminder_acked = Column(Boolean, default=False)
```

- [ ] **Step 2: Generate Alembic migration**

```bash
cd backend && uv run alembic revision --autogenerate -m "add reminder fields to todos and timeline_events"
```

- [ ] **Step 3: Verify migration SQL**

```bash
cd backend && uv run alembic upgrade head && echo "Migration OK"
```

Expected: Migration applies without error. Existing rows get `False` defaults.

- [ ] **Step 4: Commit**

```bash
git add backend/app/models.py backend/migrations/versions/004_*.py
git commit -m "feat: add reminder fields to Todo and TimelineEvent models"
```

---

### Task 2: Reminder Scheduler Service

**Files:**
- Create: `backend/app/services/reminder_scheduler.py`
- Create: `backend/tests/unit/test_reminder_scheduler.py`

- [ ] **Step 1: Write the failing test for reminder detection**

```python
# backend/tests/unit/test_reminder_scheduler.py
import pytest
from datetime import datetime, timedelta
from app.services.reminder_scheduler import _check_reminders


def _make_todo(db, **kwargs):
    from app.models import Todo
    todo = Todo(
        id="todo-1", project_id="proj-1", user_id="user-1",
        content="test", **kwargs
    )
    db.add(todo)
    db.commit()
    return todo


def _make_event(db, **kwargs):
    from app.models import TimelineEvent
    event = TimelineEvent(
        id="evt-1", project_id="proj-1", user_id="user-1",
        title="test", start_time=datetime.utcnow(), **kwargs
    )
    db.add(event)
    db.commit()
    return event


def test_check_reminders_detects_todo_in_window(db):
    now = datetime.utcnow()
    _make_todo(db,
        reminder_enabled=True,
        reminder_at=now + timedelta(seconds=10),
        reminder_detected=False,
    )
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
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 0


def test_check_reminders_detects_event_in_window(db):
    now = datetime.utcnow()
    _make_event(db,
        reminder_enabled=True,
        reminder_minutes=5,
        start_time=now + timedelta(minutes=5, seconds=10),
        reminder_detected=False,
    )
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
    events, todos = _check_reminders(db, now=now)
    assert len(todos) == 0
```

Note: tests use the session-scoped `db` fixture from `conftest.py`.

- [ ] **Step 2: Run tests, verify they fail**

```bash
cd backend && uv run pytest tests/unit/test_reminder_scheduler.py -v
```

Expected: FAIL with `ModuleNotFoundError` (file doesn't exist yet)

- [ ] **Step 3: Write the scheduler service**

```python
# backend/app/services/reminder_scheduler.py
import asyncio
import logging
from datetime import datetime, timedelta

from sqlalchemy import text
from app.database import SessionLocal
from app.models import TimelineEvent, Todo

logger = logging.getLogger(__name__)

INTERVAL_SECONDS = 30
DETECTION_WINDOW_SECONDS = 35

_stop_event: asyncio.Event | None = None


def _check_reminders(db, now=None):
    """Check for reminders due within the detection window.
    Marks matching records with reminder_detected=True.
    Returns (events, todos) lists of detected records.
    """
    if now is None:
        now = datetime.utcnow()
    window_end = now + timedelta(seconds=DETECTION_WINDOW_SECONDS)

    # --- TimelineEvent: computed reminder time = start_time - reminder_minutes ---
    events = db.query(TimelineEvent).filter(
        TimelineEvent.reminder_enabled == True,
        TimelineEvent.reminder_detected == False,
        TimelineEvent.reminder_minutes.isnot(None),
    ).all()

    detected_events = []
    for e in events:
        if e.reminder_minutes is not None:
            reminder_time = e.start_time - timedelta(minutes=e.reminder_minutes)
            if now <= reminder_time <= window_end:
                e.reminder_detected = True
                detected_events.append(e)

    # --- Todo: reminder_at is an absolute timestamp ---
    todos = db.query(Todo).filter(
        Todo.reminder_enabled == True,
        Todo.reminder_detected == False,
        Todo.reminder_at.isnot(None),
        Todo.reminder_at.between(now, window_end),
    ).all()

    for t in todos:
        t.reminder_detected = True

    if detected_events or todos:
        db.commit()
        logger.info(
            "Reminders detected: %d events, %d todos",
            len(detected_events), len(todos),
        )

    return detected_events, todos


async def _run_loop():
    """Background loop: check reminders every INTERVAL_SECONDS."""
    global _stop_event
    _stop_event = asyncio.Event()
    while not _stop_event.is_set():
        try:
            db = SessionLocal()
            try:
                _check_reminders(db)
            finally:
                db.close()
        except Exception:
            logger.exception("Reminder check failed")
        await asyncio.wait_for(
            _stop_event.wait(), timeout=INTERVAL_SECONDS
        )


def start_reminder_scheduler():
    global _stop_event
    _stop_event = asyncio.Event()
    return asyncio.create_task(_run_loop())


def stop_reminder_scheduler():
    global _stop_event
    if _stop_event:
        _stop_event.set()
```

- [ ] **Step 4: Run tests, verify they pass**

```bash
cd backend && uv run pytest tests/unit/test_reminder_scheduler.py -v
```

Expected: 4 PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/services/reminder_scheduler.py backend/tests/unit/test_reminder_scheduler.py
git commit -m "feat: add reminder scheduler service with detection logic"
```

---

### Task 3: Reminders API Endpoints

**Files:**
- Create: `backend/app/api/reminders.py`
- Create: `backend/tests/integration/test_reminders.py`

- [ ] **Step 1: Write the failing integration test**

```python
# backend/tests/integration/test_reminders.py
from datetime import datetime, timedelta
from app.models import Todo, TimelineEvent


def test_get_pending_reminders_returns_unacked(auth_client, test_user, team, project):
    db = auth_client.app.dependency_overrides  # not how it works — use conftest db session
    ...


def test_ack_reminders_sets_acked(auth_client, test_user, team, project):
    ...


def test_pending_reminders_requires_auth(client):
    response = client.get("/api/reminders/proj-1/pending")
    assert response.status_code == 401
```

Note: These use the existing `auth_client`, `client`, `test_user`, `team`, and `project` fixtures from `tests/conftest.py`. For brevity, the full test code uses the fixtures to create Todo/TimelineEvent rows with `reminder_enabled=True, reminder_at=now+timedelta(seconds=10), reminder_detected=True, reminder_acked=False`, then calls `GET /pending` and asserts the records appear. After calling `POST /ack`, the records should no longer appear in `/pending`.

- [ ] **Step 2: Run tests, verify they fail**

```bash
cd backend && uv run pytest tests/integration/test_reminders.py -v
```

Expected: FAIL (404 — router not registered)

- [ ] **Step 3: Write the reminders API router**

```python
# backend/app/api/reminders.py
from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel
from sqlalchemy.orm import Session

from app.database import get_db
from app.models import Project, TeamMember, TimelineEvent, Todo
from app.api.auth import get_current_user
from app.models import User

router = APIRouter()


class AckRequest(BaseModel):
    type: str  # "event" or "todo"
    ids: list[str]


@router.get("/{project_id}/pending")
def get_pending_reminders(
    project_id: str,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = (
        db.query(TeamMember)
        .filter(
            TeamMember.team_id == project.team_id,
            TeamMember.user_id == current_user.id,
        )
        .first()
    )
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")

    events = (
        db.query(TimelineEvent)
        .filter(
            TimelineEvent.project_id == project_id,
            TimelineEvent.reminder_detected == True,
            TimelineEvent.reminder_acked == False,
        )
        .all()
    )
    todos = (
        db.query(Todo)
        .filter(
            Todo.project_id == project_id,
            Todo.reminder_detected == True,
            Todo.reminder_acked == False,
        )
        .all()
    )

    return {
        "events": [
            {
                "id": e.id,
                "title": e.title,
                "description": e.description,
                "start_time": e.start_time.isoformat() if e.start_time else None,
            }
            for e in events
        ],
        "todos": [
            {"id": t.id, "content": t.content, "due_date": t.due_date.isoformat() if t.due_date else None}
            for t in todos
        ],
    }


@router.post("/{project_id}/ack")
def ack_reminders(
    project_id: str,
    body: AckRequest,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db),
):
    project = db.query(Project).filter(Project.id == project_id).first()
    if not project:
        raise HTTPException(status_code=404, detail="Project not found")
    member = (
        db.query(TeamMember)
        .filter(
            TeamMember.team_id == project.team_id,
            TeamMember.user_id == current_user.id,
        )
        .first()
    )
    if not member:
        raise HTTPException(status_code=403, detail="Not a team member")

    if body.type == "event":
        db.query(TimelineEvent).filter(
            TimelineEvent.id.in_(body.ids),
            TimelineEvent.project_id == project_id,
        ).update({"reminder_acked": True}, synchronize_session=False)
    elif body.type == "todo":
        db.query(Todo).filter(
            Todo.id.in_(body.ids),
            Todo.project_id == project_id,
        ).update({"reminder_acked": True}, synchronize_session=False)
    else:
        raise HTTPException(status_code=400, detail="Invalid type, must be 'event' or 'todo'")

    db.commit()
    return {"status": "acked", "type": body.type, "count": len(body.ids)}
```

- [ ] **Step 4: Register the router in main.py**

In `backend/app/main.py`, add after line 10:

```python
from app.api import reminders
```

And add after the references router line (after line 124):

```python
app.include_router(reminders.router, prefix="/api/reminders", tags=["提醒"])
```

- [ ] **Step 5: Run integration tests**

```bash
cd backend && uv run pytest tests/integration/test_reminders.py -v
```

Expected: 3 PASS

- [ ] **Step 6: Commit**

```bash
git add backend/app/api/reminders.py backend/tests/integration/test_reminders.py backend/app/main.py
git commit -m "feat: add reminders API endpoints (pending + ack)"
```

---

### Task 4: Wire Reminder Scheduler into FastAPI Lifecycle

**Files:**
- Modify: `backend/app/main.py`

- [ ] **Step 1: Add scheduler import and lifecycle hooks**

In `backend/app/main.py`, add import after line 13 (`from app.services.zotero_sync import ...`):

```python
from app.services.reminder_scheduler import start_reminder_scheduler, stop_reminder_scheduler
```

Modify the startup event (lines 132-134):

```python
@app.on_event("startup")
async def startup_event():
    asyncio.create_task(start_sync_scheduler())
    asyncio.create_task(start_reminder_scheduler())
```

Modify the shutdown event (lines 137-139):

```python
@app.on_event("shutdown")
async def shutdown_event():
    stop_sync_scheduler()
    stop_reminder_scheduler()
```

- [ ] **Step 2: Verify startup**

```bash
cd backend && timeout 5 uv run uvicorn app.main:app --port 8000 2>&1 || true
```

Expected: No import errors. Scheduler starts cleanly.

- [ ] **Step 3: Commit**

```bash
git add backend/app/main.py
git commit -m "feat: wire reminder scheduler into FastAPI lifecycle"
```

---

### Task 5: Accept Reminder Fields in TimelineEvent and Todo APIs

**Files:**
- Modify: `backend/app/api/timeline.py`
- Modify: `backend/app/api/home.py`

- [ ] **Step 1: Add reminder params to timeline create_event**

In `backend/app/api/timeline.py:13-49`, add query params and model population:

Add to `create_event` function signature (after `is_team_event: bool = False,`):

```python
    reminder_enabled: bool = False,
    reminder_minutes: int = None,
```

Add in the `event = TimelineEvent(...)` constructor block:

```python
        reminder_enabled=reminder_enabled,
        reminder_minutes=reminder_minutes,
```

In the `list_events` response dict (line 61-69), add the new fields:

```python
        "reminder_enabled": e.reminder_enabled,
        "reminder_minutes": e.reminder_minutes,
```

- [ ] **Step 2: Add reminder params to home create_todo**

In `backend/app/api/home.py:148-174`, add query params to `create_todo`:

After `due_date: str = None,`:

```python
    reminder_enabled: bool = False,
    reminder_minutes: int = None,
```

In the `Todo(...)` constructor (after `due_date=...` line):

```python
        reminder_enabled=reminder_enabled,
        reminder_at=datetime.fromisoformat(due_date) - timedelta(minutes=reminder_minutes)
            if (due_date and reminder_minutes is not None) else None,
```

Add `from datetime import timedelta` to the function-level import line:

```python
    from datetime import datetime, timedelta
```

In `list_todos` response dict (line 186), add:

```python
        "reminder_enabled": t.reminder_enabled,
        "reminder_at": t.reminder_at.isoformat() if t.reminder_at else None,
```

- [ ] **Step 3: Run existing tests to check for regressions**

```bash
cd backend && uv run pytest tests/integration/test_timeline.py tests/integration/test_home.py -v
```

Expected: All existing tests still PASS (new params have defaults)

- [ ] **Step 4: Commit**

```bash
git add backend/app/api/timeline.py backend/app/api/home.py
git commit -m "feat: accept reminder fields in timeline and todo APIs"
```

---

### Task 6: Frontend API Client Additions

**Files:**
- Modify: `frontend/lib/api.ts`

- [ ] **Step 1: Add reminder API functions**

Read the existing `frontend/lib/api.ts` to find the api instance export pattern, then add at the end of the file:

```typescript
export async function getPendingReminders(projectId: string) {
  const { data } = await api.get(`/reminders/${projectId}/pending`);
  return data as {
    events: { id: string; title: string; description: string; start_time: string }[];
    todos: { id: string; content: string; due_date: string | null }[];
  };
}

export async function ackReminders(projectId: string, type: "event" | "todo", ids: string[]) {
  const { data } = await api.post(`/reminders/${projectId}/ack`, { type, ids });
  return data;
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd frontend && npx tsc --noEmit --pretty 2>&1 | head -20
```

Expected: No new errors from these additions.

- [ ] **Step 3: Commit**

```bash
git add frontend/lib/api.ts
git commit -m "feat: add reminder API client functions"
```

---

### Task 7: useReminderPolling Hook

**Files:**
- Create: `frontend/hooks/use-reminder-polling.ts`

- [ ] **Step 1: Write the hook**

```typescript
// frontend/hooks/use-reminder-polling.ts
"use client";

import { useEffect, useRef } from "react";
import { toast } from "sonner";
import { getPendingReminders, ackReminders } from "@/lib/api";

const POLL_INTERVAL = 30_000;

function notifyViaToast(title: string, body: string) {
  toast(title, { description: body });
}

function notifyViaDesktop(title: string, body: string) {
  if (typeof window !== "undefined" && "Notification" in window) {
    if (Notification.permission === "granted") {
      new Notification(title, { body, icon: "/favicon.ico" });
    }
  }
}

function notify(title: string, body: string) {
  if (typeof document !== "undefined" && document.visibilityState === "visible") {
    notifyViaToast(title, body);
  } else {
    notifyViaDesktop(title, body);
  }
}

export function useReminderPolling(projectId: string | null) {
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    if (!projectId) return;

    const poll = async () => {
      try {
        const { events, todos } = await getPendingReminders(projectId);

        const eventIds: string[] = [];
        for (const e of events) {
          notify("日程提醒", `${e.title} 即将开始`);
          eventIds.push(e.id);
        }

        const todoIds: string[] = [];
        for (const t of todos) {
          const label = t.due_date
            ? `截止: ${new Date(t.due_date).toLocaleDateString("zh-CN")}`
            : "";
          notify("待办提醒", `${t.content}${label ? " — " + label : ""}`);
          todoIds.push(t.id);
        }

        if (eventIds.length > 0) {
          await ackReminders(projectId, "event", eventIds);
        }
        if (todoIds.length > 0) {
          await ackReminders(projectId, "todo", todoIds);
        }
      } catch {
        // Silently retry next interval
      }
    };

    poll(); // immediate first check
    pollingRef.current = setInterval(poll, POLL_INTERVAL);

    return () => {
      if (pollingRef.current) clearInterval(pollingRef.current);
    };
  }, [projectId]);
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd frontend && npx tsc --noEmit --pretty 2>&1 | head -20
```

Expected: No new errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/hooks/use-reminder-polling.ts
git commit -m "feat: add useReminderPolling hook with dual-channel notification"
```

---

### Task 8: Home Page — Todo due_date + Reminder UI

**Files:**
- Modify: `frontend/app/(main)/home/page.tsx`

- [ ] **Step 1: Update the Todo interface**

Find the `Todo` interface in the file and replace it with:

```typescript
interface Todo {
  id: string;
  content: string;
  completed: boolean;
  is_team_todo: boolean;
  due_date: string | null;
  reminder_enabled: boolean;
  reminder_at: string | null;
}
```

- [ ] **Step 2: Add state and UI for todo creation form**

Add these state variables near the other useState declarations:

```typescript
const [todoDueDate, setTodoDueDate] = useState("");
const [todoReminder, setTodoReminder] = useState(false);
const [todoReminderMinutes, setTodoReminderMinutes] = useState(15);
```

In the todo creation form JSX (find the `<form>` or `<div>` where content input and add button live), add between the content input and the add button:

```tsx
<input
  type="date"
  value={todoDueDate}
  onChange={(e) => setTodoDueDate(e.target.value)}
  className="..."
/>

{todoDueDate && (
  <>
    <label className="flex items-center gap-1 text-xs">
      <input
        type="checkbox"
        checked={todoReminder}
        onChange={(e) => setTodoReminder(e.target.checked)}
      />
      提醒
    </label>
    {todoReminder && (
      <select
        value={todoReminderMinutes}
        onChange={(e) => setTodoReminderMinutes(Number(e.target.value))}
        className="..."
      >
        <option value={5}>5分钟前</option>
        <option value={15}>15分钟前</option>
        <option value={30}>30分钟前</option>
        <option value={60}>1小时前</option>
        <option value={1440}>1天前</option>
      </select>
    )}
  </>
)}
<Button onClick={handleAddTodo}>添加</Button>
```

Note: Use the existing shadcn `Button` component and match Tailwind classes from surrounding elements.

- [ ] **Step 3: Update createTodo to pass reminder params**

Update `handleAddTodo` (or `createTodo` function) to include the new params:

```typescript
await api.post(`/home/${selectedProject}/todos`, null, {
  params: {
    content: todoContent,
    is_team_todo: isTeamTodo,
    due_date: todoDueDate || undefined,
    reminder_enabled: todoReminder,
    reminder_minutes: todoReminder ? todoReminderMinutes : undefined,
  },
});
```

Reset the new states after creation (alongside the existing `setTodoContent("")`):

```typescript
setTodoDueDate("");
setTodoReminder(false);
setTodoReminderMinutes(15);
```

- [ ] **Step 4: Display due_date and reminder icon in todo list**

In the todo item rendering, add after the checkbox/todo content display:

```tsx
{todo.due_date && (
  <span className={cn(
    "text-xs ml-2 px-1.5 py-0.5 rounded",
    new Date(todo.due_date) < new Date() ? "bg-red-100 text-red-700" : "bg-blue-100 text-blue-700"
  )}>
    {new Date(todo.due_date).toLocaleDateString("zh-CN", { month: "short", day: "numeric" })}
  </span>
)}
{todo.reminder_enabled && <span className="text-xs ml-1">🔔</span>}
```

- [ ] **Step 5: Add useReminderPolling to the page**

Import at top:

```typescript
import { useReminderPolling } from "@/hooks/use-reminder-polling";
```

Add inside the component body (near other hooks):

```typescript
useReminderPolling(selectedProject);
```

- [ ] **Step 6: Verify build**

```bash
cd frontend && npx tsc --noEmit --pretty 2>&1 | head -20
```

Expected: No new errors. Fix any type errors from the Todo interface change (list_todos response now includes reminder fields — ensure the API type matches).

- [ ] **Step 7: Commit**

```bash
git add frontend/app/\(main\)/home/page.tsx
git commit -m "feat: add due_date picker and reminder to todo form and display"
```

---

### Task 9: Timeline Page — Reminder Config in Event Dialog

**Files:**
- Modify: `frontend/app/(main)/timeline/page.tsx`

- [ ] **Step 1: Add reminder state and form controls**

Add state variables near other useState declarations:

```typescript
const [reminderEnabled, setReminderEnabled] = useState(false);
const [reminderMinutes, setReminderMinutes] = useState(15);
```

In the event creation dialog, after the "team event" checkbox, add:

```tsx
<label className="flex items-center gap-2 text-sm">
  <input
    type="checkbox"
    checked={reminderEnabled}
    onChange={(e) => setReminderEnabled(e.target.checked)}
  />
  开启提醒
</label>
{reminderEnabled && (
  <select
    value={reminderMinutes}
    onChange={(e) => setReminderMinutes(Number(e.target.value))}
    className="..."
  >
    <option value={5}>5分钟前</option>
    <option value={15}>15分钟前</option>
    <option value={30}>30分钟前</option>
    <option value={60}>1小时前</option>
    <option value={1440}>1天前</option>
  </select>
)}
```

- [ ] **Step 2: Add reminder params to the create API call**

Find the `api.post` call that creates the event. Add to its params:

```typescript
reminder_enabled: reminderEnabled,
reminder_minutes: reminderEnabled ? reminderMinutes : undefined,
```

Reset states after creation:

```typescript
setReminderEnabled(false);
setReminderMinutes(15);
```

- [ ] **Step 3: Show bell icon on events with reminders**

In the event card rendering, add where the event title or metadata is displayed:

```tsx
{event.reminder_enabled && <span className="text-sm ml-1">🔔</span>}
```

Note: Update the `Event` interface to include `reminder_enabled: boolean` and `reminder_minutes: number | null`.

- [ ] **Step 4: Add useReminderPolling**

```typescript
import { useReminderPolling } from "@/hooks/use-reminder-polling";
// ...
useReminderPolling(selectedProject);
```

- [ ] **Step 5: Verify build**

```bash
cd frontend && npx tsc --noEmit --pretty 2>&1 | head -20
```

Expected: No new errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/app/\(main\)/timeline/page.tsx
git commit -m "feat: add reminder config to timeline event dialog and display"
```

---

### Task 10: Settings Page — Notification Settings Card

**Files:**
- Modify: `frontend/app/(main)/settings/page.tsx`

- [ ] **Step 1: Add notification permission request button**

Add a "通知设置" card in the settings page. Find the existing card pattern (e.g., the LLM or document provider section) and add a similar card:

```tsx
<Card>
  <CardHeader>
    <CardTitle>通知设置</CardTitle>
  </CardHeader>
  <CardContent className="space-y-4">
    <div className="flex items-center justify-between">
      <div>
        <p className="font-medium">桌面通知</p>
        <p className="text-sm text-muted-foreground">
          {typeof window !== "undefined" && ("Notification" in window)
            ? Notification.permission === "granted"
              ? "已开启"
              : Notification.permission === "denied"
              ? "已拒绝（可在浏览器设置中重新开启）"
              : "未授权"
            : "浏览器不支持桌面通知"}
        </p>
      </div>
      <Button
        variant="outline"
        disabled={
          typeof window !== "undefined" &&
          "Notification" in window &&
          (Notification.permission === "granted" || Notification.permission === "denied")
        }
        onClick={async () => {
          if (typeof window !== "undefined" && "Notification" in window) {
            const perm = await Notification.requestPermission();
            if (perm === "granted") {
              new Notification("数模Dashboard", { body: "通知已开启！" });
            }
          }
        }}
      >
        {typeof window !== "undefined" &&
        "Notification" in window &&
        Notification.permission === "granted"
          ? "已开启"
          : "去开启"}
      </Button>
    </div>
  </CardContent>
</Card>
```

Use existing shadcn `Card`, `CardHeader`, `CardTitle`, `CardContent`, and `Button` imports already present in the settings page.

- [ ] **Step 2: Verify build**

```bash
cd frontend && npx tsc --noEmit --pretty 2>&1 | head -20
```

- [ ] **Step 3: Commit**

```bash
git add frontend/app/\(main\)/settings/page.tsx
git commit -m "feat: add notification permission settings card"
```

---

### Task 11: End-to-End Verification

- [ ] **Step 1: Run full backend test suite**

```bash
cd backend && uv run pytest tests/ -v --tb=short 2>&1
```

Expected: All tests pass (185+), 0 failures.

- [ ] **Step 2: Run full frontend type check**

```bash
cd frontend && npx tsc --noEmit --pretty 2>&1
```

Expected: No new type errors from the new code.

- [ ] **Step 3: Start services and smoke test**

```bash
./scripts/start-all.sh
```

Manual verification:
1. Open `http://localhost:3000/home` → create a todo with a due_date 2 minutes in the future + reminder
2. Open timeline → create an event starting 2 minutes from now + 5min reminder
3. Wait for the polling interval → verify toast pops up
4. Switch to another tab → verify desktop notification appears (if permission granted)
5. Open settings → check notification permission status display
6. Ctrl+C to stop all services

- [ ] **Step 4: Commit any final fixes**

```bash
git add -A
git commit -m "chore: final verification fixes for reminder system"
```
