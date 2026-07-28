# Data Hub, object routing, and Project Context

Search terms: Data Hub, Object Registry, stable Object ID, `data.list`,
`data.read`, Activity Timeline, Project Context, Context Proposal,
human confirmation, projection registry, read Adapter, home aggregate.

The canonical request and response schemas are in
[`contracts/openapi/core.yaml`](../../contracts/openapi/core.yaml). Data Hub
lives in `backend/internal/datahub`; migration `000007_datahub` owns its
projection tables.

## Operations

| `operationId`                   | Method | Path                                                                  | Purpose                                     |
| ------------------------------- | ------ | --------------------------------------------------------------------- | ------------------------------------------- |
| `data.list`                     | GET    | `/v1/data/projects/{projectId}/objects`                               | List searchable project objects             |
| `data.read`                     | GET    | `/v1/data/projects/{projectId}/objects/{objectId}`                    | Route a stable Object ID to full content    |
| `data.activity.list`            | GET    | `/v1/data/projects/{projectId}/activity`                              | Read the project activity timeline          |
| `data.context.list`             | GET    | `/v1/data/projects/{projectId}/context`                               | List only human-confirmed context           |
| `data.context.proposals.list`   | GET    | `/v1/data/projects/{projectId}/context/proposals`                     | List context proposals                      |
| `data.context.proposals.create` | POST   | `/v1/data/projects/{projectId}/context/proposals`                     | Suggest an item for formal Project Context  |
| `data.context.proposals.review` | POST   | `/v1/data/projects/{projectId}/context/proposals/{proposalId}/review` | Human acceptance or rejection               |
| `data.home.get`                 | GET    | `/v1/data/projects/{projectId}/home`                                  | Read the typed project-home aggregate shell |

Object and activity lists use opaque cursor pagination. `type` filters the
object list by its stable `object_type`; clients must not inspect cursor
contents.

## Ownership and routing

`data_objects` is a searchable projection, not authoritative business state.
Its `(source_module, object_type, source_id)` key is unique, so an object keeps
the same `object_id` across projection updates.

Every business module contributing an object must register both:

1. an event projector that upserts its list/search projection idempotently;
2. an object-type Reader Adapter that loads the authoritative full content.

`data.list` reads projections. `data.read` first verifies project access, loads
the registry row, and dispatches to the registered Adapter. No cross-module
table read or whole-project dump is permitted. Current registrations are:

| Object type        | Projection source         | Full-content owner |
| ------------------ | ------------------------- | ------------------ |
| `project`          | `project.created`         | Project module     |
| `context-proposal` | Context Proposal mutation | Data Hub           |
| `project-context`  | Human acceptance          | Data Hub           |

The `datahub.projections` Event Bus consumer currently registers
`project.created` and `project.updated`. Its activity insert is unique by
`event_id`, making normal delivery and explicit replay idempotent.

## Project Context safety boundary

Project Context contains deliberate, shared team knowledge such as decisions,
assumptions, findings, and constraints. It is distinct from an Agent's private
working context.

- Owners, maintainers, editors, and Agents may create a proposal.
- Only an authenticated browser `session` with `project.context.review` may
  accept or reject it.
- Agent/API/Box credentials can never confirm a proposal, even if their role
  later gains broader permissions.
- Pending and rejected proposals never appear in `data.context.list`.
- Acceptance creates a new immutable stable context object and records the
  human reviewer and timestamp in the same transaction.

The relevant stable permissions are `project.data.read`,
`project.context.propose`, and `project.context.review`.

## Home aggregate shell

`data.home.get` currently returns typed, empty sections for `problem`,
`milestones`, `todos`, `models`, `experiments`, `article`, and `agent`.
`available` is false, `total` is zero, and `items` is an empty array. This
preserves the future response contract without pretending that later business
modules are implemented. Web BFF exposes it through the registered `home` page
aggregator.
