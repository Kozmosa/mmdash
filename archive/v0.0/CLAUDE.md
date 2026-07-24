# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

数模Dashboard is a collaborative platform for mathematical modeling competitions. It features a Next.js frontend, FastAPI backend, Cloud Agent (context/memory), Local Agent (WebSocket-based local execution), and a Doc Server (document provider abstraction). Services communicate via HTTP and WebSocket.

## Common Commands

### Setup (first time)
```bash
./scripts/setup.sh
```

### Start all services
```bash
./scripts/start-all.sh        # Redis + Backend + CloudAgent + DocServer + LocalAgent + Frontend
```

### Start services individually
```bash
# Redis
./scripts/start-redis.sh

# Backend (runs alembic upgrade head on startup via start-all.sh)
cd backend && uv run uvicorn app.main:app --reload --port 8000

# Cloud Agent
cd cloud_agent && uv run python main.py

# Local Agent (WebSocket server)
cd local_agent && uv run python main.py

# Doc Server
cd doc_server && uv run uvicorn doc_server.main:app --port 8002

# Frontend
cd frontend && npm run dev
```

### Backend
```bash
cd backend
uv run pytest                    # Run all tests
uv run pytest tests/integration/test_auth.py  # Run single test file
uv run alembic upgrade head      # Run migrations
uv run alembic revision --autogenerate -m "msg"  # Create migration
```

### Frontend
```bash
cd frontend
npm run dev
npm run lint
npm run build
npm run analyze                  # Bundle analysis
```

## Service Architecture

```
Frontend (Next.js)  →  Backend (FastAPI)  →  Cloud Agent (FastAPI)
  localhost:3000        localhost:8000        localhost:8001
                          ↓
                    Doc Server (FastAPI)
                      localhost:8002
                          ↓
                    Local Agent (WebSocket)
                      ws://127.0.0.1:8765
```

**Backend** (`backend/`) — FastAPI service handling auth, teams, projects, todos, timeline, model snapshots, and git integration. Uses SQLAlchemy 2.0 with SQLite. Alembic migrations in `backend/migrations/`. Redis is used for caching.

**Cloud Agent** (`cloud_agent/`) — FastAPI service maintaining in-memory project context vectors. Receives project state summaries from the backend.

**Local Agent** (`local_agent/`) — Python asyncio WebSocket server that connects directly to the browser. Handles local environment detection, shell execution, and automated experiment runs.

**Doc Server** (`doc_server/`) — FastAPI service that serves as a local document provider backend, separate from the main backend.

## Key Architectural Patterns

### Document Provider Abstraction
The backend uses a pluggable document provider system (`app/services/document_provider.py`). Providers implement `DocumentProvider` and register themselves via `@register_provider`. The registry is `_PROVIDER_REGISTRY`. Currently implemented:
- `NotionProvider` (`app/services/notion_provider.py`) — Notion API via OAuth
- `LocalFileProvider` (`app/services/local_file_provider.py`) — Local file system

Providers are auto-imported in `app/main.py` to trigger registration. The active provider is controlled by `DOCUMENT_PROVIDER` env var (`"notion"` or `"local_file"`).

### Unified Provider Binding
`ProviderBinding` (table `provider_bindings`) replaced the legacy `NotionBinding`. On startup, `app/main.py` runs a one-time migration that copies `notion_bindings` data into `provider_bindings` with `provider_type="notion"` and credentials serialized as JSON.

### Backend Test Setup
Tests use an in-memory SQLite database with `TestClient`. The test engine and session are monkey-patched onto `app.database` at import time in `tests/conftest.py`. Fixtures provide `client`, `auth_client`, `test_user`, `team`, `project`, and `provider_binding`.

**Important:** Because `app.database` module globals are patched during import, any code that captures `engine` or `SessionLocal` at module level will use the test versions. Database-dependent code should import from `app.database` rather than caching references.

### Frontend Structure
- Uses Next.js App Router with route groups: `app/(main)/` for authenticated pages, `app/auth/` for login/register
- Path alias `@/*` maps to the `frontend/` root
- shadcn/ui components in `components/ui/` (style: new-york, icon library: lucide)
- Tailwind CSS v4 with CSS variables
- Zustand stores in `stores/` (`auth.ts`, `data-cache.ts`)
- API client in `lib/api.ts` — axios instance with JWT Bearer token from `localStorage`, redirects to `/auth/login` on 401
- Local Agent client in `lib/local_agent.ts` — WebSocket connection wrapper

### Authentication Flow
- Backend uses JWT (HS256) with `python-jose`. Tokens expire in 7 days.
- Login endpoint: `POST /api/auth/login` (form data: username=email, password)
- Frontend stores token in `localStorage` as `"token"`, sends as `Authorization: Bearer <token>`
- Auth context is managed by Zustand store in `stores/auth.ts`

## Environment & Configuration

Backend settings are loaded from `.env` via Pydantic Settings (`app/core/config.py`):
- `DATABASE_URL` — defaults to `sqlite:///./mmdash.db`
- `REDIS_URL` — defaults to `redis://localhost:6379/0`
- `SECRET_KEY`, `ALGORITHM`, `ACCESS_TOKEN_EXPIRE_MINUTES` — JWT config
- `NOTION_CLIENT_ID`, `NOTION_CLIENT_SECRET`, `NOTION_REDIRECT_URI` — Notion OAuth
- `OPENAI_API_KEY` — LLM features
- `DOCUMENT_PROVIDER` — `"notion"` or `"local_file"`
- `DOC_SERVER_URL`, `DOC_SERVER_API_KEY` — Doc Server connection

## Database Models

Key tables: `users`, `teams`, `team_members`, `projects`, `todos`, `timeline_events`, `problem_files`, `model_snapshots`, `provider_bindings` (replaces `notion_bindings`).

All models use UUID strings as primary keys (`generate_uuid()`). Relationships use SQLAlchemy 2.0 style with `relationship()` and back_populates.

## API Routers

Backend routers in `app/api/`: `auth`, `teams`, `projects`, `home`, `timeline`, `model`, `model_version`, `git`. All prefixed with `/api/...` except the `/health` endpoint.
