# Local development

## Prerequisites

- Node.js 20.9 or newer and pnpm 11
- Go 1.18 or newer (the modules remain buildable with Go 1.17 during bootstrap)
- Python 3.11 or newer and uv
- Docker Compose

Copy `.env.example` to `.env`, then install and verify all workspaces:

```bash
pnpm install
uv sync --all-packages
pnpm check
```

## Run the baseline

```bash
pnpm dev
pnpm smoke
pnpm dev:down
```

The smoke test verifies Web → Web BFF → Core → PostgreSQL. Local ports are
Web `3000`, BFF `3001`, Core `8080`, PostgreSQL `5432`, MinIO API `9000`, and
MinIO Console `9001`.

Run migrations independently with:

```bash
DATABASE_URL=postgres://mmdash:mmdash@localhost:5432/mmdash?sslmode=disable pnpm migrate
```

## Quality commands

| Command                                       | Purpose                                          |
| --------------------------------------------- | ------------------------------------------------ |
| `pnpm lint`                                   | TypeScript, Go formatting, and Python lint       |
| `pnpm test`                                   | TypeScript, Go, and Python tests                 |
| `pnpm build`                                  | All three language builds                        |
| `pnpm format`                                 | Format supported source files                    |
| `pnpm api:check`                              | Check OpenAPI operations against the API catalog |
| `pnpm commit:check -- "feat(scope): summary"` | Validate a commit subject                        |

## Create a module

```bash
pnpm scaffold:module -- sample
```

The generator creates explicit Core, BFF, Web, and contract starting points.
Read the generated `README.md`, add the endpoint to the OpenAPI contract and API
catalog, then add tests at every process boundary. Domain modules may omit a
layer only when their module documentation explains why.
