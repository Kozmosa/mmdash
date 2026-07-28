# Settings and secret management API

This is the operational lookup for stage 3.9. OpenAPI remains the
machine-readable source of truth.

## Ownership and registration

Settings owns the framework, not every product module's configuration. A
module registers a stable type definition in Core with:

- a namespaced `key`, owning module, title, description, and display order;
- supported `system` and/or `project` scopes;
- typed fields (`boolean`, `number`, `secret`, `select`, `string`, or `url`);
- required fields and select options;
- an optional in-process connection tester.

Duplicate keys, unknown scopes, invalid field definitions, and duplicate field
keys are rejected before the server starts. Git, Hermes, Notion, Zotero,
notification, Box, and MCP configuration types remain owned by those later
modules.

Use:

- `GET /v1/settings/types?scope=system` for system types;
- `GET /v1/settings/types?scope=project&project_id={projectId}` for project
  types;
- the corresponding browser routes
  `GET /api/settings/types?scope=system` and
  `GET /api/projects/{projectId}/settings/types`.

## Read, update, and delete

Core system resources use `/v1/settings/system/{typeKey}`. Core project
resources use `/v1/settings/projects/{projectId}/{typeKey}`. The browser
equivalents are `/api/settings/system/{typeKey}` and
`/api/projects/{projectId}/settings/{typeKey}`.

`PATCH` accepts:

```json
{
  "values": {
    "endpoint": "https://provider.example",
    "access_token": "new-secret"
  }
}
```

Only registered fields are accepted. Omitted fields keep their saved values;
`null` removes an optional value. Sending `********` for a secret preserves
the existing ciphertext.

## Secret lifecycle

- Secret fields are split from public JSON before persistence.
- Core encrypts each secret with AES-256-GCM using key material from
  `SETTINGS_ENCRYPTION_KEY`.
- HTTP reads always return `********`; ciphertext, nonce, and plaintext never
  leave Core.
- Only trusted in-process module adapters can resolve decrypted values.
- Outbox events contain scope, type, and version metadata only—never setting
  values or credentials.

Changing `SETTINGS_ENCRYPTION_KEY` without migrating existing ciphertext makes
saved secrets unreadable. Production deployments must inject a stable key
through their secret manager.

## Permissions

System settings require a human/API identity whose `system_role` is `admin`.
The bootstrap account is the initial system administrator.

Project settings use Project RBAC:

| Roles                      | Read | Manage / test |
| -------------------------- | ---- | ------------- |
| owner, maintainer          | yes  | yes           |
| editor, viewer, agent, box | yes  | no            |

Project-scoped Agent and Box tokens remain constrained to their own
`project_id`.

## Connection-test convention

`POST .../{typeKey}/test` tests the saved configuration. Every response uses:

```json
{
  "status": "passed",
  "checked_at": "2026-07-28T12:00:00Z",
  "checks": [
    {
      "name": "authentication",
      "status": "passed"
    }
  ]
}
```

Top-level status is `passed`, `failed`, or `unsupported`; each named check is
`passed` or `failed`. Adapter errors are converted to safe messages and must
not expose credentials or raw provider responses.
