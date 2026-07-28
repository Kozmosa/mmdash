# Documosa Integration Requirements Document

**Prepared for**: Documosa Development Agent
**Project**: mmdash (Mathematical Modeling Dashboard) — Documosa Integration
**Date**: 2026-05-01

---

## 1. Background

[mmdash](https://github.com/kozmosa/mmdash) is a collaborative platform for mathematical modeling competitions. It currently supports Notion and a simple local file server (`doc_server`) as document backends via a pluggable `DocumentProvider` abstraction.

We want to integrate **documosa** as a first-class document backend for mmdash. Documosa should remain an independent, generic document service — mmdash is just one of potentially many clients.

## 2. Design Principles

1. **Documosa remains independent** — no mmdash-specific code in core document logic.
2. **Existing API preserved** — `/api/documents/*` stays untouched for documosa's native UI and generic clients.
3. **Adapter layer pattern** — mmdash-specific endpoints live under `/api/mmdash/*`, internally calling existing core logic.
4. **Unified authentication** — both services use JWT (shared `SECRET_KEY`). No API keys or separate login flows.
5. **One mmdash Project = one documosa Document** — direct 1:1 ID mapping.

## 3. Functional Requirements

### 3.1 Authentication Upgrade (JWT)

**Current state**: documosa trusts LAN headers (`x-documosa-client-id`, `x-documosa-nickname`, `x-documosa-role-mode`). This is insecure for cross-service communication.

**Required change**: Implement JWT verification for all `/api/mmdash/*` routes.

#### 3.1.1 Configuration

Add to documosa's environment / config:
```bash
JWT_SECRET=<same value as mmdash SECRET_KEY>
JWT_ALGORITHM=HS256  # default, configurable
```

#### 3.1.2 JWT Verification Middleware

- Extract token from `Authorization: Bearer <token>` header.
- Verify signature using `JWT_SECRET`.
- Validate `exp` claim (reject expired tokens).
- Extract identity from JWT payload:
  - `client_id` = `"mmdash-{sub}"` (prefix to avoid collision with native documosa clients)
  - `nickname` = `name` field from payload, fallback to `email`, fallback to `"mmdash-user"`
  - `role_mode` = `Writer` (mmdash users always have write permission)

- On validation failure, return `401 Unauthorized` with JSON body:
  ```json
  {"error": "invalid_token", "message": "..."}
  ```

#### 3.1.3 Existing Routes Behavior

- `/api/documents/*` and `/api/documents/{id}/ws` continue using existing LAN header auth (or optionally also accept JWT — your call, but don't break native UI).
- Only `/api/mmdash/*` **requires** JWT.

### 3.2 Mmdash Adapter API Layer (`/api/mmdash/*`)

Implement a new Axum router mounted at `/api/mmdash`. All routes in this namespace require JWT auth (section 3.1).

#### 3.2.1 GET `/api/mmdash/documents/{document_id}/content`

**Purpose**: Return document content in mmdash-compatible format.

**Implementation**:
1. Look up document by `document_id`.
2. Retrieve all non-deleted lines, ordered by `order_index`.
3. Join lines into a single Markdown string.
4. Parse Markdown into Notion-compatible `blocks` array.
5. Return JSON.

**Response 200**:
```json
{
  "page_id": "550e8400-e29b-41d4-a716-446655440000",
  "title": "Document Title",
  "blocks": [
    {"type": "heading_1", "content": "Heading Text"},
    {"type": "heading_2", "content": "Subheading"},
    {"type": "paragraph", "content": "Plain text paragraph."},
    {"type": "code", "content": "import numpy as np", "language": "python"},
    {"type": "equation", "content": "E = mc^2"},
    {"type": "bulleted_list_item", "content": "List item 1"},
    {"type": "numbered_list_item", "content": "Numbered item 1"},
    {"type": "quote", "content": "A quote."},
    {"type": "divider"}
  ],
  "markdown": "# Heading Text\n\n## Subheading\n\nPlain text paragraph.\n```python\nimport numpy as np\n```\n$$ E = mc^2 $$\n- List item 1\n1. Numbered item 1\n\n> A quote.\n\n---"
}
```

**Block type mapping rules** (from Markdown → blocks):

| Markdown Pattern | Block Type | Fields |
|---|---|---|
| `# text` | `heading_1` | `content` |
| `## text` | `heading_2` | `content` |
| `### text` | `heading_3` | `content` |
| `\`\`\`lang\ncode\n\`\`\`` | `code` | `content`, `language` |
| `$$ text $$` | `equation` | `content` |
| `- text` | `bulleted_list_item` | `content` |
| `1. text` | `numbered_list_item` | `content` |
| `> text` | `quote` | `content` |
| `---` or `***` | `divider` | (no content) |
| plain text | `paragraph` | `content` |

**Error responses**:
- `404 Not Found` — document does not exist.
- `401 Unauthorized` — invalid or missing JWT.

#### 3.2.2 GET `/api/mmdash/documents/{document_id}`

**Purpose**: Return document metadata only.

**Response 200**:
```json
{
  "page_id": "550e8400-e29b-41d4-a716-446655440000",
  "title": "Document Title",
  "created_at": "2024-01-15T08:30:00Z",
  "updated_at": "2024-01-20T14:22:00Z"
}
```

#### 3.2.3 POST `/api/mmdash/documents`

**Purpose**: Create a new document.

**Request**:
```json
{
  "title": "New Document Title",
  "content": "# Initial Heading\n\nInitial paragraph."
}
```

- `title`: required, non-empty string.
- `content`: optional Markdown string. If omitted, create empty document.

**Implementation**:
1. Use existing `db::create_document` core function.
2. Convert `content` Markdown to lines (split by `\n`).
3. Use JWT-derived identity as the actor for audit log.

**Response 201**:
```json
{
  "page_id": "660e8400-e29b-41d4-a716-446655440001",
  "title": "New Document Title",
  "created_at": "2024-01-15T08:30:00Z"
}
```

#### 3.2.4 PUT `/api/mmdash/documents/{document_id}/content`

**Purpose**: Full-content replacement (mmdash editor saves entire document).

**Request**:
```json
{
  "title": "Optional New Title",
  "markdown": "# Updated content\n\nNew paragraph.",
  "blocks": [
    {"type": "heading_1", "content": "Updated content"},
    {"type": "paragraph", "content": "New paragraph."}
  ]
}
```

- `title`: optional. If provided, update document title.
- `blocks` and `markdown`: **exactly one must be provided**. If both present, prefer `blocks`.

**Implementation**:
1. If `blocks` provided, convert blocks → Markdown string (see reverse mapping below).
2. Convert Markdown → lines.
3. Use existing `db::update_content` core function for optimistic-locking full replacement.
4. For `base_revisions`, fetch current active lines and pass their `(line_id, revision)` tuples.
5. Use JWT-derived identity as actor.

**Block → Markdown reverse mapping**:

| Block Type | Markdown Output |
|---|---|
| `heading_1` | `# {content}` |
| `heading_2` | `## {content}` |
| `heading_3` | `### {content}` |
| `code` | `\`\`\`{language}\n{content}\n\`\`\`` |
| `equation` | `$$ {content} $$` |
| `bulleted_list_item` | `- {content}` |
| `numbered_list_item` | `1. {content}` |
| `quote` | `> {content}` |
| `divider` | `---` |
| `paragraph` | `{content}` |

**Response 200**:
```json
{
  "page_id": "550e8400-e29b-41d4-a716-446655440000",
  "title": "Updated Title",
  "blocks": [...],
  "markdown": "..."
}
```

**Error responses**:
- `409 Conflict` — optimistic lock conflict (document was modified by another client since last read). Return current snapshot so client can retry.
- `404 Not Found` — document does not exist.

## 4. Non-Functional Requirements

### 4.1 Backward Compatibility
- All existing `/api/documents/*` routes must continue working exactly as before.
- Native documosa UI must be unaffected.
- WebSocket `/api/documents/{id}/ws` must continue using existing auth.

### 4.2 Audit Logging
- All write operations via `/api/mmdash/*` must be recorded in the existing `audit_events` table.
- Use JWT-derived identity (`client_id`, `nickname`, `role_mode=writer`) as the actor.

### 4.3 Performance
- Block conversion (Markdown ↔ blocks) should be lazy — only compute when `/api/mmdash/*` is called.
- The core line model must not be altered for this integration.

### 4.4 Error Handling
- Use existing `AppError` enum and error response format.
- All `/api/mmdash/*` errors should be machine-parseable JSON.

## 5. Files to Modify / Create

| File | Action | Description |
|---|---|---|
| `src/api.rs` | Modify | Add `/api/mmdash` router alongside existing router |
| `src/mmdash_api.rs` | **Create** | New module: mmdash adapter route handlers |
| `src/mmdash_auth.rs` | **Create** | JWT verification middleware and identity extraction |
| `src/mmdash_blocks.rs` | **Create** | Markdown ↔ blocks bidirectional converter |
| `src/main.rs` | Modify | Mount new router, add JWT config |
| `src/models.rs` | Modify (minor) | Add `RoleMode::Writer` default if needed |
| `.env.example` | Modify | Add `JWT_SECRET`, `JWT_ALGORITHM` |

## 6. Dependencies to Add

Add to `Cargo.toml`:
```toml
jsonwebtoken = "9"
serde = { version = "1", features = ["derive"] }  # likely already present
```

## 7. Testing Expectations

- Unit tests for Markdown ↔ blocks converter (cover all block types).
- Integration tests for each `/api/mmdash/*` endpoint with valid/invalid JWT.
- Verify existing `/api/documents/*` tests still pass (no regression).

## 8. Open Questions (for documosa agent to resolve)

1. Should the block parser support nested lists? (mmdash currently uses flat lists.)
2. For equations, mmdash uses `$$ ... $$` inline and display. Should documosa preserve the distinction?
3. Should `/api/mmdash/documents/{id}/content` also return `comments` and `suggestions` (for mmdash to optionally display)? Or keep it content-only?

---

**Contact**: mmdash integration lead. For questions about block format specifics, reference `backend/app/api/model.py:_blocks_to_markdown` in the mmdash repo.
