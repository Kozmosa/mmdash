# Reminder & Notification System Design

## Overview

Add deadline reminder and browser notification support for TimelineEvent and Todo. Users receive reminders via in-page Sonner toast while the page is in the foreground, and via the browser Notification API when the page is in the background.

## Data Model Changes

### TimelineEvent (new columns)

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| `reminder_enabled` | Boolean | `False` | Whether reminder is active for this event |
| `reminder_minutes` | Integer | nullable | Minutes before `start_time` to fire the reminder |
| `reminder_detected` | Boolean | `False` | Backend has detected the reminder window |
| `reminder_acked` | Boolean | `False` | Frontend has displayed the notification |

The effective reminder time is computed as `start_time - interval 'reminder_minutes minutes'` at query time rather than stored.

### Todo (new columns)

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| `reminder_enabled` | Boolean | `False` | Whether reminder is active for this todo |
| `reminder_at` | DateTime | nullable | Absolute reminder time, set when `due_date` is chosen with a reminder |
| `reminder_detected` | Boolean | `False` | Backend has detected the reminder window |
| `reminder_acked` | Boolean | `False` | Frontend has displayed the notification |

Unlike TimelineEvent, Todo stores `reminder_at` as an absolute timestamp because `due_date` is optional and may be cleared independently.

### Alembic migration

One migration adding columns to both tables with `ALTER TABLE`. All new columns are nullable or have defaults, so migration is safe on existing data.

## Backend

### Reminder Scheduler (`backend/app/services/reminder_scheduler.py`)

- Reuses the Zotero sync loop pattern: `asyncio.Event.wait(timeout=30)` in a `while True` loop
- Started on FastAPI startup, stopped on shutdown
- Each tick queries for events/todos where:
  - `reminder_enabled == True`
  - `reminder_detected == False`
  - Computed reminder time falls within `[now, now + 35s]`
- Sets `reminder_detected = True` for matched records

Detection query logic:

- **TimelineEvent**: `start_time - (reminder_minutes || 0) * INTERVAL '1 minute'` BETWEEN now and now+35s
- **Todo**: `reminder_at` BETWEEN now and now+35s

SQL is expressed in Python/SQLAlchemy, supporting both SQLite (dev) and PostgreSQL (future).

### API Endpoints (`backend/app/api/reminders.py`)

Prefix: `/api/reminders`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/{project_id}/pending` | Returns `{events: [...], todos: [...]}` where `reminder_detected=True AND reminder_acked=False` for the project |
| `POST` | `/{project_id}/ack` | Body: `{type: "event"|"todo", ids: [...]}`. Sets `reminder_acked=True` for the given ids. Idempotent. |

All endpoints require auth, team membership, and project access.

## Frontend

### Notification Channel Strategy

```
Page visibility check:
  document.visibilityState === "visible"
    → Sonner toast
  document.visibilityState === "hidden"
    → new Notification(title, {body, icon})   (if permission granted)
```

- No Service Worker required. Uses the simpler Notification API.
- HTTPS not required for localhost development.

### Permission Flow

- On Timeline and Home page mount, check `Notification.permission`
- If `"default"`: show a subtle banner "开启桌面通知以接收截止日期提醒" with an [开启] button
- Button click → `Notification.requestPermission()` → update banner state
- If `"denied"`: banner hides permanently (permission remembered by browser)
- Permission state stored in `localStorage` as cache, re-checked from browser API on each page load

### Polling

- Timeline and Home pages start a 30-second polling interval on mount
- Calls `GET /api/reminders/{project_id}/pending`
- For each returned item, fires notification (toast or desktop) then calls `POST /api/reminders/{project_id}/ack`
- Ack is idempotent — if two tabs poll simultaneously, only the first ack succeeds logically; the second sees already-acked items filtered out

### Timeline Page Changes

**Event creation dialog** — add below the "team event" checkbox:

- `reminder_enabled`: checkbox "开启提醒"
- `reminder_minutes`: dropdown `[5分钟, 15分钟, 30分钟, 1小时, 1天]`, visible only when reminder_enabled is checked

**Event list cards** — show a bell icon (🔔) for events with `reminder_enabled=True`.

**Event detail** — show reminder configuration inline on the event card.

### Home Page Changes

**Todo interface** — add two fields to the `Todo` TypeScript interface:

```typescript
interface Todo {
  id: string;
  content: string;
  completed: boolean;
  is_team_todo: boolean;
  due_date: string | null;        // NEW — was already in API response
  reminder_enabled: boolean;       // NEW
  reminder_at: string | null;      // NEW
}
```

**Todo creation form** — add:

- `due_date`: lightweight date picker (no time component needed)
- `reminder_enabled` + reminder dropdown: visible only when `due_date` is set

**Todo list display** — show:

- `due_date` label on todo items (e.g., "5月20日" or "明天"), color-coded: green (far), orange (this week), red (overdue or today)
- Bell icon for items with `reminder_enabled=True`

### Settings Page Changes

New "通知设置" card at the bottom of the settings page (before the document provider section):

- Desktop notification permission status with [去开启] / [测试通知] button
- Default reminder lead time dropdown (stored in `localStorage`, used as preset for new events/todos)

### API Client (`frontend/lib/api.ts`)

Add:

```typescript
export async function getPendingReminders(projectId: string) { ... }
export async function ackReminders(projectId: string, type: string, ids: string[]) { ... }
```

## Data Flow

```
Time passes
  → ReminderScheduler.run() detects event/todo in window
  → Sets reminder_detected=True
  → Frontend 30s poll: GET /pending
  → Returns items with detected=True, acked=False
  → Frontend checks visibilityState
  → Fires toast or Notification
  → Frontend calls POST /ack
  → Sets reminder_acked=True
```

## Error Handling

- **Notification permission denied**: degrade to toast-only, no error shown
- **Polling fails**: silently retry next interval, no user-facing error
- **Scheduler DB error**: log exception, continue next tick
- **Double notification**: prevented by `reminder_acked` — second tab's poll returns empty list after first tab acks

## Testing

- Backend: unit test for `_check_reminders()` with in-memory SQLite, test both event and todo detection
- Backend: integration tests for `/pending` and `/ack` endpoints
- Frontend: verify polling hook behavior, notification permission flow, form field rendering

## Out of Scope (deferred to future iterations)

- Service Worker / Web Push API (offline notifications)
- Email notifications
- Recurring events (daily/weekly reminders)
- Calendar/Gantt visualization of timeline
- WebSocket push for instant delivery (acceptable delay: up to 30s)
