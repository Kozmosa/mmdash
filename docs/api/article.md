# Article authoring and build contract

Article uses one authoritative Tiptap JSON document, a Yjs collaboration
snapshot, and derived Markdown. Top-level blocks have stable `id` attributes.
Core normalizes the JSON and derives block projections and Markdown in the same
draft transaction; clients must not persist signed download URLs. Artifact
images store Artifact and immutable Version IDs and are resolved by the Worker
only for a frozen Preview or Build.

The supported rich nodes include headings, paragraphs, lists, block quotes,
code, inline/block math, ordinary and Artifact images, tables with bound
captions, Zotero citations, and immutable Artifact/Model/Experiment references.
Image captions serialize below their image, table captions immediately above
their table, and Zotero citations serialize as Pandoc citation keys.

The Web BFF collaboration server initializes an empty Yjs document from Core's
Tiptap JSON for legacy drafts before accepting browser updates. Commit and
Preview routes flush collaboration first. Preview freezes a draft revision;
formal Build freezes a Commit and its reference bibliography. A Worker failure
is reported to Core without terminating the Worker polling loop.
Publication and Release requests may omit `notes` or send an empty string; the
stored record keeps an empty publication note in either case.

## Built-in template and rendering theme

Aggregate reads idempotently provision the deterministic
`mmdash 默认论文模板` as a protected system Artifact. The ZIP contains a
versioned manifest and a XeLaTeX `ctexart` entrypoint with math, images,
`booktabs`, and Pandoc-generated content support. Generated TeX and bibliography
targets use ordinary relative filenames so they remain readable under the
Worker's paranoid TeX file-access policy. It is not eligible for normal
Preview/Build selection until its automatic template-test Build succeeds.
Ordinary template update, version initialization, trash, restore, and purge
operations reject this protected Artifact; users register a separate template
to customize it.

The draft is the only mandatory Article aggregate projection. The aggregate
also includes the latest commit operation statuses when available, allowing
the Web workbench to restore asynchronous Commit and publication progress after
a dialog closes or a page is refreshed. Version History renders the latest
operation state as a compact status icon and opens the recent queue, including
stable failure details, on demand. If references, commits, commit
operations, builds, releases, templates, or chapter tags temporarily fail to
load, Core returns the usable draft plus an `ARTICLE_COMPONENT_UNAVAILABLE`
entry in `warnings`. The warning exposes a stable component name and safe
message rather than a database or adapter error. A draft read failure still
fails the request because the collaboration document cannot be initialized
safely.

The project setting `article.rendering` stores `md` or `latex`. It changes the
authoring and reference-preview presentation only; canonical Markdown content
is unchanged. LaTeX presentation loads Noto Serif SC, displays hierarchical
heading counters, uses three-line tables, and keeps table/image captions in
their publication positions.

## Article chapter tags

Chapter tags are stored separately from Article block tags. Each record is
bound to one stable `heading_block_id` and stores the heading type and a
content fingerprint used to protect review provenance.

When a persisted draft changes a heading's content, changes its node type, or
removes/replaces its block ID, the bound chapter tag remains available but is
marked `needs_review` with a `stale_reason`. A new heading ID receives a fresh
`unedited` chapter tag during draft persistence. A review succeeds only when
the current heading still exists, is a heading node, and matches the stored
fingerprint; successful review records `reviewed_by` and `reviewed_at`.

The `PATCH` operation resets a tag to `unedited` or `unreviewed` after the
caller has inspected the heading change and refreshes its fingerprint. The
dedicated `POST .../review` operation sets or withdraws the reviewed state and
restores the tag that preceded the review when withdrawing it.
