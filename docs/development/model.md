# Stage 7 Model development and acceptance

Model is the Core authority for a Project's single Notion source, recursively
discovered child pages, question bindings, synchronization state, immutable
Snapshots, content Hashes, character-level Diff, tags, and version notes. The
browser, CLI, MCP Gateway, Worker, and Data Hub never write Model tables
directly.

## User workflow and invariants

1. An owner, maintainer, or editor enters one root-page URL and starts the
   mmdash public Notion integration authorization. The Notion page picker must
   include that root page.
2. Core validates the one-time OAuth `state`, the returning user, Project
   permission, and actual root-page access before encrypting the access and
   refresh tokens. It then creates or updates the Project's single Source. A
   discovery Job recursively reads child pages through the Notion API.
3. The user creates `Q1`, `Q2`, or another explicit question code and binds it
   to one discovered descendant page. One child page can be bound to only one
   active question.
4. A manual or scheduled question sync produces normalized content. Core
   creates a Snapshot only when the normalized content Hash differs from the
   latest Snapshot for that question.
5. The question detail renders its independent timeline and fixed Snapshots.
   Tags and version notes are editable metadata; Snapshot content and Hash are
   immutable.

The Web Model index renders one question card per row. The detail page uses an
approximately `1:6:3` timeline/document/information grid on wide screens and a
responsive stack on smaller screens. It does not expose an mmdash editor for
the Notion document body.

## Settings and credentials

`model.notion` is Project-scoped and defines:

| Field                         | Meaning                                      |
| ----------------------------- | -------------------------------------------- |
| `access_token`                | OAuth secret; encrypted and never returned   |
| `refresh_token`               | Rotating OAuth secret; encrypted and never returned |
| `root_page_url`               | The one shared Project root page             |
| `auto_sync_enabled`           | Enables or disables automatic synchronization |
| `auto_sync_interval_seconds`  | `60`–`86400`; defaults to `300` seconds      |

The tokens are encrypted and redacted by Settings. Core resolves and consumes
them inside the Notion adapter. A rejected access token triggers one serialized
refresh-token rotation and retry; disconnect removes the local credentials and
requests provider revocation without deleting immutable Snapshots. The Worker
receives a Job-bound export of page and block JSON but never either token. Both
legacy `*.notion.so` links, published `*.notion.site` links, and current
`*.notion.com` application links such as
`https://app.notion.com/p/<workspace>/<page-id>` are accepted. The parser uses
only the page ID; content requests still go through the fixed official Notion
API endpoint.

