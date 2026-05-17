# Feishu IM Integration — Phase 1: Notification Push

## Overview

Phase 1 extends the existing browser-only reminder notification system to also deliver notifications via IM (Feishu/Lark). An `IMProvider` abstraction layer is introduced so future IM backends (Telegram, etc.) can be added as plugins. Notifications are fire-and-forget — they do not block the scheduler loop and failure of any single message does not affect others.

## Architecture

```
reminder_scheduler._check_reminders()
  → marks reminder_detected=True
  → asyncio.create_task(_dispatch_im_notifications())
    → for each IMProvider that is_configured():
      → look up IMUserBinding + IMProjectBinding
      → team event/todo: send to chat_id (group) + user_id (personal)
      → personal event/todo: send to user_id only

Frontend polling (unchanged)
  → toast / browser Notification API
```

IM sending is added as a parallel channel to browser notifications — not a replacement.

## Data Models

### IMUserBinding

| Column | Type | Description |
|--------|------|-------------|
| `id` | String(36) PK | UUID |
| `user_id` | FK → users.id, NOT NULL | Which user this binding belongs to |
| `provider_type` | String(50), NOT NULL | e.g. "feishu_cli" |
| `im_user_id` | String(255), NOT NULL | Feishu user_id or open_id for this user |
| `enabled` | Boolean, default=True | Whether notifications are enabled |
| `created_at` | DateTime, default=utcnow | |

Unique constraint: `(user_id, provider_type)` — one binding per user per IM type.

### IMProjectBinding

| Column | Type | Description |
|--------|------|-------------|
| `id` | String(36) PK | UUID |
| `project_id` | FK → projects.id, NOT NULL | Which project |
| `provider_type` | String(50), NOT NULL | e.g. "feishu_cli" |
| `im_chat_id` | String(255), NOT NULL | Feishu group chat_id |
| `enabled` | Boolean, default=True | Whether group notifications are enabled |
| `created_at` | DateTime, default=utcnow | |

Unique constraint: `(project_id, provider_type)` — one binding per project per IM type.

### Why two tables instead of reusing ProviderBinding

- ProviderBinding is designed for service credentials (access_token, api_key) stored as JSON blobs
- IM bindings have different semantics: user identity mapping vs chat target mapping
- Separate tables allow clean queries: "get all enabled user bindings for this project's team" and "get chat_id for this project"
- Clean separation makes adding Telegram/other IMs straightforward

## IMProvider Abstraction

### Interface

```python
class IMProvider(ABC):
    @abstractmethod
    async def send_message(self, recipient_type: str, recipient_id: str, title: str, body: str) -> bool:
        """Send a text message. recipient_type is 'user' or 'chat'."""

    @abstractmethod
    def get_provider_type(self) -> str:
        """Return 'feishu_cli' etc."""

    @abstractmethod
    def is_configured(self) -> bool:
        """Check prerequisites: CLI installed, authenticated."""
```

### Provider Registry

Mirrors the existing `_PROVIDER_REGISTRY` / `register_provider` / `get_provider` pattern from `document_provider.py`:

```python
_IM_PROVIDER_REGISTRY: dict[str, type[IMProvider]] = {}

def register_im_provider(provider_type: str, cls: type[IMProvider]): ...
def get_im_providers() -> list[IMProvider]: ...
```

`get_im_providers()` returns only providers where `is_configured()` is True, so uninstalled/unauthenticated providers are silently skipped.

### FeishuCLIProvider

- `is_configured()`: runs `which lark-cli` + `lark-cli auth status`, gate with `shutil.which`
- `send_message()`: `asyncio.create_subprocess_exec` with 10s timeout:

```
lark-cli messenger send --recipient-type <user|chat> --recipient-id <id> --text "<title>\n\n<body>"
```

- Message format (plain text, Phase 1):

```
📅 日程提醒
Team Meeting
开始时间: 2026-05-20 14:00
```

```
✅ 待办提醒
Finish sensitivity analysis
截止时间: 2026-05-21
```

## Scheduler Integration

### `_dispatch_im_notifications(db, events, todos)` (new function in `reminder_scheduler.py`)

Logic:

1. Query `IMUserBinding` (all enabled, joined with users who have bindings)
2. Query `IMProjectBinding` (all enabled, for the relevant project_ids from detected events/todos)
3. Build a dispatch map: `{project_id: {chat_id, user_ids: [...]}}`
4. For each detected event:
   - Get project's chat_id and relevant user_ids
   - If `is_team_event`: send to chat_id AND all bound user_ids
   - If not team event: send only to the event creator's user_id
5. For each detected todo: same logic using `is_team_todo`
6. Each message is sent independently with try/catch — one failure does not block others
7. Fire-and-forget: called via `asyncio.create_task()`, does not block the scheduler loop

### Edge cases

| Scenario | Behavior |
|----------|----------|
| Project has no IMProjectBinding | Skip group message, still send personal |
| User has no IMUserBinding | Skip personal message, still send group for team events |
| CLI not installed / not logged in | `is_configured()` returns False, provider skipped entirely |
| lark-cli subprocess timeout (10s) | Log warning, continue to next message |
| lark-cli returns non-zero exit | Log error with stderr, continue |
| Multiple IM providers configured | Each provider gets its own set of messages |

