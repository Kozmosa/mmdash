# v0.1 implementation coverage matrix

This matrix maps original functionality to its v0.4 architecture owner and
evidence. It is expanded as product modules are implemented.

| Original capability                         | v0.4 architecture location                                        | Implementing module  | Test evidence                                      |
| ------------------------------------------- | ----------------------------------------------------------------- | -------------------- | -------------------------------------------------- |
| Shared technical baseline                   | Root workspaces, `deploy/compose`, Core platform                  | engineering baseline | root `pnpm check`                                  |
| Minimal Web-to-database chain               | Web, Web BFF, Core example, PostgreSQL                            | example              | BFF and Go unit tests; `pnpm smoke`                |
| Searchable API reference                    | `contracts/openapi`, `docs/api`                                   | API documentation    | `pnpm api:check`                                   |
| Browser application shell                   | `apps/web` App Router, providers, route registry                  | web foundation       | `apps/web/test` and production build               |
| Browser API boundary                        | Fastify BFF and generated Core client                             | web-bff foundation   | BFF unit and streaming proxy tests                 |
| Agent API boundary                          | MCP Gateway, token/session policy, tool registry                  | MCP foundation       | MCP v2 transport and permission tests              |
| Native local CLI and MCP bridge             | Device login, secure credentials, Project selection, stdio bridge | Stage 3 CLI/MCP      | Go unit, cross-build, Gateway, and stdio E2E tests |
| Authoritative Core boundary                 | Go modular monolith and platform infrastructure                   | Core foundation      | Go platform/module tests and readiness checks      |
| User login and revocable credentials        | Core Auth, BFF session cookie, Web login                          | auth                 | Go auth tests and BFF login tests                  |
| Team projects and role-based access         | Core Project, BFF project routes, Web project list                | project              | Go RBAC tests and BFF forwarding tests             |
| Typed configuration and secret management   | Core Settings, BFF settings routes, Web settings slots            | settings             | Go registry/crypto/service and BFF proxy tests     |
| Generated contracts and compatibility gates | OpenAPI, JSON Schema, TS/Go generation, Mock Server               | contracts            | `pnpm contracts:check` and Go validation tests     |
| Durable asynchronous work                   | Core Job Queue and Python Worker HTTP runtime                     | jobs/worker          | Go queue-policy and Python runtime tests           |
| Reliable domain-event delivery              | Event Bus, Outbox Processor, delivery/replay tables               | events/outbox        | Go Bus, Processor, idempotency, and API tests      |
| Managed project Git repositories            | Core Repo, BFF read/manage routes, read-only Web, MCP             | repo                 | Go real-Git tests, Web/BFF/MCP tests, Repo E2E     |
