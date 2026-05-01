# mmdash — Documosa Integration Specification

**Author**: mmdash development (self)
**Date**: 2026-05-01
**Status**: Draft

---

## 1. Overview

This spec defines all changes required on the **mmdash** side to integrate documosa as a document provider backend. Documosa runs as an independent service; mmdash communicates with it via REST API over HTTP.

### 1.1 Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              User Browser                                │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────────────────┐   │
│  │ mmdash FE   │────▶│ mmdash BE   │────▶│ documosa                │   │
│  │ (Next.js)   │JWT  │ (FastAPI)   │JWT  │ /api/mmdash/*           │   │
│  └─────────────┘     └─────────────┘     └─────────────────────────┘   │
│       │                    │                                              │
│       │                    │ (forwards user's JWT in Authorization)      │
│       │                    ▼                                              │
│       │             ┌─────────────┐                                       │
│       │             │ Local Agent │                                       │
│       │             │ (WebSocket) │                                       │
│       │             └─────────────┘                                       │
│       │                                                                   │
│       └────────────▶ documosa native UI (optional, direct access)        │
│                      /api/documents/*                                     │
└─────────────────────────────────────────────────────────────────────────┘
```

### 1.2 ID Mapping

| mmdash Entity | Documosa Entity | Field |
|---|---|---|
| `Project.id` | n/a | UUID string |
| `Project.model_data_page_id` | `Document.id` | Direct 1:1 mapping |

When a user creates a new model document in mmdash:
1. mmdash BE calls `POST {documosa}/api/mmdash/documents`.
2. documosa returns `{page_id: "doc-uuid"}`.
3. mmdash stores `doc-uuid` in `Project.model_data_page_id`.

---

## 2. Authentication Flow

### 2.1 Shared JWT Secret

Both mmdash and documosa must be configured with the same `SECRET_KEY`:
- mmdash: `app/core/config.py` → `settings.SECRET_KEY`
- documosa: env var `JWT_SECRET`

### 2.2 Token Flow

```
User login ──▶ mmdash BE issues JWT ──▶ mmdash FE stores in localStorage
                                              │
                                              ▼
                                    FE calls mmdash BE APIs
                                    (Authorization: Bearer <token>)
                                              │
                                              ▼
                                    mmdash BE calls documosa APIs
                                    (forwards same Authorization header)
```

### 2.3 Identity Mapping in Documosa

Documosa extracts from JWT payload:
- `client_id` = `"mmdash-{sub}"`
- `nickname` = `name || email || "mmdash-user"`
- `role_mode` = `Writer`

This identity is used in documosa's audit log and real-time collaboration (if user also opens documosa native UI).

---

## 3. DocumentProvider Abstraction Changes

### 3.1 Current State

```python
# backend/app/services/document_provider.py
class DocumentProvider(ABC):
    @abstractmethod
    async def fetch_page_content(self, page_id: str, credentials: dict) -> dict:
        """Returns {page_id, blocks, title}"""

    @abstractmethod
    async def fetch_page_metadata(self, page_id: str, credentials: dict) -> dict:
        """Returns {page_id, title}"""

    @abstractmethod
    def get_provider_type(self) -> str: ...
```

### 3.2 Extended Interface

```python
# backend/app/services/document_provider.py
class DocumentProvider(ABC):
    # ── Existing read operations ──
    @abstractmethod
    async def fetch_page_content(self, page_id: str, credentials: dict) -> dict: ...

    @abstractmethod
    async def fetch_page_metadata(self, page_id: str, credentials: dict) -> dict: ...

    @abstractmethod
    def get_provider_type(self) -> str: ...

    def get_auth_url(self) -> Optional[str]: ...
    async def exchange_auth_code(self, code: str) -> dict: ...

    # ── New write operations ──
    async def create_page(self, title: str, content: str, credentials: dict) -> dict:
        """Create a new document.

        Args:
            title: Document title.
            content: Initial Markdown content (optional, may be empty).
            credentials: Provider-specific credentials dict.

        Returns:
            {"page_id": str, "title": str}

        Raises:
            NotImplementedError: if provider does not support creation.
        """
        raise NotImplementedError("Create not supported by this provider")

    async def update_page_content(
        self,
        page_id: str,
        content: dict,
        credentials: dict,
    ) -> dict:
        """Update document content (full replacement).

        Args:
            page_id: Document identifier.
            content: Dict with optional keys:
                - "title": str (optional)
                - "markdown": str (mutually exclusive with "blocks")
                - "blocks": list[dict] (mutually exclusive with "markdown")
            credentials: Provider-specific credentials dict.

        Returns:
            {"page_id": str, "title": str, "blocks": list, "markdown": str}

        Raises:
            NotImplementedError: if provider does not support updates.
        """
        raise NotImplementedError("Update not supported by this provider")
```

**Design rationale**:
- Write methods are **not abstract** — existing providers (Notion, local_file) can opt-in when ready.
- `create_page` uses Markdown string as input because mmdash's editor will work in Markdown.
- `update_page_content` accepts both `markdown` and `blocks` for flexibility; the provider picks whichever it prefers.

### 3.3 ProviderBinding Update

No schema change needed. For documosa provider, `credentials` stores:
```json
{"base_url": "http://localhost:3000"}
```

No `api_key` needed since auth is JWT-based.

---

## 4. DocumosaProvider Implementation

### 4.1 File

`backend/app/services/documosa_provider.py`

### 4.2 Class Skeleton

```python
import httpx
from app.core.config import get_settings
from app.services.document_provider import DocumentProvider, register_provider

settings = get_settings()


class DocumosaProvider(DocumentProvider):
    """Documosa document provider."""

    def get_provider_type(self) -> str:
        return "documosa"

    async def fetch_page_content(self, page_id: str, credentials: dict) -> dict:
        base_url = credentials["base_url"]
        token = _get_current_user_token()  # from request context
        async with httpx.AsyncClient() as client:
            resp = await client.get(
                f"{base_url}/api/mmdash/documents/{page_id}/content",
                headers={"Authorization": f"Bearer {token}"},
                timeout=30.0,
            )
            resp.raise_for_status()
            data = resp.json()
        return {
            "page_id": data["page_id"],
            "title": data.get("title", ""),
            "blocks": data.get("blocks", []),
        }

    async def fetch_page_metadata(self, page_id: str, credentials: dict) -> dict:
        base_url = credentials["base_url"]
        token = _get_current_user_token()
        async with httpx.AsyncClient() as client:
            resp = await client.get(
                f"{base_url}/api/mmdash/documents/{page_id}",
                headers={"Authorization": f"Bearer {token}"},
                timeout=10.0,
            )
            resp.raise_for_status()
            data = resp.json()
        return {
            "page_id": data["page_id"],
            "title": data.get("title", ""),
        }

    async def create_page(self, title: str, content: str, credentials: dict) -> dict:
        base_url = credentials["base_url"]
        token = _get_current_user_token()
        async with httpx.AsyncClient() as client:
            resp = await client.post(
                f"{base_url}/api/mmdash/documents",
                headers={"Authorization": f"Bearer {token}"},
                json={"title": title, "content": content},
                timeout=10.0,
            )
            resp.raise_for_status()
            data = resp.json()
        return {"page_id": data["page_id"], "title": data["title"]}

    async def update_page_content(
        self, page_id: str, content: dict, credentials: dict
    ) -> dict:
        base_url = credentials["base_url"]
        token = _get_current_user_token()
        payload = {}
        if "title" in content:
            payload["title"] = content["title"]
        if "blocks" in content:
            payload["blocks"] = content["blocks"]
        elif "markdown" in content:
            payload["markdown"] = content["markdown"]

        async with httpx.AsyncClient() as client:
            resp = await client.put(
                f"{base_url}/api/mmdash/documents/{page_id}/content",
                headers={"Authorization": f"Bearer {token}"},
                json=payload,
                timeout=30.0,
            )
            resp.raise_for_status()
            data = resp.json()
        return {
            "page_id": data["page_id"],
            "title": data["title"],
            "blocks": data.get("blocks", []),
            "markdown": data.get("markdown", ""),
        }


register_provider("documosa", DocumosaProvider)
```

### 4.3 Token Extraction from Request Context

Since `DocumentProvider` methods are called from FastAPI request handlers, we need a way to extract the current user's JWT token.

**Approach A** (recommended): Pass token explicitly via credentials.

Modify the calling code in `model.py` to extract the token from the request and inject it into credentials before calling provider methods:

```python
# In model.py route handlers
credentials = json.loads(binding.credentials)
credentials["_token"] = extract_bearer_token(request)  # helper
provider = get_provider(binding.provider_type)
content = await provider.fetch_page_content(page_id, credentials)
```

Then `_get_current_user_token()` reads `credentials["_token"]`.

**Approach B**: Use `contextvars` to store the token in request-scoped context.

Less explicit, harder to test. Approach A is preferred.

### 4.4 Error Translation

Documosa error → mmdash HTTPException mapping:

| Documosa Status | mmdash Status | Detail |
|---|---|---|
| 401 | 401 | "Documosa authentication failed" |
| 404 | 404 | "Document not found in documosa" |
| 409 | 409 | "Document was modified by another user" |
| 500 | 500 | "Documosa internal error" |
| timeout | 504 | "Documosa connection timeout" |

---

## 5. Backend API Changes

### 5.1 New Endpoints in `backend/app/api/model.py`

#### 5.1.1 POST `/{project_id}/content`

Update model document content.

**Request body**:
```json
{
  "title": "Optional new title",
  "markdown": "# Updated content\n\nNew paragraph."
}
```

**Authorization**: Bearer token (same as all other endpoints).

**Flow**:
1. Verify user is team member.
2. Check `project.model_data_page_id` is set. If not, return `400`.
3. Get `ProviderBinding`, instantiate provider.
4. Call `provider.update_page_content(page_id, content, credentials)`.
5. Invalidate cache (`set_cached_page` with new content, or delete cache entry).
6. Return updated content.

**Response 200**:
```json
{
  "page_id": "...",
  "title": "Updated Title",
  "markdown": "...",
  "blocks": [...]
}
```

#### 5.1.2 POST `/{project_id}/create-page`

Create a new documosa document and bind it to the project.

**Request body**:
```json
{
  "title": "New Model Document"
}
```

**Flow**:
1. Verify user is team member.
2. Get `ProviderBinding`, verify provider supports `create_page`.
3. Call `provider.create_page(title, "", credentials)`.
4. Set `project.model_data_page_id = result["page_id"]`.
5. Commit DB.
6. Return `{status: "created", page_id: "..."}`.

**Response 201**:
```json
{
  "status": "created",
  "page_id": "660e8400-e29b-41d4-a716-446655440001",
  "title": "New Model Document"
}
```

#### 5.1.3 GET `/{project_id}/content` (existing)

No signature change. Internally, add cache invalidation logic after `update_page_content` calls. Also forward user's JWT token in credentials.

### 5.2 Helper: Extract Bearer Token from Request

```python
# backend/app/api/model.py or app/services/utils.py
from fastapi import Request

def extract_bearer_token(request: Request) -> str:
    auth = request.headers.get("Authorization", "")
    if auth.lower().startswith("bearer "):
        return auth[7:]
    return ""
```

---

## 6. Frontend Changes

### 6.1 Model Page (`frontend/app/(main)/model/page.tsx`)

#### 6.1.1 Content Tab — Add Editor

Current state: content tab is read-only, shows rendered Markdown via `<MarkdownRenderer>`.

**New state**:
- Content tab has two modes: **View** (rendered) and **Edit** (textarea/Monaco).
- Toggle button switches between modes.
- In Edit mode: textarea with raw Markdown, "Save" and "Cancel" buttons.
- When no `model_data_page_id` is bound: show "Create Document" button instead of empty state.

**State additions**:
```typescript
const [editMode, setEditMode] = useState(false);
const [editContent, setEditContent] = useState("");
```

**Save flow**:
```typescript
const saveContent = async () => {
  if (!selectedProject || !editContent) return;
  try {
    await api.post(`/model/${selectedProject}/content`, {
      markdown: editContent,
    });
    setMarkdown(editContent);
    setEditMode(false);
    toast.success("保存成功");
  } catch (err: any) {
    toast.error(err.response?.data?.detail || "保存失败");
  }
};
```

**Create document flow**:
```typescript
const createDocument = async () => {
  if (!selectedProject) return;
  try {
    const res = await api.post(`/model/${selectedProject}/create-page`, {
      title: `${selectedProjectName} 模型`,
    });
    fetchProjects(selectedTeam); // refresh to show "已绑定"
    toast.success("文档已创建");
  } catch (err: any) {
    toast.error(err.response?.data?.detail || "创建失败");
  }
};
```

#### 6.1.2 Binding Card Update

Current "绑定文档页面" card has an input for `page_id` and a bind button.

**New behavior**:
- If project already has `model_data_page_id`: show page ID and link to documosa native UI (`{documosa_url}/documents/{page_id}`).
- If not bound: show input for manual binding (existing) + "自动创建文档" button (new).

#### 6.1.3 Export Button

Export Markdown now calls existing `GET /model/{id}/export/md` — no change needed, but verify it works with documosa content.

### 6.2 Provider Selection UI

In the binding/settings flow, user selects provider type from a dropdown:
- Notion
- 本地文件 (local_file)
- Documosa (new)

For Documosa, the binding form only needs:
- `base_url`: text input, default `http://localhost:3000`
- No OAuth flow, no API key — JWT is automatic.

The binding is stored in `ProviderBinding` with `provider_type="documosa"`, `credentials={"base_url": "..."}`.

---

## 7. Data Flow Examples

### 7.1 User Opens Model Page (Read)

```
User ──▶ FE: GET /model/{project_id}/content
           FE ──▶ BE: GET /model/{project_id}/content (with JWT)
                      BE ──▶ DB: get project.model_data_page_id
                      BE ──▶ Cache: check cached page
                      miss ──▶ BE ──▶ Documosa: GET /api/mmdash/documents/{id}/content
                                              (Authorization: Bearer <JWT>)
                                              Documosa ──▶ DB: get lines
                                              Documosa ──▶ lines → markdown → blocks
                                              Documosa ──▶ BE: {page_id, title, blocks, markdown}
                      BE ──▶ Cache: store
                      BE ──▶ FE: {page_id, markdown, blocks}
           FE ──▶ render MarkdownRenderer
```

### 7.2 User Edits and Saves (Write)

```
User ──▶ FE: click Save
           FE ──▶ BE: POST /model/{project_id}/content
                      body: {markdown: "# New..."}
                      BE ──▶ DB: get binding (provider_type="documosa")
                      BE ──▶ Documosa: PUT /api/mmdash/documents/{id}/content
                                              body: {markdown: "# New..."}
                                              Documosa ──▶ optimistic lock check
                                              Documosa ──▶ replace lines
                                              Documosa ──▶ audit log
                                              Documosa ──▶ BE: updated snapshot
                      BE ──▶ Cache: invalidate
                      BE ──▶ FE: {page_id, title, blocks, markdown}
           FE ──▶ show success toast, switch to View mode
```

### 7.3 User Creates New Document

```
User ──▶ FE: click "创建文档"
           FE ──▶ BE: POST /model/{project_id}/create-page
                      body: {title: "..."}
                      BE ──▶ Documosa: POST /api/mmdash/documents
                                              body: {title: "...", content: ""}
                                              Documosa ──▶ create doc with empty lines
                                              Documosa ──▶ BE: {page_id, title}
                      BE ──▶ DB: project.model_data_page_id = page_id
                      BE ──▶ FE: {status: "created", page_id, title}
           FE ──▶ refresh project list, show "已绑定" badge
```

---

## 8. Error Handling & Edge Cases

| Scenario | Handling |
|---|---|
| User has no ProviderBinding | Return `400` "请先绑定文档源" |
| Provider type is not "documosa" | `create_page` raises `NotImplementedError` → `400` "当前文档源不支持创建文档" |
| Documosa is unreachable | `504` "无法连接到文档服务" |
| Documosa returns 409 conflict | Forward to FE, show "文档已被其他用户修改，请刷新后重试" |
| JWT expired during documosa call | `401` → FE redirects to login |
| Project has no `model_data_page_id` on save | `400` "请先绑定或创建文档" |
| User saves empty content | Allowed — creates empty documosa document |

---

## 9. Configuration

### 9.1 Environment Variables

No new env vars needed on mmdash side. Existing ones sufficient:
- `SECRET_KEY` — must be shared with documosa.

### 9.2 Documosa Binding Settings

Stored in `ProviderBinding.credentials` (JSON):
```json
{"base_url": "http://localhost:3000"}
```

No `api_key` field.

---

## 10. Testing Plan

### 10.1 Backend Tests

- Test `DocumosaProvider.fetch_page_content` with mocked HTTP responses.
- Test `DocumosaProvider.create_page` with mocked HTTP responses.
- Test `DocumosaProvider.update_page_content` with mocked HTTP responses.
- Test token forwarding in `model.py` route handlers.
- Test 409 conflict handling.

### 10.2 Frontend Tests

- Test edit mode toggle in ModelPage.
- Test save flow with mocked API.
- Test create-document flow.
- Test "请先绑定文档" error state.

### 10.3 Integration Tests (manual)

1. Start documosa with `JWT_SECRET` matching mmdash.
2. In mmdash, bind documosa provider.
3. Create a project → create model document.
4. Edit content in mmdash → verify in documosa native UI.
5. Edit content in documosa native UI → refresh in mmdash → verify sync.

---

## 11. Rollout Plan

| Phase | Scope | Files |
|---|---|---|
| 1 | Documosa JWT auth + adapter layer | documosa repo (see requirements doc) |
| 2 | mmdash DocumentProvider extension + DocumosaProvider | `document_provider.py`, `documosa_provider.py` |
| 3 | mmdash backend write APIs | `model.py` |
| 4 | mmdash frontend editor | `model/page.tsx` |
| 5 | Integration testing | manual E2E |

---

## 12. Open Questions

1. Should mmdash cache documosa content in Redis or drop the cache layer entirely? Documosa has its own caching via snapshot.
2. Should mmdash support switching a project's document provider (e.g., from Notion to documosa) with content migration?
3. For the frontend editor, should we use a simple `<textarea>` or integrate a richer Markdown editor (e.g., `@uiw/react-md-editor`)?

---

**Related Documents**:
- `docs/documosa-integration-requirements.md` — documosa-side requirements
- `CLAUDE.md` — mmdash project architecture overview
