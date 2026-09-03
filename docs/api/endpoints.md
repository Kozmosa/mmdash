# API endpoint catalog

Stage 9 Article operations:

`article.blocks.review` and its BFF proxy accept the reviewed block's
`content_fingerprint`; a mismatch returns `ARTICLE_BLOCK_CHANGED` (409) so the
browser can synchronize before retrying. Repeating the operation for an
already-reviewed block withdraws the review and restores its previous tag.

- Core collaborative source and review: `article.get`, `article.draft.get`,
  `article.draft.flush`, `article.blocks.review`, `article.patches.list`, `article.patches.create`,
  `article.chapter_tags.list`, `article.chapter_tags.create`, `article.chapter_tags.get`,
  `article.chapter_tags.update`, `article.chapter_tags.delete`, and `article.chapter_tags.review`,
  `article.patches.review`, `article.references.list`,
  `article.references.create`, and `article.references.delete`.
- Core immutable history: `article.commits.list`, `article.commits.create`,
  `article.commit-operations.create`, `article.commit-operations.get`,
  `article.commits.get`,
  `article.commits.restore`, `article.builds.list`,
  `article.builds.create`, `article.preview_builds.create`,
  `article.builds.get`, `article.builds.retry`, `article.releases.list`,
  `article.releases.create`, and `article.releases.get`.
- Core publication, templates, Zotero, and Worker boundary:
  `article.publications.create`, `article.publication-operations.create`,
  `article.publications.retry`,
  `article.templates.list`, `article.templates.create`, `article.zotero.get`,
  `article.zotero.update`, `article.zotero.delete`, `article.zotero.search`,
  `article.zotero.collections`, `article.zotero.items`,
  `article.artifacts.describe`, `article.artifact_semantic_jobs.execute`,
  `article.build_jobs.input.get`, `article.build_jobs.progress.update`, and
  `article.build_jobs.outputs.upload`.
- Browser BFF: `bff.article.get`, `bff.article.collaboration.connect`,
  `bff.article.draft.get`, `bff.article.draft.flush`, `bff.article.blocks.review`,
  `bff.article.chapter_tags.list`, `bff.article.chapter_tags.create`, `bff.article.chapter_tags.get`,
  `bff.article.chapter_tags.update`, `bff.article.chapter_tags.delete`, and
  `bff.article.chapter_tags.review`,
  `bff.article.patches.list`, `bff.article.patches.create`,
  `bff.article.patches.review`, `bff.article.references.list`,
  `bff.article.references.create`, `bff.article.references.delete`,
  `bff.article.commits.list`, `bff.article.commits.create`,
  `bff.article.commit-operations.create`,
  `bff.article.commit-operations.get`, `bff.article.commits.get`,
  `bff.article.commits.restore`,
  `bff.article.builds.list`, `bff.article.builds.create`,
  `bff.article.preview_builds.create`, `bff.article.builds.get`,
  `bff.article.builds.retry`, `bff.article.publications.create`,
  `bff.article.publication-operations.create`,
  `bff.article.publications.retry`, `bff.article.releases.list`,
  `bff.article.releases.create`, `bff.article.releases.get`,
  `bff.article.templates.list`, `bff.article.templates.create`,
  `bff.article.zotero.get`, `bff.article.zotero.update`,
  `bff.article.zotero.delete`, `bff.article.zotero.search`,
  `bff.article.zotero.collections`, `bff.article.zotero.items`, and
  `bff.article.artifacts.describe`.

The collaboration operation upgrades a signed browser Session to Hocuspocus
WebSocket. BFF resolves Project permissions before attaching the room while
buffering initial protocol frames; all persistence is performed through Core.
Internal build and semantic operations require a live claimed Worker lease.

Stage 8 Experiment and Box operations:

- Core Experiment: `experiment.settings.get`, `experiment.settings.update`,
  `experiment.list`, `experiment.create`, `experiment.compare`,
  `experiment.get`, `experiment.run`, `experiment.cancel`,
  `experiment.archive`, `experiment.rerun`, `experiment.result.bind`,
  `experiment.logs.list`, `result.get`, `experiment.result_jobs.input.get`,
  and `experiment.result_jobs.finalize`.
- Core Box Control: `box.source.download`, `box.register`, `box.personal.list`, `box.personal.get`,
  `box.personal.update`, `box.revoke`, `box.heartbeat`, `box.project.list`,
  `box.project.assign`, `box.project.remove`, `box.tasks.claim`,
  `box.tasks.resume`, `box.tasks.logs.append_batch`, `box.tasks.status`,
  `box.tasks.result`, `box.tasks.artifact.upload`, and `box.releases.list`.
- Web BFF: `bff.experiment.list`, `bff.experiment.create`,
  `bff.experiment.compare`, `bff.experiment.get`, `bff.experiment.run`,
  `bff.experiment.cancel`, `bff.experiment.archive`, `bff.experiment.rerun`,
  `bff.experiment.result.bind`, `bff.experiment.settings.get`,
  `bff.experiment.settings.update`, `bff.experiment.logs.list`,
  `bff.experiment.logs.stream`, `bff.result.get`, `bff.box.personal.list`,
  `bff.box.personal.get`, `bff.box.personal.update`, `bff.box.revoke`,
  `bff.box.project.list`, `bff.box.project.assign`, `bff.box.project.remove`,
  and `bff.box.releases.list`.
- MCP Gateway and CLI expose the frozen `experiment.create`, `experiment.run`,
  `experiment.status`, `experiment.result.bind`, and `result.get` tools through
  the same Core RBAC and Audit boundary.
  Stage 7 Model operations:

- Core: `model.get`, `model.source.get`, `model.source.sync`,
  `model.notion.oauth.get`, `model.notion.oauth.start`,
  `model.notion.oauth.disconnect`, `model.notion.oauth.callback`,
  `model.questions.list`, `model.questions.create`, `model.questions.get`,
  `model.questions.update`, `model.questions.delete`, `model.questions.sync`,
  `model.snapshots.list`, `model.snapshots.get`, `model.snapshots.update`,
  `model.snapshots.diff`, and the leased-Worker-only `model.worker.export`.
