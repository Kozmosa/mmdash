# Auth, Project, and RBAC API

This page is the operational lookup guide for stage 3.8. The canonical
schemas remain in `contracts/openapi/core.yaml` and
`contracts/openapi/web-bff.yaml`.

## Browser login

1. `POST /api/auth/login` accepts an email and password.
2. Web BFF calls Core `auth.login`.
3. Core verifies the password, persists a revocable session, and returns a
   signed JWT.
4. BFF stores the JWT inside the signed, HTTP-only `mmdash_session` cookie.
5. Authenticated BFF calls forward that JWT to Core as a Bearer token.

The cookie uses `SameSite=Lax`, is scoped to `/`, and is `Secure` in
production. `POST /api/auth/logout` revokes the Core session before clearing
the browser cookie.

Local Compose creates the configurable bootstrap account declared by
`AUTH_BOOTSTRAP_EMAIL` and `AUTH_BOOTSTRAP_PASSWORD`.

## Non-browser tokens

Core supports three revocable opaque token kinds:

| Kind    | Intended caller                  | Project scope                        |
| ------- | -------------------------------- | ------------------------------------ |
| `api`   | Human automation and API clients | Optional                             |
| `agent` | MCP Gateway and project agents   | Required by callers for project work |
| `box`   | Registered Box capability nodes  | Required by callers for project work |

Only a SHA-256 token hash is stored. The secret is returned once by
`auth.tokens.create`; list operations expose metadata only.

## Collaborative project roles

| Role         | Effective project permissions                         |
| ------------ | ----------------------------------------------------- |
| `owner`      | read, update, archive, manage members/tokens/settings |
| `maintainer` | read, update, manage members/tokens/settings          |
| `editor`     | read, update, read settings                           |
| `viewer`     | read, read settings                                   |
| `agent`      | read, read settings                                   |
| `box`        | read, read settings                                   |

The creator becomes the first owner in the same transaction as project
creation. Removing the last owner is rejected. Agent and Box credentials are
restricted to their configured `project_id`.

## Project data boundary

Project owns the structured problem fields:

- `problem_title`
- `problem_summary`
- `project_constraints[]`
- `source_artifact_ids[]`

Artifact IDs are references only. File metadata and binary content remain
owned by the later Artifact module.