## API Endpoints

Prefix: `/api/im`

All endpoints require authentication. Project-binding endpoints require team membership.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/status` | Returns `{providers: [{type, configured, name}]}` for each registered IM provider |
| `GET` | `/user-binding` | Returns current user's `{binding: {provider_type, im_user_id, enabled} \| null}` |
| `POST` | `/user-binding` | Body: `{provider_type, im_user_id, enabled}`. Upserts user binding. |
| `GET` | `/project-binding/{project_id}` | Returns `{binding: {provider_type, im_chat_id, enabled} \| null}` |
| `POST` | `/project-binding/{project_id}` | Body: `{provider_type, im_chat_id, enabled}`. Upserts project binding. |
| `POST` | `/verify` | Body: `{provider_type, recipient_type, recipient_id}`. Sends a test message "mmdash IM notification test". Returns `{success, error?}`. |

### Duplicate prevention

User-binding upsert uses unique constraint `(user_id, provider_type)` — `INSERT ... ON CONFLICT UPDATE` semantics via SQLAlchemy merge.
Project-binding upsert uses `(project_id, provider_type)`.

## Frontend

### Settings Page — New "IM通知" Tab

Add a fifth tab to the settings page:

```
Tab: IM通知
Icon: Bell (or MessageSquare)

Content:
┌─ IM 通知设置 ─────────────────────────────────────┐
│  [飞书状态: CLI 已安装 ✓ | 已登录 ✓]              │
│                                                    │
│  个人飞书绑定                                       │
│  ├ 飞书用户 ID: [input] [验证]                      │
│  └ ☑ 启用飞书通知                                   │
│                                                    │
│  项目群绑定                                         │
│  ├ 项目: [select]                                   │
│  ├ 飞书群 Chat ID: [input]                         │
│  ├ ☑ 启用群通知                                     │
│  └ [保存]                                          │
│                                                    │
│  已绑定的项目群:                                     │
│  ┌─ 项目A: chat_xxx ☑ ──────────── [删除] ┐       │
│  └─ 项目B: chat_yyy ☑ ──────────── [删除] ┘       │
└────────────────────────────────────────────────────┘
```

### API Client

Add to `frontend/lib/api.ts`:

```typescript
export const imApi = {
  async getStatus(): Promise<{providers: {type: string, configured: boolean, name: string}[]}>,
  async getUserBinding(): Promise<{binding: ... | null}>,
  async saveUserBinding(data: {provider_type, im_user_id, enabled}): Promise<...>,
  async getProjectBinding(projectId: string): Promise<{binding: ... | null}>,
  async saveProjectBinding(projectId: string, data: {provider_type, im_chat_id, enabled}): Promise<...>,
  async verify(data: {provider_type, recipient_type, recipient_id}): Promise<{success, error?}>,
};
```

### Notification Permission (IM layer)

IM通知 tab has its own state panel. On mount, it calls `GET /api/im/status` to check which providers are available and configured. CLI installation/auth status is displayed as badges.

## Setup Script Changes

In `scripts/setup.sh`, add an optional step (non-blocking if skipped):

```bash
# Feishu CLI (optional — for IM notifications)
if ! command -v lark-cli &>/dev/null; then
    echo "→ 安装飞书 CLI (可选，用于 IM 通知)..."
    npm install -g @larksuite/cli || echo "  飞书 CLI 安装失败，可稍后手动安装"
fi
if command -v lark-cli &>/dev/null; then
    echo "→ 初始化飞书 CLI..."
    lark-cli config init 2>/dev/null || true
    echo "→ 请手动运行 'lark-cli auth login --recommend' 完成飞书登录"
fi
```

## Message Format (Phase 1)

Plain text only. No interactive cards, no rich formatting. Pattern:

```
📅 日程提醒
{event.title}
开始时间: {event.start_time}
描述: {event.description or "无"}
```

```
✅ 待办提醒
{todo.content}
截止时间: {todo.due_date or "无"}
```

## Error Handling

- IM send timeout (10s): log warning, skip to next message
- CLI not found: provider marked as not configured, logged at startup
- Auth expired: `lark-cli auth status` returns non-zero → provider skipped until re-authenticated
- Subprocess stderr captured and logged
- IM dispatch runs in a separate asyncio task; failure does not affect the scheduler loop
- Each individual message is in its own try/catch; one failed send does not block others

## Testing

- Unit: `test_feishu_cli_provider.py` — mock subprocess, test `is_configured`, `send_message` success/failure/timeout
- Unit: `test_im_dispatch.py` — test dispatch logic with various binding configurations (no binding, user only, project only, both)
- Integration: `test_im_api.py` — test CRUD endpoints for user-binding and project-binding
- No integration tests that call real lark-cli (requires auth)

## Out of Scope (Phase 2+)

- Interactive Feishu cards with approve/reject buttons (human-in-the-loop review)
- Bot that responds to @mentions (query project status, create tasks)
- Calendar sync between mmdash TimelineEvent and Feishu Calendar
- Telegram / other IM providers
- Rich text / Markdown formatting in IM messages
- lark-cli auth login automation (manual step for now)
