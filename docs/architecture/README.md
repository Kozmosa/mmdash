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

The design baseline is maintained under `docs/design/v0.1`. Implementation
decisions that change module boundaries belong under `docs/adr`.
