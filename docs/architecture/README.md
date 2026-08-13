# Architecture index

mmdash v0.1 uses a monorepo with strict process boundaries:

```text
Browser -> Web BFF -> Go Core -> PostgreSQL / Object Storage
Local Coding Agent -> Go CLI stdio bridge -> MCP Gateway -> Go Core
Bound Hermes Agent -> MCP Gateway -> Go Core
Go Core <-> Python Worker
Go Core <-> Box Gateway -> Capability -> Runtime
```

The two Agent paths use different principals. The local path delegates the
signed-in user's CLI identity. Hermes is an independently hosted MCP client and,
in the later Agent stage, presents its own Project-scoped Agent Token directly
to the Gateway; it never traverses the CLI. `manual` and `auto` Hermes
management modes change who installs and rotates that credential, not the MCP
runtime path. Automatic management uses a separate server-reachable Hermes
management connection, either directly or through an authenticated network
layer such as Cloudflare Access.

Only Core owns authoritative business state. Web BFF, MCP Gateway, CLI, Worker,
and Box do not write the business database directly.

Stage 9 keeps the same boundary for collaborative writing: the existing Web
BFF hosts one Hocuspocus/Yjs instance for browser rooms and presence, while all
draft snapshots, blocks, commits, builds, templates, and releases remain Core
state. Worker Article builds use only Job-scoped Core input/output APIs and do
not connect to PostgreSQL, Git, object storage, Zotero, or model vendors.

The remote MCP boundary prefers the 2026-07-28 stateless Streamable HTTP
protocol. Logical mmdash sessions use a separate principal-bound header for
audit correlation; they are not presented as protocol-level MCP sessions.

The design baseline is maintained under `docs/design/v0.1`. Implementation
decisions that change module boundaries belong under `docs/adr`.

More detail:

- [Core platform boundaries](core-platform.md)
