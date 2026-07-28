# v0.1 implementation coverage matrix

This matrix maps original functionality to its v0.4 architecture owner and
evidence. It is expanded as product modules are implemented.

| Original capability           | v0.4 architecture location                       | Implementing module  | Test evidence                       |
| ----------------------------- | ------------------------------------------------ | -------------------- | ----------------------------------- |
| Shared technical baseline     | Root workspaces, `deploy/compose`, Core platform | engineering baseline | root `pnpm check`                   |
| Minimal Web-to-database chain | Web, Web BFF, Core example, PostgreSQL           | example              | BFF and Go unit tests; `pnpm smoke` |
| Searchable API reference      | `contracts/openapi`, `docs/api`                  | API documentation    | `pnpm api:check`                    |
