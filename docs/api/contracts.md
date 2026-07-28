# Contracts, code generation, and compatibility

This page is the searchable operational guide for stage 3.10.

## Sources of truth

| Boundary     | Source                                          | Generated / runtime consumer               |
| ------------ | ----------------------------------------------- | ------------------------------------------ |
| Core HTTP    | `contracts/openapi/core.yaml`                   | TypeScript Core Client and Go request DTOs |
| Browser HTTP | `contracts/openapi/web-bff.yaml`                | BFF/browser contract tests                 |
| Shared DTOs  | `contracts/json-schema/common/dtos.schema.json` | `@mmdash/api-types`                        |
| Events       | `contracts/events/event-envelope.schema.json`   | TypeScript and Go Event Envelope           |
| MCP tools    | `contracts/json-schema/mcp-tools/*.json`        | MCP registry and API catalog               |

`contracts/openapi/catalog.json` declares each OpenAPI document, its audience,
and generation targets.

## Commands

```bash
pnpm contracts:generate
pnpm contracts:check
pnpm contracts:baseline
pnpm contract:mock
```

- `contracts:generate` refreshes the generated TypeScript Core API, shared
  TypeScript DTOs, Go handler request types, Go validation, and Go Event
  Envelope.
- `contracts:check` compiles schemas, validates examples and mock fixtures,
  checks generated-file freshness, exercises the Mock Server, and applies the
  compatibility baseline.
- `contracts:baseline` rewrites the compatibility floor and therefore requires
  explicit review.
- `contract:mock` starts the local Core mock at `127.0.0.1:4010` by default.
  Override `CONTRACT_MOCK_HOST` or `CONTRACT_MOCK_PORT` when needed.

## Go handler validation

Request body schemas named in Core OpenAPI generate structs under
`backend/internal/contract/generated`. Core handlers decode through
`httpx.DecodeJSON`, which enforces:

- a 1 MiB maximum body;
- exactly one JSON value;
- unknown-field rejection;
- generated required, format, minimum-length, enum, and `minProperties`
  validation.

Invalid input always becomes the stable `INVALID_REQUEST` envelope. Generated
files carry a `DO NOT EDIT` header.

## JSON Schema and events

Schemas use JSON Schema 2020-12 and stable HTTPS IDs. The Event Envelope always
contains:

```text
event_id event_type schema_version occurred_at producer project_id
actor correlation_id causation_id payload
```

Domain modules version their payloads independently. Envelope fields may not be
renamed or removed.

## Compatibility rules

The baseline rejects removed/moved operations, removed responses, newly
required inputs, removed properties or enum values, type/format/reference
changes, and newly required schema fields. Additive operations and optional
properties are allowed.

See `contracts/compatibility/README.md` before changing a stable contract.