- Web BFF: `bff.model.get`, `bff.model.source.get`,
  `bff.model.source.sync`, `bff.model.notion.oauth.get`,
  `bff.model.notion.oauth.start`, `bff.model.notion.oauth.disconnect`,
  `bff.model.notion.oauth.callback`, `bff.model.questions.list`,
  `bff.model.questions.create`, `bff.model.questions.get`,
  `bff.model.questions.update`, `bff.model.questions.delete`,
  `bff.model.questions.sync`, `bff.model.snapshots.list`,
  `bff.model.snapshots.get`, `bff.model.snapshots.update`, and
  `bff.model.snapshots.diff`.

Stage 5 Agent operations are documented in
[`agent.md`](agent.md). Core and Web BFF expose one-to-one instance, project
access, Token, Prompt, Session, message, Run, and Run-SSE routes under their
project-scoped Agent prefixes. The MCP catalog additionally exposes
`context.promote` through the existing Context Proposal boundary. Stage 6
extends the closed Agent Token scope with `progress.get`,
`progress.recalculate`, the attachment download grant `artifact.read`, and the
direct multipart `artifact.upload`; create and update contracts reject every
other Tool name.

Stage 4 and Stage 6 Progress operations:

- Core Stage 4: `progress.get`, `progress.milestones.list`,
  `progress.milestones.create`, `progress.milestones.update`,
  `progress.milestones.delete`,
  `progress.tasks.list`, `progress.tasks.create`, `progress.tasks.update`,
  `progress.tasks.delete`, `progress.dependencies.list`,
  `progress.dependencies.create`, `progress.dependencies.delete`,
  `progress.reminders.list`, `progress.reminders.create`,
  `progress.reminders.trigger`, `progress.proposals.list`,
  `progress.proposals.create`, `progress.proposals.review`,
  `progress.settings.get`, and `progress.settings.update`.
- Web BFF Stage 4: `bff.progress.get`, `bff.progress.milestones.list`,
  `bff.progress.milestones.create`, `bff.progress.milestones.update`,
  `bff.progress.milestones.delete`,
  `bff.progress.tasks.list`, `bff.progress.tasks.create`,
  `bff.progress.tasks.update`, `bff.progress.tasks.delete`,
  `bff.progress.dependencies.list`, `bff.progress.dependencies.create`,
  `bff.progress.dependencies.delete`, `bff.progress.reminders.list`,
  `bff.progress.reminders.create`, `bff.progress.reminders.trigger`,
  `bff.progress.proposals.list`, `bff.progress.proposals.create`,
  `bff.progress.proposals.review`, `bff.progress.settings.get`, and
  `bff.progress.settings.update`.
- Core adds `progress.proposals.batch_review`, `progress.recalculate`,
  `progress.evaluations.list`,
  `progress.evaluations.get`, `progress.evaluations.retry`,
  `progress.stage_override.set`, `progress.stage_override.clear`, and the
  leased-Worker-only `progress.worker.input` / `progress.worker.execute` to the
  existing Stage 4 Progress operations.
- Web BFF adds `bff.progress.proposals.batch_review`, `bff.progress.recalculate`,
  `bff.progress.evaluations.list`, `bff.progress.evaluations.get`,
  `bff.progress.evaluations.retry`, `bff.progress.stage_override.set`, and
  `bff.progress.stage_override.clear` to its existing one-to-one Progress
  routes.
- MCP Gateway exposes `progress.get` and `progress.recalculate` through exact
  Agent Tool grants and the same Core RBAC boundary.

`progress.settings.get` and `progress.settings.update` expose a project-owned
automatic evaluation policy. `cron_schedule` is a five-field UTC expression
evaluated by mmdash Core; `cron_next_run_at` and `cron_last_scheduled_at` report
Core scheduler state. These endpoints do not create or synchronize Hermes
Jobs. Each resulting evaluation records its Agent Session and Run provenance,
which the browser uses for the read-only live Session view.

Notification 3.17 operations:

- Core: `notification.inbox.list`, `notification.inbox.unread_count`, `notification.inbox.mark_all_read`, `notification.inbox.get`, `notification.inbox.update`, `notification.channels.get`, `notification.channels.update`, `notification.channels.delete`, `notification.channels.test`, `notification.rules.get`, `notification.rules.update`, `notification.deliveries.list`, and `notification.deliveries.retry`.
- Core project invitation action: `projects.invitations.accept_by_id`.
- Web BFF: `bff.notification.inbox.list`, `bff.notification.inbox.unread_count`, `bff.notification.inbox.mark_all_read`, `bff.notification.inbox.get`, `bff.notification.inbox.update`, `bff.notification.channels.get`, `bff.notification.channels.update`, `bff.notification.channels.delete`, `bff.notification.channels.test`, `bff.notification.rules.get`, `bff.notification.rules.update`, `bff.notification.deliveries.list`, `bff.notification.deliveries.retry`, plus `bff.projects.invitations.accept_by_id`.

Search this file by `operationId`, method/name, path, service, or module. The
catalog is the human-readable lookup index; the linked OpenAPI contract is the
machine-readable source of truth.

`repo.connect` and `bff.repo.connect` accept the optional, explicitly confirmed
`replace_disconnected` request flag. It authorizes removal of a different
disconnected binding's Core-managed local data and metadata before creating the
new binding. External GitHub/server-existing data is never deleted; replacing a
managed binding deletes that managed repository's authoritative Git data after
explicit confirmation. Issue #60 intentionally replaces the old `local`
provider enum with `server_existing`, backed by migration
`000050_repo_provider_paths`; `managed` is the new default product path.

Stage 3.16 adds the following account and collaboration operations:

