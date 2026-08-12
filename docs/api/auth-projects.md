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

## CLI device authorization and refresh

The native CLI never collects an account password. It starts a ten-minute
device authorization with `auth.device.authorize`, opens the returned
`verification_uri_complete`, and polls `auth.device.token` at the advertised
interval. An authenticated browser approves or denies the user code through
`bff.auth.device.verify`. Device codes and user codes are stored only as hashes,
and a successful exchange is atomic and single-use.

Core sessions now have a shorter access-token lifetime and a longer absolute
session lifetime. `auth.refresh` rotates both the JWT and the high-entropy
refresh token using compare-and-swap storage; the previous pair is invalid as
soon as rotation succeeds. The CLI keeps both secrets only in the operating
system credential store. Browser BFF continues to expose neither secret to
JavaScript.

Local Compose creates the configurable bootstrap account declared by
`AUTH_BOOTSTRAP_EMAIL` and `AUTH_BOOTSTRAP_PASSWORD`.

## Non-browser tokens

The generic `auth.tokens.create` operation issues two revocable opaque token
kinds:

| Kind    | Intended caller                  | Project scope                        |
| ------- | -------------------------------- | ------------------------------------ |
| `api`   | Human automation and API clients | Optional                             |
| `box`   | Registered Box capability nodes  | Required by callers for project work |

Only a SHA-256 token hash is stored. The secret is returned once by
`auth.tokens.create`; list operations expose metadata only.

Stage 5 product Agent Tokens use a separate Auth-owned lifecycle so an Agent
instance, Project Grant, exact Tool allowlist, pending verification, and safe
rotation cannot be omitted by a generic caller. An Agent instance receives a
high-entropy opaque Token through the project-scoped Agent API. In `manual`
management mode the user stores the one-time secret in Hermes; in `auto` mode
the Hermes Adapter writes it through an authenticated, server-reachable Hermes
management endpoint. Hermes then presents the secret directly to the remote
MCP Gateway. The user-device CLI is neither a hop in that request path nor a
holder of the Hermes Agent Token.

The Gateway authenticates a pending product Token through `auth.me`, but that
authentication alone is not verification. After a protocol-negotiated MCP
Session successfully performs `tools/list`, the Gateway records durable
evidence through `auth.agent_tokens.verification.record` using the same pending
Agent Token and the single-use challenge embedded in its one-time MCP endpoint.
Core accepts only the exact pending Token/Agent/Project binding and atomically
consumes the stored challenge Hash. An admin API Token, browser Session,
`auth.me` request, current `server/discover`, or legacy `initialize` cannot
create evidence by itself.
Activation then atomically promotes the
pending Token and revokes its predecessor. Any verification or transaction
failure leaves the old Token active.

The generic request and `AccessToken.kind` schemas continue to recognize the
historical `agent` value for wire and stored-data compatibility, but
`auth.tokens.create` rejects that value with `400 INVALID_REQUEST`. New product
Agent credentials can be created only through the project-scoped Agent
lifecycle.

The static `MCP_AGENT_TOKEN` accepted by the current foundation Gateway is a
development and boundary-test credential. It is not the product Agent Token
lifecycle described above.

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
creation. An owner cannot change their role or remove themselves through the
ordinary member operations. Assigning `owner` to another existing member is an
atomic ownership transfer: the target becomes owner and the caller becomes
maintainer. Owner invitations are rejected, so this explicit transfer is the
only supported ownership transition. Human invitations and member role changes
accept only `owner`, `maintainer`, `editor`, and `viewer` as applicable;
`agent` and `box` remain machine credential roles and cannot be assigned to a
human collaborator. Agent and Box credentials are restricted to their
configured `project_id`.

A recipient may reject a pending invitation with its one-time token without
signing in. Rejection changes the invitation status to `declined` and
permanently prevents later acceptance.

## Project recycle bin

Only the current project owner may move a project to the recycle bin. Trashed
projects disappear from every member's normal project list and cannot be used
through project-scoped APIs. The owner can list and restore them for 30 days.
After `purge_at`, Core permanently removes the project and its cascading
project-owned records. Projects that used the earlier archive action are
migrated into the recycle bin with the same 30-day recovery window.

## Project data boundary

Project owns the structured problem fields:

- `problem_title`
- `problem_summary`
- `project_constraints[]`
- `source_artifact_ids[]`

Artifact IDs are references only. File metadata and binary content remain
owned by the later Artifact module.
