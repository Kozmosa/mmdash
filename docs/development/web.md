# Web foundation

`apps/web` is the Next.js App Router shell for browser interaction. It owns
presentation and browser state, but never accesses PostgreSQL, object storage,
Git, Hermes, or Box directly.

## Route map

| Route                               | Purpose                         |
| ----------------------------------- | ------------------------------- |
| `/login`                            | Browser-authentication shell    |
| `/projects`                         | Project-list shell              |
| `/projects/[projectId]`             | Project workspace and home slot |
| `/projects/[projectId]/agent`       | mmdash Agent slot               |
| `/projects/[projectId]/progress`    | Progress slot                   |
| `/projects/[projectId]/models`      | Model slot                      |
| `/projects/[projectId]/article`     | Article slot                    |
| `/projects/[projectId]/experiments` | Experiment slot                 |
| `/projects/[projectId]/settings`    | Module settings slots           |

The canonical seven-item navigation registry is
`apps/web/src/lib/navigation.ts`. A feature route must be registered there and
tested before it appears in the sidebar.

## Context and data boundaries

- `UserProvider` exposes the current browser identity to UI components.
- `ProjectProvider` exposes the current project selected by the dynamic route.
- TanStack Query owns server-state caching once BFF read endpoints are added.
- Zustand owns local workspace chrome state, such as sidebar visibility.
- `ApiClient` sends same-origin `/api` requests with browser credentials and
  converts safe BFF errors into `ApiError`.

The bootstrap user and project label are shell-only values. Auth and Project
stages replace them with BFF data.

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

## Settings slots

`SettingsSlotRegistry` stores ordered descriptors and rejects duplicate IDs.
Each future domain module owns its settings UI and explicitly registers one
descriptor. The Settings shell displays unimplemented descriptors without
inventing temporary business state.

## Visual baseline

The neutral OKLCH tokens, compact radii, bordered cards, collapsible sidebar,
and restrained shadows intentionally follow the archived v0.0 front-end. New
work should use semantic variables in `src/app/styles.css` instead of hard-coded
page colors.