- Core: `auth.register`, `auth.me.update`, `auth.me.password.update`,
  `auth.invitations.preview`, `auth.invitations.accept`,
  `auth.invitations.reject`,
  `projects.members.upsert`, `projects.invitations.list`,
  `projects.invitations.create`, and `projects.invitations.revoke`.
- Web BFF: `bff.auth.register`, `bff.auth.me.update`,
  `bff.auth.me.password.update`, `bff.auth.invitations.preview`,
  `bff.auth.invitations.accept`, `bff.auth.invitations.reject`,
  `bff.projects.members.upsert`,
  `bff.projects.invitations.list`, `bff.projects.invitations.create`, and
  `bff.projects.invitations.revoke`.

Stage 3 CLI adds Core `auth.refresh`, `auth.device.authorize`,
`auth.device.verify`, and `auth.device.token`, plus browser approval through
`bff.auth.device.verify`. MCP `project.list` and `project.get` are cataloged in
[`mcp-tools.md`](mcp-tools.md).

| Service     | Kind          | Method / name | Path                                                                                                                   | `operationId`                              | Module               | Contract                    |
| ----------- | ------------- | ------------- | ---------------------------------------------------------------------------------------------------------------------- | ------------------------------------------ | -------------------- | --------------------------- |
| Core        | HTTP          | `GET`         | `/health/live`                                                                                                         | `health.live`                              | platform health      | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/health/ready`                                                                                                        | `health.ready`                             | platform health      | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/openapi.yaml`                                                                                                        | `system.openapi.get`                       | platform contract    | `core.yaml`                 |
| Core        | Metrics       | `GET`         | `/metrics`                                                                                                             | `observability.metrics.get`                | platform metrics     | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/example`                                                                                                          | `example.check`                            | engineering baseline | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/auth/login`                                                                                                       | `auth.login`                               | auth                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/auth/refresh`                                                                                                     | `auth.refresh`                             | auth                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/auth/device/authorize`                                                                                            | `auth.device.authorize`                    | auth                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/auth/device/verify`                                                                                               | `auth.device.verify`                       | auth                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/auth/device/token`                                                                                                | `auth.device.token`                        | auth                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/auth/logout`                                                                                                      | `auth.logout`                              | auth                 | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/auth/me`                                                                                                          | `auth.me`                                  | auth                 | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/auth/tokens`                                                                                                      | `auth.tokens.list`                         | auth                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/auth/tokens`                                                                                                      | `auth.tokens.create`                       | auth                 | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/auth/tokens/{tokenId}`                                                                                            | `auth.tokens.revoke`                       | auth                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/auth/agent-tokens/{tokenId}/verification`                                                                         | `auth.agent_tokens.verification.record`    | auth                 | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects`                                                                                                         | `projects.list`                            | project              | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects`                                                                                                         | `projects.create`                          | project              | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/trash`                                                                                                   | `projects.trash.list`                      | project              | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}`                                                                                             | `projects.get`                             | project              | `core.yaml`                 |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}`                                                                                             | `projects.update`                          | project              | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}`                                                                                             | `projects.trash`                           | project              | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/restore`                                                                                     | `projects.restore`                         | project              | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/members`                                                                                     | `projects.members.list`                    | project              | `core.yaml`                 |
| Core        | HTTP          | `PUT`         | `/v1/projects/{projectId}/members/{userId}`                                                                            | `projects.members.upsert`                  | project              | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/members/{userId}`                                                                            | `projects.members.remove`                  | project              | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/permissions`                                                                                 | `projects.permissions.get`                 | project              | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/settings/types`                                                                                                   | `settings.types.list`                      | settings             | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/settings/system/{typeKey}`                                                                                        | `settings.system.get`                      | settings             | `core.yaml`                 |
| Core        | HTTP          | `PATCH`       | `/v1/settings/system/{typeKey}`                                                                                        | `settings.system.update`                   | settings             | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/settings/system/{typeKey}`                                                                                        | `settings.system.delete`                   | settings             | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/settings/system/{typeKey}/test`                                                                                   | `settings.system.test`                     | settings             | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/settings/projects/{projectId}/{typeKey}`                                                                          | `settings.projects.get`                    | settings             | `core.yaml`                 |
| Core        | HTTP          | `PATCH`       | `/v1/settings/projects/{projectId}/{typeKey}`                                                                          | `settings.projects.update`                 | settings             | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/settings/projects/{projectId}/{typeKey}`                                                                          | `settings.projects.delete`                 | settings             | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/settings/projects/{projectId}/{typeKey}/test`                                                                     | `settings.projects.test`                   | settings             | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/jobs`                                                                                                             | `jobs.create`                              | jobs                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/jobs/claim`                                                                                                       | `jobs.claim`                               | jobs                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/jobs/workers/heartbeat`                                                                                           | `jobs.workers.heartbeat`                   | jobs                 | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/jobs/{jobId}`                                                                                                     | `jobs.get`                                 | jobs                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/jobs/{jobId}/cancel`                                                                                              | `jobs.cancel`                              | jobs                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/jobs/{jobId}/heartbeat`                                                                                           | `jobs.lease.renew`                         | jobs                 | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/jobs/{jobId}/logs`                                                                                                | `jobs.logs.list`                           | jobs                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/jobs/{jobId}/logs`                                                                                                | `jobs.logs.append`                         | jobs                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/jobs/{jobId}/complete`                                                                                            | `jobs.complete`                            | jobs                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/jobs/{jobId}/fail`                                                                                                | `jobs.fail`                                | jobs                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/events/test`                                                                                                      | `events.test.emit`                         | events/outbox        | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/events/consumers`                                                                                                 | `events.consumers.list`                    | events/event bus     | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/events/{eventId}`                                                                                                 | `events.get`                               | events/outbox        | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/events/{eventId}/replay`                                                                                          | `events.replay`                            | events/outbox        | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/data/projects/{projectId}/objects`                                                                                | `data.list`                                | datahub              | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/data/projects/{projectId}/objects/{objectId}`                                                                     | `data.read`                                | datahub              | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/data/projects/{projectId}/activity`                                                                               | `data.activity.list`                       | datahub              | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/data/projects/{projectId}/context`                                                                                | `data.context.list`                        | datahub              | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/data/projects/{projectId}/context/proposals`                                                                      | `data.context.proposals.list`              | datahub              | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/data/projects/{projectId}/context/proposals`                                                                      | `data.context.proposals.create`            | datahub              | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/data/projects/{projectId}/context/proposals/{proposalId}/review`                                                  | `data.context.proposals.review`            | datahub              | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/data/projects/{projectId}/home`                                                                                   | `data.home.get`                            | datahub              | `core.yaml`                 |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/context/proposals`                                                                          | `bff.data.context.proposals.list`          | datahub              | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/context/proposals/{proposalId}/review`                                                      | `bff.data.context.proposals.review`        | datahub              | `web-bff.yaml`              |
| Core        | HTTP          | `GET`         | `/v1/audit/events`                                                                                                     | `audit.events.list`                        | audit                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/audit/events`                                                                                                     | `audit.events.record`                      | audit                | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/inbox`                                                                                                            | `notification.inbox.list`                  | notification         | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/inbox/unread-count`                                                                                               | `notification.inbox.unread_count`          | notification         | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/inbox/mark-all-read`                                                                                              | `notification.inbox.mark_all_read`         | notification         | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/inbox/{inboxItemId}`                                                                                              | `notification.inbox.get`                   | notification         | `core.yaml`                 |
| Core        | HTTP          | `PATCH`       | `/v1/inbox/{inboxItemId}`                                                                                              | `notification.inbox.update`                | notification         | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/notification-channels/{channelKey}`                                                          | `notification.channels.get`                | notification         | `core.yaml`                 |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}/notification-channels/{channelKey}`                                                          | `notification.channels.update`             | notification         | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/notification-channels/{channelKey}`                                                          | `notification.channels.delete`             | notification         | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/notification-channels/{channelKey}/test`                                                     | `notification.channels.test`               | notification         | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/notification-rules/{typeKey}`                                                                | `notification.rules.get`                   | notification         | `core.yaml`                 |
| Core        | HTTP          | `PUT`         | `/v1/projects/{projectId}/notification-rules/{typeKey}`                                                                | `notification.rules.update`                | notification         | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/notification-deliveries`                                                                     | `notification.deliveries.list`             | notification         | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/notification-deliveries/{deliveryId}/retry`                                                  | `notification.deliveries.retry`            | notification         | `core.yaml`                 |
| Web BFF     | HTTP          | `GET`         | `/health/live`                                                                                                         | `bff.health.live`                          | health               | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/health/ready`                                                                                                        | `bff.health.ready`                         | health               | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/example`                                                                                                         | `bff.example.check`                        | example proxy        | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/auth/login`                                                                                                      | `bff.auth.login`                           | auth                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/auth/device/verify`                                                                                              | `bff.auth.device.verify`                   | auth                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/auth/me`                                                                                                         | `bff.auth.me`                              | auth                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/auth/logout`                                                                                                     | `bff.auth.logout`                          | auth                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects`                                                                                                        | `bff.projects.list`                        | project              | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects`                                                                                                        | `bff.projects.create`                      | project              | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/trash`                                                                                                  | `bff.projects.trash.list`                  | project              | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}`                                                                                            | `bff.projects.get`                         | project              | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}`                                                                                            | `bff.projects.update`                      | project              | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}`                                                                                            | `bff.projects.trash`                       | project              | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/restore`                                                                                    | `bff.projects.restore`                     | project              | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/members`                                                                                    | `bff.projects.members.list`                | project              | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PUT`         | `/api/projects/{projectId}/members/{userId}`                                                                           | `bff.projects.members.upsert`              | project              | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/members/{userId}`                                                                           | `bff.projects.members.remove`              | project              | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/permissions`                                                                                | `bff.projects.permissions.get`             | project              | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/settings/types`                                                                                                  | `bff.settings.system.types.list`           | settings             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/settings/system/{typeKey}`                                                                                       | `bff.settings.system.get`                  | settings             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PATCH`       | `/api/settings/system/{typeKey}`                                                                                       | `bff.settings.system.update`               | settings             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/settings/system/{typeKey}`                                                                                       | `bff.settings.system.delete`               | settings             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/settings/system/{typeKey}/test`                                                                                  | `bff.settings.system.test`                 | settings             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/settings/types`                                                                             | `bff.settings.projects.types.list`         | settings             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/settings/{typeKey}`                                                                         | `bff.settings.projects.get`                | settings             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/settings/{typeKey}`                                                                         | `bff.settings.projects.update`             | settings             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/settings/{typeKey}`                                                                         | `bff.settings.projects.delete`             | settings             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/settings/{typeKey}/test`                                                                    | `bff.settings.projects.test`               | settings             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/inbox`                                                                                                           | `bff.notification.inbox.list`              | notification         | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/inbox/unread-count`                                                                                              | `bff.notification.inbox.unread_count`      | notification         | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/inbox/mark-all-read`                                                                                             | `bff.notification.inbox.mark_all_read`     | notification         | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/inbox/{inboxItemId}`                                                                                             | `bff.notification.inbox.get`               | notification         | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PATCH`       | `/api/inbox/{inboxItemId}`                                                                                             | `bff.notification.inbox.update`            | notification         | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/notification-channels/{channelKey}`                                                         | `bff.notification.channels.get`            | notification         | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/notification-channels/{channelKey}`                                                         | `bff.notification.channels.update`         | notification         | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/notification-channels/{channelKey}`                                                         | `bff.notification.channels.delete`         | notification         | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/notification-channels/{channelKey}/test`                                                    | `bff.notification.channels.test`           | notification         | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/notification-rules/{typeKey}`                                                               | `bff.notification.rules.get`               | notification         | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PUT`         | `/api/projects/{projectId}/notification-rules/{typeKey}`                                                               | `bff.notification.rules.update`            | notification         | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/notification-deliveries`                                                                    | `bff.notification.deliveries.list`         | notification         | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/notification-deliveries/{deliveryId}/retry`                                                 | `bff.notification.deliveries.retry`        | notification         | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/pages/{pageId}`                                                                             | `bff.page.get`                             | page aggregation     | `web-bff.yaml`              |
| Web BFF     | SSE           | `GET`         | `/api/projects/{projectId}/events`                                                                                     | `bff.events.stream`                        | stream proxy         | `web-bff.yaml`              |
| Web BFF     | WebSocket     | `CONNECT`     | `/api/projects/{projectId}/socket`                                                                                     | `bff.socket.connect`                       | stream proxy         | `web-bff.yaml`              |
| Web BFF     | File stream   | `GET`         | `/api/projects/{projectId}/files/{filePath}`                                                                           | `bff.file.download`                        | file proxy           | `web-bff.yaml`              |
| Web BFF     | File metadata | `HEAD`        | `/api/projects/{projectId}/files/{filePath}`                                                                           | `bff.file.head`                            | file proxy           | `web-bff.yaml`              |
| Web BFF     | File stream   | `PUT`         | `/api/projects/{projectId}/files/{filePath}`                                                                           | `bff.file.upload`                          | file proxy           | `web-bff.yaml`              |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models`                                                                                      | `model.get`                                | model                | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models/source`                                                                               | `model.source.get`                         | model                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/models/source/sync`                                                                          | `model.source.sync`                        | model                | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models/notion/oauth`                                                                         | `model.notion.oauth.get`                   | model                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/models/notion/oauth/authorizations`                                                          | `model.notion.oauth.start`                 | model                | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/models/notion/oauth/connection`                                                              | `model.notion.oauth.disconnect`            | model                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/model-notion/oauth/callback`                                                                                      | `model.notion.oauth.callback`              | model                | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models/questions`                                                                            | `model.questions.list`                     | model                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/models/questions`                                                                            | `model.questions.create`                   | model                | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models/questions/{questionId}`                                                               | `model.questions.get`                      | model                | `core.yaml`                 |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}/models/questions/{questionId}`                                                               | `model.questions.update`                   | model                | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/models/questions/{questionId}`                                                               | `model.questions.delete`                   | model                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/models/questions/{questionId}/sync`                                                          | `model.questions.sync`                     | model                | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models/questions/{questionId}/snapshots`                                                     | `model.snapshots.list`                     | model                | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models/questions/{questionId}/snapshots/{snapshotId}`                                        | `model.snapshots.get`                      | model                | `core.yaml`                 |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}/models/questions/{questionId}/snapshots/{snapshotId}`                                        | `model.snapshots.update`                   | model                | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models/questions/{questionId}/diff`                                                          | `model.snapshots.diff`                     | model                | `core.yaml`                 |
| Core        | Internal HTTP | `GET`         | `/v1/internal/model-notion-jobs/{jobId}/export`                                                                        | `model.worker.export`                      | model                | `core.yaml`                 |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models`                                                                                     | `bff.model.get`                            | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models/source`                                                                              | `bff.model.source.get`                     | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/models/source/sync`                                                                         | `bff.model.source.sync`                    | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models/notion/oauth`                                                                        | `bff.model.notion.oauth.get`               | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/models/notion/oauth/authorizations`                                                         | `bff.model.notion.oauth.start`             | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/models/notion/oauth/connection`                                                             | `bff.model.notion.oauth.disconnect`        | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/integrations/notion/oauth/callback`                                                                              | `bff.model.notion.oauth.callback`          | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models/questions`                                                                           | `bff.model.questions.list`                 | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/models/questions`                                                                           | `bff.model.questions.create`               | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models/questions/{questionId}`                                                              | `bff.model.questions.get`                  | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/models/questions/{questionId}`                                                              | `bff.model.questions.update`               | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/models/questions/{questionId}`                                                              | `bff.model.questions.delete`               | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/models/questions/{questionId}/sync`                                                         | `bff.model.questions.sync`                 | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models/questions/{questionId}/snapshots`                                                    | `bff.model.snapshots.list`                 | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models/questions/{questionId}/snapshots/{snapshotId}`                                       | `bff.model.snapshots.get`                  | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/models/questions/{questionId}/snapshots/{snapshotId}`                                       | `bff.model.snapshots.update`               | model                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models/questions/{questionId}/diff`                                                         | `bff.model.snapshots.diff`                 | model                | `web-bff.yaml`              |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository`                                                                                  | `repo.get`                                 | repo                 | `core.yaml`                 |
| Core        | HTTP          | `PUT`         | `/v1/projects/{projectId}/repository`                                                                                  | `repo.connect`                             | repo                 | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/repository`                                                                                  | `repo.disconnect`                          | repo                 | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository/capabilities`                                                                     | `repo.capabilities.get`                    | repo                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/repository/test`                                                                             | `repo.connection.test`                     | repo                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/repository/sync`                                                                             | `repo.sync.request`                        | repo                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/repository/webhook-secret`                                                                   | `repo.webhook-secret.rotate`               | repo                 | `core.yaml`                 |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}/repository/workspaces`                                                                       | `repo.workspaces.update`                   | repo                 | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository/branches`                                                                         | `repo.branches.list`                       | repo                 | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository/commits`                                                                          | `repo.commits.list`                        | repo                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/repository/commits`                                                                          | `repo.commits.create`                      | repo                 | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository/commits/{commitSha}`                                                              | `repo.commits.get`                         | repo                 | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository/tree`                                                                             | `repo.tree.list`                           | repo                 | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository/content`                                                                          | `repo.content.get`                         | repo                 | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository/raw`                                                                              | `repo.content.raw`                         | repo                 | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/repository/checkouts`                                                                        | `repo.checkouts.create`                    | repo                 | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository/checkouts/{checkoutId}`                                                           | `repo.checkouts.get`                       | repo                 | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/repository/checkouts/{checkoutId}`                                                           | `repo.checkouts.release`                   | repo                 | `core.yaml`                 |
| Core        | Webhook       | `POST`        | `/v1/repo/webhooks/github/{hookId}`                                                                                    | `repo.webhooks.github`                     | repo                 | `core.yaml`                 |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/repository`                                                                                 | `bff.repo.get`                             | repo                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PUT`         | `/api/projects/{projectId}/repository`                                                                                 | `bff.repo.connect`                         | repo                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/repository`                                                                                 | `bff.repo.disconnect`                      | repo                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/repository/capabilities`                                                                    | `bff.repo.capabilities.get`                | repo                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/repository/test`                                                                            | `bff.repo.connection.test`                 | repo                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/repository/sync`                                                                            | `bff.repo.sync.request`                    | repo                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/repository/webhook-secret`                                                                  | `bff.repo.webhook-secret.rotate`           | repo                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/repository/workspaces`                                                                      | `bff.repo.workspaces.update`               | repo                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/repository/branches`                                                                        | `bff.repo.branches.list`                   | repo                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/repository/commits`                                                                         | `bff.repo.commits.list`                    | repo                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/repository/commits/{commitSha}`                                                             | `bff.repo.commits.get`                     | repo                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/repository/tree`                                                                            | `bff.repo.tree.list`                       | repo                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/repository/content`                                                                         | `bff.repo.content.get`                     | repo                 | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/repository/raw`                                                                             | `bff.repo.content.raw`                     | repo                 | `web-bff.yaml`              |
| Web BFF     | Webhook       | `POST`        | `/api/webhooks/github/{hookId}`                                                                                        | `bff.repo.webhooks.github`                 | repo                 | `web-bff.yaml`              |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/uploads`                                                                           | `artifact.uploads.initialize`              | artifact             | `core.yaml`                 |
| Core        | Internal HTTP | `POST`        | `/v1/projects/{projectId}/artifacts/agent-uploads`                                                                     | `artifact.agentUploads.initialize`         | artifact             | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/artifacts/uploads/{uploadId}`                                                                | `artifact.uploads.get`                     | artifact             | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/artifacts/uploads/{uploadId}`                                                                | `artifact.uploads.abort`                   | artifact             | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/uploads/{uploadId}/parts/sign`                                                     | `artifact.uploads.parts.sign`              | artifact             | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/uploads/{uploadId}/confirm`                                                        | `artifact.uploads.confirm`                 | artifact             | `core.yaml`                 |
| Core        | File stream   | `GET`         | `/v1/artifact-transfers/{signedToken}`                                                                                 | `artifact.transfers.get`                   | artifact             | `core.yaml`                 |
| Core        | File stream   | `PUT`         | `/v1/artifact-transfers/{signedToken}`                                                                                 | `artifact.transfers.put`                   | artifact             | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/artifacts`                                                                                   | `artifact.list`                            | artifact             | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/artifacts/folders`                                                                           | `artifact.folders.list`                    | artifact             | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/folders`                                                                           | `artifact.folders.create`                  | artifact             | `core.yaml`                 |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}/artifacts/folders/{folderId}`                                                                | `artifact.folders.rename`                  | artifact             | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/folders/{folderId}/move`                                                           | `artifact.folders.move`                    | artifact             | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/artifacts/folders/{folderId}`                                                                | `artifact.folders.delete`                  | artifact             | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/artifacts/trash`                                                                             | `artifact.trash.list`                      | artifact             | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/artifacts/{artifactId}`                                                                      | `artifact.get`                             | artifact             | `core.yaml`                 |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}/artifacts/{artifactId}`                                                                      | `artifact.update`                          | artifact             | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/artifacts/{artifactId}`                                                                      | `artifact.trash`                           | artifact             | `core.yaml`                 |
| Core        | HTTP          | `PUT`         | `/v1/projects/{projectId}/artifacts/{artifactId}/folder`                                                               | `artifact.folder.move`                     | artifact             | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/artifacts/{artifactId}/versions`                                                             | `artifact.versions.list`                   | artifact             | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/{artifactId}/versions/uploads`                                                     | `artifact.versions.uploads.initialize`     | artifact             | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/{artifactId}/versions/{versionId}/restore`                                         | `artifact.versions.restore`                | artifact             | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/{artifactId}/download`                                                             | `artifact.download`                        | artifact             | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/{artifactId}/versions/{versionId}/download`                                        | `artifact.versions.download`               | artifact             | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/artifacts/{artifactId}/versions/{versionId}/previews`                                        | `artifact.previews.list`                   | artifact             | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/{artifactId}/restore`                                                              | `artifact.restore`                         | artifact             | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/artifacts/{artifactId}/purge`                                                                | `artifact.purge`                           | artifact             | `core.yaml`                 |
| Core        | Internal HTTP | `POST`        | `/v1/internal/artifact-preview-jobs/{jobId}/transfers`                                                                 | `artifact.preview-jobs.transfers.create`   | artifact             | `core.yaml`                 |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/uploads`                                                                          | `bff.artifact.uploads.initialize`          | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/artifacts/uploads/{uploadId}`                                                               | `bff.artifact.uploads.get`                 | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/artifacts/uploads/{uploadId}`                                                               | `bff.artifact.uploads.abort`               | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/uploads/{uploadId}/parts/sign`                                                    | `bff.artifact.uploads.parts.sign`          | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/uploads/{uploadId}/confirm`                                                       | `bff.artifact.uploads.confirm`             | artifact             | `web-bff.yaml`              |
| Web BFF     | File stream   | `GET`         | `/api/artifact-transfers/{signedToken}`                                                                                | `bff.artifact.transfers.get`               | artifact             | `web-bff.yaml`              |
| Web BFF     | File stream   | `PUT`         | `/api/artifact-transfers/{signedToken}`                                                                                | `bff.artifact.transfers.put`               | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/artifacts`                                                                                  | `bff.artifact.list`                        | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/artifacts/folders`                                                                          | `bff.artifact.folders.list`                | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/folders`                                                                          | `bff.artifact.folders.create`              | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/artifacts/folders/{folderId}`                                                               | `bff.artifact.folders.rename`              | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/folders/{folderId}/move`                                                          | `bff.artifact.folders.move`                | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/artifacts/folders/{folderId}`                                                               | `bff.artifact.folders.delete`              | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/artifacts/trash`                                                                            | `bff.artifact.trash.list`                  | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/artifacts/{artifactId}`                                                                     | `bff.artifact.get`                         | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/artifacts/{artifactId}`                                                                     | `bff.artifact.update`                      | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/artifacts/{artifactId}`                                                                     | `bff.artifact.trash`                       | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PUT`         | `/api/projects/{projectId}/artifacts/{artifactId}/folder`                                                              | `bff.artifact.folder.move`                 | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/artifacts/{artifactId}/versions`                                                            | `bff.artifact.versions.list`               | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/{artifactId}/versions/uploads`                                                    | `bff.artifact.versions.uploads.initialize` | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/{artifactId}/versions/{versionId}/restore`                                        | `bff.artifact.versions.restore`            | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/{artifactId}/download`                                                            | `bff.artifact.download`                    | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/{artifactId}/versions/{versionId}/download`                                       | `bff.artifact.versions.download`           | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/artifacts/{artifactId}/versions/{versionId}/previews`                                       | `bff.artifact.previews.list`               | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/{artifactId}/restore`                                                             | `bff.artifact.restore`                     | artifact             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/artifacts/{artifactId}/purge`                                                               | `bff.artifact.purge`                       | artifact             | `web-bff.yaml`              |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/agent-instances`                                                                             | `agent.instances.list`                     | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances`                                                                             | `agent.instances.create`                   | agent                | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}`                                                           | `agent.instances.get`                      | agent                | `core.yaml`                 |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}`                                                           | `agent.instances.update`                   | agent                | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}`                                                           | `agent.instances.disable`                  | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/checks`                                                    | `agent.instances.check`                    | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/project-access/verify`                                     | `agent.project_access.verify`              | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/tokens/rotate`                                             | `agent.tokens.rotate`                      | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/tokens/{tokenId}/verify`                                   | `agent.tokens.verify`                      | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/tokens/{tokenId}/abort`                                    | `agent.tokens.abort`                       | agent                | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/tokens/{tokenId}`                                          | `agent.tokens.revoke`                      | agent                | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/prompt`                                                    | `agent.prompt.get`                         | agent                | `core.yaml`                 |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/prompt`                                                    | `agent.prompt.update`                      | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/prompt/reset`                                              | `agent.prompt.reset`                       | agent                | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions`                                                  | `agent.sessions.list`                      | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions`                                                  | `agent.sessions.create`                    | agent                | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}`                                      | `agent.sessions.get`                       | agent                | `core.yaml`                 |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}`                                      | `agent.sessions.update`                    | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/end`                                  | `agent.sessions.end`                       | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/continue`                             | `agent.sessions.continue`                  | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/fork`                                 | `agent.sessions.fork`                      | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/default`                              | `agent.sessions.default`                   | agent                | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/messages`                             | `agent.sessions.messages.list`             | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/runs`                                 | `agent.runs.start`                         | agent                | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/runs/{runId}`                         | `agent.runs.get`                           | agent                | `core.yaml`                 |
| Core        | SSE           | `GET`         | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/runs/{runId}/events`                  | `agent.runs.events`                        | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/runs/{runId}/approvals/{approvalId}`  | `agent.runs.approve`                       | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/runs/{runId}/stop`                    | `agent.runs.stop`                          | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/runs/{runId}/regenerate`              | `agent.runs.regenerate`                    | agent                | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/runs/{runId}/rerun`                   | `agent.runs.rerun`                         | agent                | `core.yaml`                 |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/agent-instances`                                                                            | `bff.agent.instances.list`                 | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances`                                                                            | `bff.agent.instances.create`               | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/agent-instances/{agentInstanceId}`                                                          | `bff.agent.instances.get`                  | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/agent-instances/{agentInstanceId}`                                                          | `bff.agent.instances.update`               | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/agent-instances/{agentInstanceId}`                                                          | `bff.agent.instances.disable`              | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/checks`                                                   | `bff.agent.instances.check`                | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/project-access/verify`                                    | `bff.agent.project_access.verify`          | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/tokens/rotate`                                            | `bff.agent.tokens.rotate`                  | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/tokens/{tokenId}/verify`                                  | `bff.agent.tokens.verify`                  | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/tokens/{tokenId}/abort`                                   | `bff.agent.tokens.abort`                   | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/tokens/{tokenId}`                                         | `bff.agent.tokens.revoke`                  | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/prompt`                                                   | `bff.agent.prompt.get`                     | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/prompt`                                                   | `bff.agent.prompt.update`                  | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/prompt/reset`                                             | `bff.agent.prompt.reset`                   | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions`                                                 | `bff.agent.sessions.list`                  | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions`                                                 | `bff.agent.sessions.create`                | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}`                                     | `bff.agent.sessions.get`                   | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}`                                     | `bff.agent.sessions.update`                | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/end`                                 | `bff.agent.sessions.end`                   | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/continue`                            | `bff.agent.sessions.continue`              | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/fork`                                | `bff.agent.sessions.fork`                  | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/default`                             | `bff.agent.sessions.default`               | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/messages`                            | `bff.agent.sessions.messages.list`         | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/runs`                                | `bff.agent.runs.start`                     | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/runs/{runId}`                        | `bff.agent.runs.get`                       | agent                | `web-bff.yaml`              |
| Web BFF     | SSE           | `GET`         | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/runs/{runId}/events`                 | `bff.agent.runs.events`                    | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/runs/{runId}/approvals/{approvalId}` | `bff.agent.runs.approve`                   | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/runs/{runId}/stop`                   | `bff.agent.runs.stop`                      | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/runs/{runId}/regenerate`             | `bff.agent.runs.regenerate`                | agent                | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/agent-instances/{agentInstanceId}/sessions/{sessionId}/runs/{runId}/rerun`                  | `bff.agent.runs.rerun`                     | agent                | `web-bff.yaml`              |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/progress/proposals/batch-review`                                                             | `progress.proposals.batch_review`          | progress             | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/progress/recalculate`                                                                        | `progress.recalculate`                     | progress tracking    | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/progress/evaluations`                                                                        | `progress.evaluations.list`                | progress tracking    | `core.yaml`                 |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/progress/evaluations/{evaluationId}`                                                         | `progress.evaluations.get`                 | progress tracking    | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/progress/evaluations/{evaluationId}/retry`                                                   | `progress.evaluations.retry`               | progress tracking    | `core.yaml`                 |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/progress/stage-override`                                                                     | `progress.stage_override.set`              | progress tracking    | `core.yaml`                 |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/progress/stage-override`                                                                     | `progress.stage_override.clear`            | progress tracking    | `core.yaml`                 |
| Core        | Internal HTTP | `GET`         | `/v1/internal/progress-evaluation-jobs/{jobId}/input`                                                                  | `progress.worker.input`                    | progress tracking    | `core.yaml`                 |
| Core        | Internal HTTP | `POST`        | `/v1/internal/progress-evaluation-jobs/{jobId}/execute`                                                                | `progress.worker.execute`                  | progress tracking    | `core.yaml`                 |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/progress/proposals/batch-review`                                                            | `bff.progress.proposals.batch_review`      | progress             | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/progress/recalculate`                                                                       | `bff.progress.recalculate`                 | progress tracking    | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/progress/evaluations`                                                                       | `bff.progress.evaluations.list`            | progress tracking    | `web-bff.yaml`              |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/progress/evaluations/{evaluationId}`                                                        | `bff.progress.evaluations.get`             | progress tracking    | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/progress/evaluations/{evaluationId}/retry`                                                  | `bff.progress.evaluations.retry`           | progress tracking    | `web-bff.yaml`              |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/progress/stage-override`                                                                    | `bff.progress.stage_override.set`          | progress tracking    | `web-bff.yaml`              |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/progress/stage-override`                                                                    | `bff.progress.stage_override.clear`        | progress tracking    | `web-bff.yaml`              |
| MCP Gateway | MCP Tool      | `tools/call`  | `/mcp`                                                                                                                 | `project.member.list`                      | project              | `project.member.list.json`  |
| MCP Gateway | MCP Tool      | `tools/call`  | `/mcp`                                                                                                                 | `project.member.get`                       | project              | `project.member.get.json`   |
| MCP Gateway | MCP Tool      | `tools/call`  | `/mcp`                                                                                                                 | `context.promote`                          | datahub proposal     | `context.promote.json`      |
| MCP Gateway | MCP Tool      | `tools/call`  | `/mcp`                                                                                                                 | `progress.get`                             | progress tracking    | `progress.get.json`         |
| MCP Gateway | MCP Tool      | `tools/call`  | `/mcp`                                                                                                                 | `progress.recalculate`                     | progress tracking    | `progress.recalculate.json` |
| MCP Gateway | MCP Tool      | `tools/call`  | `/mcp`                                                                                                                 | `artifact.read`                            | artifact             | `artifact.read.json`        |
| MCP Gateway | MCP Tool      | `tools/call`  | `/mcp`                                                                                                                 | `artifact.upload`                          | artifact             | `artifact.upload.json`      |
| MCP Gateway | MCP Tool      | `tools/call`  | `/mcp`                                                                                                                 | `system.echo`                              | engineering baseline | `system.echo.json`          |

