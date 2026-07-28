# Core platform boundaries

The Go Core Server is a modular monolith. A domain module owns its tables,
repositories, application services, HTTP handlers, migrations, and emitted
events. Cross-module writes are prohibited.

```text
HTTP request
  -> request context / recovery / JSON access log
  -> explicitly registered domain module
  -> application service
  -> transaction
       -> module-owned tables
       -> system_outbox
  -> stable response or safe error
```

PostgreSQL contains authoritative business state. Object storage contains
Artifact bytes under the configured bucket, while Artifact metadata and
ownership remain in PostgreSQL. The platform object-storage package only
initializes configuration and readiness; signed URLs and object operations
belong to the later Artifact module.

Web BFF, MCP Gateway, CLI, Worker, and Box call Core contracts. They do not
receive database or object-storage credentials and do not write authoritative
state directly.

Shared platform primitives remain under `backend/internal/platform`. Public
`backend/pkg` is reserved for types proven stable enough for external Go
components; stage 3.7 exports none prematurely.

Durable module events follow a second explicit path:

```text
module transaction
  -> system_outbox
  -> leased Outbox publication
  -> one durable delivery per matching Event Bus consumer
  -> consumer handler
  -> consumption idempotency or retry/failure record
```

Publication and delivery use independent PostgreSQL leases with
`FOR UPDATE SKIP LOCKED`, allowing multiple Core processes without duplicate
claims. Explicit replay preserves the original envelope and creates a new
delivery key.
