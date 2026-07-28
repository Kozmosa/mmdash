# v0.1 implementation coverage matrix

This matrix maps original functionality to its v0.4 architecture owner and
evidence. It is expanded as product modules are implemented.

| Original capability           | v0.4 architecture location                       | Implementing module  | Test evidence                        |
| ----------------------------- | ------------------------------------------------ | -------------------- | ------------------------------------ |
| Shared technical baseline     | Root workspaces, `deploy/compose`, Core platform | engineering baseline | root `pnpm check`                    |
| Minimal Web-to-database chain | Web, Web BFF, Core example, PostgreSQL           | example              | BFF and Go unit tests; `pnpm smoke`  |
| Searchable API reference      | `contracts/openapi`, `docs/api`                  | API documentation    | `pnpm api:check`                     |
| Browser application shell     | `apps/web` App Router, providers, route registry | web foundation       | `apps/web/test` and production build |
| Browser API boundary          | Fastify BFF and generated Core client             | web-bff foundation   | BFF unit and streaming proxy tests   |
| Agent API boundary            | MCP Gateway, token/session policy, tool registry  | MCP foundation       | MCP v2 transport and permission tests |
| Local CLI shell               | Installable command registry and platform paths   | CLI foundation       | CLI command, path, build, and pack tests |