## Browser BFF conventions

- Except for health and engineering checks, BFF operations require the signed
  `mmdash_session` browser cookie.
- Project context comes from the path, `project_id` query parameter, or
  `x-mmdash-project-id`; multiple different values yield
  `PROJECT_CONTEXT_CONFLICT`.
- Every response carries `x-request-id`. Error JSON contains `code`, a safe
  `message`, and `request_id`.
- SSE and file bodies pass through without application buffering. The
  WebSocket route proxies frames bidirectionally.
- `/api/projects/{projectId}/pages/{pageId}` is an extension point. The
  `workspace-shell` aggregator returns browser-safe user/project context; the
  `home` aggregator delegates its typed aggregate to Core Data Hub.

## Core platform conventions

- Core is the authoritative state boundary. Other processes call its
  contract and do not connect to PostgreSQL or object storage directly.
- `GET /health/live` only checks the process. `GET /health/ready` checks both
  `postgres` and `object_storage`, returning HTTP 503 and `not_ready` when
  either is unavailable.
- `GET /openapi.yaml` serves the canonical contract configured at process
  startup, so operators and generated clients can be compared against the
  running service.
- All Core JSON errors carry stable `code` and `message` fields; request-bound
  errors also carry `request_id`. Internal causes are never serialized.
- Worker operations (`jobs.claim`, Worker heartbeat, lease renewal, log append,
  completion, and failure) require an Auth-issued API token. The Python Worker
  calls these operations and never connects to PostgreSQL.
- Audit rows are append-only and searchable by request, actor, project,
  category, action, outcome, and source. `/metrics` uses bounded labels only.

## `example.check`

Verifies the complete engineering-baseline path. It executes
`SELECT CURRENT_TIMESTAMP` through the Core PostgreSQL pool and returns:

```json
{
  "status": "ok",
  "storage": "postgres",
  "checked_at": "2026-07-28T12:00:00Z"
}
```

This is an engineering endpoint rather than a product-domain API. It may be
removed only after a replacement smoke path is documented.
