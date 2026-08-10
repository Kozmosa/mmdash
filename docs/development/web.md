# Web foundation

`apps/web` is the Next.js App Router shell for browser interaction. It owns
presentation and browser state, but never accesses PostgreSQL, object storage,
Git, Hermes, or Box directly.

## Route map

| Route                               | Purpose                              |
| ----------------------------------- | ------------------------------------ |
| `/login`                            | Browser-authentication shell         |
| `/inbox`                            | Global unread/all/processed Inbox    |
| `/inbox/[inboxItemId]`              | Controlled Inbox message detail      |
| `/projects`                         | Project-list shell                   |
| `/projects/[projectId]`             | Project workspace and home slot      |
| `/projects/[projectId]/agent`       | mmdash Agent slot                    |
| `/projects/[projectId]/progress`    | Progress slot                        |
| `/projects/[projectId]/models`      | Model slot                           |
| `/projects/[projectId]/article`     | Article slot                         |
| `/projects/[projectId]/experiments` | Experiment-record slot               |
| `/projects/[projectId]/repository`  | Read-only managed Repository browser |
| `/projects/[projectId]/settings`    | Module settings slots                |

The canonical seven-item navigation registry is
`apps/web/src/lib/navigation.ts`. A feature route must be registered there and
tested before it appears in the sidebar.

Inbox is global rather than one of the seven project modules. The shared
`InboxNavLink` renders the same icon-only link and unread badge in the global
project-list header and project workspace navbar. Inbox copy comes from the
Core-rendered snapshot; the browser does not derive arbitrary presentation
from raw event payloads.

The Repository browser is intentionally reached from Repo settings and is not
registered in the workspace sidebar. The experiment route remains reserved for
experiment records whose future detail view binds a commit and combines its
file tree, file preview, and result analysis.

## Context and data boundaries

- `UserProvider` exposes the current browser identity to UI components.
- `ProjectProvider` exposes the current project selected by the dynamic route.
- TanStack Query owns server-state caching once BFF read endpoints are added.
- Zustand owns local workspace chrome state, such as sidebar visibility.
- `ApiClient` sends same-origin `/api` requests with browser credentials and
  converts safe BFF errors into `ApiError`.

`UserProvider` resolves the signed browser session from `/api/auth/me`.
`/login` creates the revocable Core session through BFF, and `/projects`
loads, creates, archives, and enters collaborative projects through BFF.
Workspace project-detail hydration remains a follow-up concern for richer
domain pages.

## Shared UI

Reusable components live under `apps/web/src/components`:

- `states`: Loading, Error, Empty, and feature placeholder;
- `ui/data-table.tsx`: accessible generic table;
- `ui/file-tree.tsx`: nested file display;
- `ui/log-viewer.tsx`: structured log stream;
- `ui/diff-viewer.tsx`: line-oriented change display;
- `ui/timeline.tsx`: ordered activity or version history;
- `ui/markdown-preview.tsx`: safe Markdown rendering;
- `ui/code-editor.tsx`: lazily loaded Monaco editor.

Heavy editors are dynamically imported and should stay outside initial route
bundles. Domain-specific components belong under `features/<module>`.

Notification UI lives under `features/notification`. `/inbox` defaults to
unread and unarchived messages and supports processed/project/type/time views,
cursor pagination, archive/read mutations, scoped batch read, and typed source
actions. Project settings display Type-owned Inbox policy as read-only context;
their editable Rule surface is exclusively for external channels.

## Settings slots

`SettingsSlotRegistry` stores ordered descriptors and rejects duplicate IDs.
Each future domain module owns its settings UI and explicitly registers one
descriptor. The Settings shell also reads Core's project-scoped type registry
through BFF, so registered field contracts, secret markers, and connection
test support can be discovered without hard-coding provider configuration in
the shell.

## Visual baseline

The neutral OKLCH tokens, compact radii, bordered cards, collapsible sidebar,
and restrained shadows intentionally follow the archived v0.0 front-end. New
work should use semantic variables in `src/app/styles.css` instead of hard-coded
page colors.
