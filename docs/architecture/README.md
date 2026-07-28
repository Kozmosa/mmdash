# Architecture index

mmdash v0.1 uses a monorepo with strict process boundaries:

```text
Browser -> Web BFF -> Go Core -> PostgreSQL / Object Storage
Agent -> CLI or Hermes -> MCP Gateway -> Go Core
Go Core <-> Python Worker
Go Core <-> Box Gateway -> Capability -> Runtime
```

Only Core owns authoritative business state. Web BFF, MCP Gateway, CLI, Worker,
and Box do not write the business database directly.

The remote MCP boundary prefers the 2026-07-28 stateless Streamable HTTP
protocol. Logical mmdash sessions use a separate principal-bound header for
audit correlation; they are not presented as protocol-level MCP sessions.

The design baseline is maintained under `docs/design/v0.1`. Implementation
decisions that change module boundaries belong under `docs/adr`.