The registered public integration uses `NOTION_OAUTH_CLIENT_ID` and
`NOTION_OAUTH_CLIENT_SECRET`. `NOTION_OAUTH_REDIRECT_URI` defaults to
`MMDASH_PUBLIC_URL/api/integrations/notion/oauth/callback` and must exactly
match a redirect URI registered in Notion. Only content-read capability is
needed. Authorization follows Notion's [public integration authorization
guide](https://developers.notion.com/guides/get-started/authorization); token
exchange, refresh, and revocation use the fixed official Notion OAuth API.
Do not paste client secrets or Project tokens into chat, source control, logs,
CLI arguments, or test fixtures.

Existing development settings that still contain `integration_token` remain
read-only upgrade compatibility: Core can consume them until the first OAuth
authorization succeeds, but the Web no longer accepts a new token. A successful
callback atomically writes OAuth credentials and deletes the legacy token.

## Synchronization and scheduling

Migration `000022_model_stage7` owns `model_sources`, `model_source_pages`,
`model_questions`, `model_syncs`, `model_snapshots`, and
`model_snapshot_assets`. Migration `000023_model_notion_oauth` owns only the
short-lived, hashed, one-use OAuth authorization state; it never stores a
provider token. The unique Project key on `model_sources` enforces a
single Source. Question code and bound-page uniqueness apply to active
questions; archived question history and referenced Artifacts are retained.

Core's scheduler polls every 15 seconds, claims due Sources with PostgreSQL
`FOR UPDATE SKIP LOCKED`, and holds a 30-second coordinator lease. Both a due
run and the Model index sync button first queue one discovery Job. Successful
discovery atomically replaces the descendant set and only then fans out one
Snapshot Job for every valid bound question, so manual and automatic full sync
use the same fresh tree. The schedule advances from the actual trigger time.

Every human Project role (owner, maintainer, editor, and viewer) has the
dedicated `project.model.sync` permission; Agent and Box identities do not.
Either manual sync button resets `next_sync_at` by the configured interval.
If the same source or question already has a queued or running Job, Core reuses
that Job while still applying the countdown reset, so repeated team-member
clicks do not fail with a synchronization conflict. The Settings page shows
the live countdown; the default is five minutes.

Automatic synchronization requires a currently resolvable Project Notion
binding. Starting OAuth alone never creates a Source or a Job. Disconnecting
immediately clears `next_sync_at` and disables the Source, and the scheduler
rechecks the encrypted setting before creating a due Job so a missing or
invalid binding cannot race with asynchronous Settings events.

Job types are `model.notion.discover` and `model.notion.snapshot`. Discovery is
bounded to 64 levels and 1,000 pages; block traversal is bounded to 64 levels
and 20,000 blocks. Core owns retries, leasing, terminal state, and the atomic
Outbox transition. The Python Worker owns Notion block normalization, including
rich text, headings, lists, to-do blocks, code, equations, tables, images,
files, and nested children.

## Hash, media, and immutable versions

The Worker computes the stable SHA-256 over normalized document semantics and
excludes temporary Notion media URLs and generated Artifact identifiers. Core
compares that Hash before media transfer. If it is unchanged, the sync ends as
`unchanged`, no media is downloaded, and no Snapshot/event is created.

For changed content, Core downloads every Notion image or embedded file through
the Artifact importer before committing the Job result. Downloads require
public HTTPS endpoints and reject redirects or DNS results that resolve to
private, loopback, link-local, multicast, or unspecified addresses. Streaming
size and SHA-256 verification uses Artifact's normal multipart lifecycle. The
created Artifact has `kind=model_file`, `source=model`, and the visible
category/tag **模型文件**. Temporary Notion URLs are removed from the durable
Worker result; Snapshots retain only stable Artifact and Version IDs.

## Diff and metadata

Diff first aligns stable block identities and then compares Unicode characters,
not visual lines. Adjacent equal, inserted, or deleted characters are merged
into one operation. The Web view has no line numbers: deletions use a faded
pink background plus strikethrough, additions use a blue background, and equal
content uses normal Notion-like typography. Layout wrapping therefore does not
change Diff semantics.

Users can assign multiple custom tags and edit the version note. The UI offers
`初稿`, `修订中`, and `最终版` as conveniences; Core applies the same validation
to built-in and custom values, deduplicates tags case-insensitively, and never
automatically adds or removes a tag.

## Data Hub, MCP, CLI, and events

Model publishes `model.sync.requested`, `model.source.changed`,
`model.question.changed`, and `model.snapshot.created` without credentials,
raw model content, or temporary media URLs. Data Hub projects `model_source`,
`model_question`, and `model_snapshot`; their authorized full-content readers
make fixed Model information available through the existing MCP `data.list`
and `data.read` tools.

The native human CLI uses the same Core authorization boundary:

```text
mmdash model list
mmdash model show <question_id>
mmdash model sync [question_id]
```

Select a Project first with `mmdash project use <project_id>`. `model sync`
without a question ID queues source discovery and all valid bound questions;
with an ID it queues only that question. Both manual forms reset the automatic
countdown.

## Focused checks

Run these before requesting a real Notion OAuth binding:

```powershell
pnpm contracts:generate
pnpm contracts:check
pnpm api:check

Set-Location backend
go test ./internal/model ./internal/artifact ./internal/datahub ./internal/project

Set-Location ../workers/mmdash-worker
uv run pytest -p no:cacheprovider
uv run ruff check .

Set-Location ../..
pnpm --filter @mmdash/web-bff test
pnpm --filter @mmdash/web test
Set-Location clients/cli
go test ./...

Set-Location ../..
pnpm check
```

After those checks pass, start the local application and ask the user to
authorize the registered mmdash integration and root page through the Settings
UI. Live acceptance must prove CSRF-safe callback completion, recursive
discovery, Q binding, first Snapshot, unchanged Hash behavior, changed Snapshot,
media transfer, metadata edits, Diff rendering, manual countdown reset, MCP
reads, human CLI reads/sync, refresh-token rotation, and local disconnect.

The final Docker acceptance is the standard Compose build, `pnpm smoke`, Model
live checks, service health/log review, and `docker compose ... down` without
`-v`. Caddy is not started; run only `pnpm caddy:check`, which validates the
repository Caddyfile.
