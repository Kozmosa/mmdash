# API endpoint catalog

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

Stage 4 Progress operations:

- Core: `progress.get`, `progress.milestones.list`, `progress.milestones.create`, `progress.milestones.update`, `progress.tasks.list`, `progress.tasks.create`, `progress.tasks.update`, `progress.tasks.delete`, `progress.dependencies.list`, `progress.dependencies.create`, `progress.dependencies.delete`, `progress.reminders.list`, `progress.reminders.create`, `progress.reminders.trigger`, `progress.proposals.list`, `progress.proposals.create`, `progress.proposals.review`, `progress.settings.get`, and `progress.settings.update`.
- Web BFF: `bff.progress.get`, `bff.progress.milestones.list`, `bff.progress.milestones.create`, `bff.progress.milestones.update`, `bff.progress.tasks.list`, `bff.progress.tasks.create`, `bff.progress.tasks.update`, `bff.progress.tasks.delete`, `bff.progress.dependencies.list`, `bff.progress.dependencies.create`, `bff.progress.dependencies.delete`, `bff.progress.reminders.list`, `bff.progress.reminders.create`, `bff.progress.reminders.trigger`, `bff.progress.proposals.list`, `bff.progress.proposals.create`, `bff.progress.proposals.review`, `bff.progress.settings.get`, and `bff.progress.settings.update`.

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
new binding; remote Git data is never deleted.

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

| Service     | Kind          | Method / name | Path                                                                             | `operationId`                              | Module               | Contract           |
| ----------- | ------------- | ------------- | -------------------------------------------------------------------------------- | ------------------------------------------ | -------------------- | ------------------ |
| Core        | HTTP          | `GET`         | `/health/live`                                                                   | `health.live`                              | platform health      | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/health/ready`                                                                  | `health.ready`                             | platform health      | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/openapi.yaml`                                                                  | `system.openapi.get`                       | platform contract    | `core.yaml`        |
| Core        | Metrics       | `GET`         | `/metrics`                                                                       | `observability.metrics.get`                | platform metrics     | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/example`                                                                    | `example.check`                            | engineering baseline | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/auth/login`                                                                 | `auth.login`                               | auth                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/auth/refresh`                                                               | `auth.refresh`                             | auth                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/auth/device/authorize`                                                      | `auth.device.authorize`                    | auth                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/auth/device/verify`                                                         | `auth.device.verify`                       | auth                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/auth/device/token`                                                          | `auth.device.token`                        | auth                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/auth/logout`                                                                | `auth.logout`                              | auth                 | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/auth/me`                                                                    | `auth.me`                                  | auth                 | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/auth/tokens`                                                                | `auth.tokens.list`                         | auth                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/auth/tokens`                                                                | `auth.tokens.create`                       | auth                 | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/auth/tokens/{tokenId}`                                                      | `auth.tokens.revoke`                       | auth                 | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects`                                                                   | `projects.list`                            | project              | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects`                                                                   | `projects.create`                          | project              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/trash`                                                             | `projects.trash.list`                      | project              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}`                                                       | `projects.get`                             | project              | `core.yaml`        |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}`                                                       | `projects.update`                          | project              | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}`                                                       | `projects.trash`                           | project              | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/restore`                                               | `projects.restore`                         | project              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/members`                                               | `projects.members.list`                    | project              | `core.yaml`        |
| Core        | HTTP          | `PUT`         | `/v1/projects/{projectId}/members/{userId}`                                      | `projects.members.upsert`                  | project              | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/members/{userId}`                                      | `projects.members.remove`                  | project              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/permissions`                                           | `projects.permissions.get`                 | project              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/settings/types`                                                             | `settings.types.list`                      | settings             | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/settings/system/{typeKey}`                                                  | `settings.system.get`                      | settings             | `core.yaml`        |
| Core        | HTTP          | `PATCH`       | `/v1/settings/system/{typeKey}`                                                  | `settings.system.update`                   | settings             | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/settings/system/{typeKey}`                                                  | `settings.system.delete`                   | settings             | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/settings/system/{typeKey}/test`                                             | `settings.system.test`                     | settings             | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/settings/projects/{projectId}/{typeKey}`                                    | `settings.projects.get`                    | settings             | `core.yaml`        |
| Core        | HTTP          | `PATCH`       | `/v1/settings/projects/{projectId}/{typeKey}`                                    | `settings.projects.update`                 | settings             | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/settings/projects/{projectId}/{typeKey}`                                    | `settings.projects.delete`                 | settings             | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/settings/projects/{projectId}/{typeKey}/test`                               | `settings.projects.test`                   | settings             | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/jobs`                                                                       | `jobs.create`                              | jobs                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/jobs/claim`                                                                 | `jobs.claim`                               | jobs                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/jobs/workers/heartbeat`                                                     | `jobs.workers.heartbeat`                   | jobs                 | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/jobs/{jobId}`                                                               | `jobs.get`                                 | jobs                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/jobs/{jobId}/cancel`                                                        | `jobs.cancel`                              | jobs                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/jobs/{jobId}/heartbeat`                                                     | `jobs.lease.renew`                         | jobs                 | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/jobs/{jobId}/logs`                                                          | `jobs.logs.list`                           | jobs                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/jobs/{jobId}/logs`                                                          | `jobs.logs.append`                         | jobs                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/jobs/{jobId}/complete`                                                      | `jobs.complete`                            | jobs                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/jobs/{jobId}/fail`                                                          | `jobs.fail`                                | jobs                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/events/test`                                                                | `events.test.emit`                         | events/outbox        | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/events/consumers`                                                           | `events.consumers.list`                    | events/event bus     | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/events/{eventId}`                                                           | `events.get`                               | events/outbox        | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/events/{eventId}/replay`                                                    | `events.replay`                            | events/outbox        | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/data/projects/{projectId}/objects`                                          | `data.list`                                | datahub              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/data/projects/{projectId}/objects/{objectId}`                               | `data.read`                                | datahub              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/data/projects/{projectId}/activity`                                         | `data.activity.list`                       | datahub              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/data/projects/{projectId}/context`                                          | `data.context.list`                        | datahub              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/data/projects/{projectId}/context/proposals`                                | `data.context.proposals.list`              | datahub              | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/data/projects/{projectId}/context/proposals`                                | `data.context.proposals.create`            | datahub              | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/data/projects/{projectId}/context/proposals/{proposalId}/review`            | `data.context.proposals.review`            | datahub              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/data/projects/{projectId}/home`                                             | `data.home.get`                            | datahub              | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/audit/events`                                                               | `audit.events.list`                        | audit                | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/audit/events`                                                               | `audit.events.record`                      | audit                | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/inbox`                                                                     | `notification.inbox.list`                 | notification         | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/inbox/unread-count`                                                        | `notification.inbox.unread_count`         | notification         | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/inbox/mark-all-read`                                                       | `notification.inbox.mark_all_read`        | notification         | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/inbox/{inboxItemId}`                                                       | `notification.inbox.get`                  | notification         | `core.yaml`        |
| Core        | HTTP          | `PATCH`       | `/v1/inbox/{inboxItemId}`                                                       | `notification.inbox.update`               | notification         | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/notification-channels/{channelKey}`                   | `notification.channels.get`               | notification         | `core.yaml`        |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}/notification-channels/{channelKey}`                   | `notification.channels.update`            | notification         | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/notification-channels/{channelKey}`                   | `notification.channels.delete`            | notification         | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/notification-channels/{channelKey}/test`              | `notification.channels.test`              | notification         | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/notification-rules/{typeKey}`                         | `notification.rules.get`                  | notification         | `core.yaml`        |
| Core        | HTTP          | `PUT`         | `/v1/projects/{projectId}/notification-rules/{typeKey}`                         | `notification.rules.update`               | notification         | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/notification-deliveries`                              | `notification.deliveries.list`            | notification         | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/notification-deliveries/{deliveryId}/retry`           | `notification.deliveries.retry`           | notification         | `core.yaml`        |
| Web BFF     | HTTP          | `GET`         | `/health/live`                                                                   | `bff.health.live`                          | health               | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/health/ready`                                                                  | `bff.health.ready`                         | health               | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/example`                                                                   | `bff.example.check`                        | example proxy        | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/auth/login`                                                                | `bff.auth.login`                           | auth                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/auth/device/verify`                                                        | `bff.auth.device.verify`                   | auth                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/auth/me`                                                                   | `bff.auth.me`                              | auth                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/auth/logout`                                                               | `bff.auth.logout`                          | auth                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects`                                                                  | `bff.projects.list`                        | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects`                                                                  | `bff.projects.create`                      | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/trash`                                                            | `bff.projects.trash.list`                  | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}`                                                      | `bff.projects.get`                         | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}`                                                      | `bff.projects.update`                      | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}`                                                      | `bff.projects.trash`                       | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/restore`                                              | `bff.projects.restore`                     | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/members`                                              | `bff.projects.members.list`                | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PUT`         | `/api/projects/{projectId}/members/{userId}`                                     | `bff.projects.members.upsert`              | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/members/{userId}`                                     | `bff.projects.members.remove`              | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/permissions`                                          | `bff.projects.permissions.get`             | project              | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/settings/types`                                                            | `bff.settings.system.types.list`           | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/settings/system/{typeKey}`                                                 | `bff.settings.system.get`                  | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PATCH`       | `/api/settings/system/{typeKey}`                                                 | `bff.settings.system.update`               | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `DELETE`      | `/api/settings/system/{typeKey}`                                                 | `bff.settings.system.delete`               | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/settings/system/{typeKey}/test`                                            | `bff.settings.system.test`                 | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/settings/types`                                       | `bff.settings.projects.types.list`         | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/settings/{typeKey}`                                   | `bff.settings.projects.get`                | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/settings/{typeKey}`                                   | `bff.settings.projects.update`             | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/settings/{typeKey}`                                   | `bff.settings.projects.delete`             | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/settings/{typeKey}/test`                              | `bff.settings.projects.test`               | settings             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/inbox`                                                                    | `bff.notification.inbox.list`             | notification         | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/inbox/unread-count`                                                       | `bff.notification.inbox.unread_count`    | notification         | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/inbox/mark-all-read`                                                      | `bff.notification.inbox.mark_all_read`   | notification         | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/inbox/{inboxItemId}`                                                      | `bff.notification.inbox.get`             | notification         | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PATCH`       | `/api/inbox/{inboxItemId}`                                                      | `bff.notification.inbox.update`          | notification         | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/notification-channels/{channelKey}`                  | `bff.notification.channels.get`          | notification         | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/notification-channels/{channelKey}`                  | `bff.notification.channels.update`       | notification         | `web-bff.yaml`     |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/notification-channels/{channelKey}`                  | `bff.notification.channels.delete`       | notification         | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/notification-channels/{channelKey}/test`             | `bff.notification.channels.test`         | notification         | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/notification-rules/{typeKey}`                        | `bff.notification.rules.get`             | notification         | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PUT`         | `/api/projects/{projectId}/notification-rules/{typeKey}`                        | `bff.notification.rules.update`          | notification         | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/notification-deliveries`                             | `bff.notification.deliveries.list`       | notification         | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/notification-deliveries/{deliveryId}/retry`          | `bff.notification.deliveries.retry`      | notification         | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/pages/{pageId}`                                       | `bff.page.get`                             | page aggregation     | `web-bff.yaml`     |
| Web BFF     | SSE           | `GET`         | `/api/projects/{projectId}/events`                                               | `bff.events.stream`                        | stream proxy         | `web-bff.yaml`     |
| Web BFF     | WebSocket     | `CONNECT`     | `/api/projects/{projectId}/socket`                                               | `bff.socket.connect`                       | stream proxy         | `web-bff.yaml`     |
| Web BFF     | File stream   | `GET`         | `/api/projects/{projectId}/files/{filePath}`                                     | `bff.file.download`                        | file proxy           | `web-bff.yaml`     |
| Web BFF     | File metadata | `HEAD`        | `/api/projects/{projectId}/files/{filePath}`                                     | `bff.file.head`                            | file proxy           | `web-bff.yaml`     |
| Web BFF     | File stream   | `PUT`         | `/api/projects/{projectId}/files/{filePath}`                                     | `bff.file.upload`                          | file proxy           | `web-bff.yaml`     |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models`                                                | `model.get`                                | model                | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models/source`                                         | `model.source.get`                         | model                | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/models/source/sync`                                    | `model.source.sync`                        | model                | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models/notion/oauth`                                   | `model.notion.oauth.get`                   | model                | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/models/notion/oauth/authorizations`                    | `model.notion.oauth.start`                 | model                | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/models/notion/oauth/connection`                        | `model.notion.oauth.disconnect`            | model                | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/model-notion/oauth/callback`                                                | `model.notion.oauth.callback`              | model                | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models/questions`                                      | `model.questions.list`                     | model                | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/models/questions`                                      | `model.questions.create`                   | model                | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models/questions/{questionId}`                         | `model.questions.get`                      | model                | `core.yaml`        |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}/models/questions/{questionId}`                         | `model.questions.update`                   | model                | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/models/questions/{questionId}`                         | `model.questions.delete`                   | model                | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/models/questions/{questionId}/sync`                    | `model.questions.sync`                     | model                | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models/questions/{questionId}/snapshots`               | `model.snapshots.list`                     | model                | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models/questions/{questionId}/snapshots/{snapshotId}`  | `model.snapshots.get`                      | model                | `core.yaml`        |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}/models/questions/{questionId}/snapshots/{snapshotId}`  | `model.snapshots.update`                   | model                | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/models/questions/{questionId}/diff`                    | `model.snapshots.diff`                     | model                | `core.yaml`        |
| Core        | Internal HTTP | `GET`         | `/v1/internal/model-notion-jobs/{jobId}/export`                                  | `model.worker.export`                      | model                | `core.yaml`        |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models`                                               | `bff.model.get`                            | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models/source`                                        | `bff.model.source.get`                     | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/models/source/sync`                                   | `bff.model.source.sync`                    | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models/notion/oauth`                                  | `bff.model.notion.oauth.get`               | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/models/notion/oauth/authorizations`                   | `bff.model.notion.oauth.start`             | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/models/notion/oauth/connection`                       | `bff.model.notion.oauth.disconnect`        | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/integrations/notion/oauth/callback`                                        | `bff.model.notion.oauth.callback`           | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models/questions`                                     | `bff.model.questions.list`                 | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/models/questions`                                     | `bff.model.questions.create`               | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models/questions/{questionId}`                        | `bff.model.questions.get`                  | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/models/questions/{questionId}`                        | `bff.model.questions.update`               | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/models/questions/{questionId}`                        | `bff.model.questions.delete`               | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/models/questions/{questionId}/sync`                   | `bff.model.questions.sync`                 | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models/questions/{questionId}/snapshots`              | `bff.model.snapshots.list`                 | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models/questions/{questionId}/snapshots/{snapshotId}` | `bff.model.snapshots.get`                  | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/models/questions/{questionId}/snapshots/{snapshotId}` | `bff.model.snapshots.update`               | model                | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/models/questions/{questionId}/diff`                   | `bff.model.snapshots.diff`                 | model                | `web-bff.yaml`     |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository`                                            | `repo.get`                                 | repo                 | `core.yaml`        |
| Core        | HTTP          | `PUT`         | `/v1/projects/{projectId}/repository`                                            | `repo.connect`                             | repo                 | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/repository`                                            | `repo.disconnect`                          | repo                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/repository/test`                                       | `repo.connection.test`                     | repo                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/repository/sync`                                       | `repo.sync.request`                        | repo                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/repository/webhook-secret`                             | `repo.webhook-secret.rotate`               | repo                 | `core.yaml`        |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}/repository/workspaces`                                 | `repo.workspaces.update`                   | repo                 | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository/branches`                                   | `repo.branches.list`                       | repo                 | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository/commits`                                    | `repo.commits.list`                        | repo                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/repository/commits`                                    | `repo.commits.create`                      | repo                 | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository/commits/{commitSha}`                        | `repo.commits.get`                         | repo                 | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository/tree`                                       | `repo.tree.list`                           | repo                 | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository/content`                                    | `repo.content.get`                         | repo                 | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/repository/checkouts`                                  | `repo.checkouts.create`                    | repo                 | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/repository/checkouts/{checkoutId}`                     | `repo.checkouts.get`                       | repo                 | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/repository/checkouts/{checkoutId}`                     | `repo.checkouts.release`                   | repo                 | `core.yaml`        |
| Core        | Webhook       | `POST`        | `/v1/repo/webhooks/github/{hookId}`                                              | `repo.webhooks.github`                     | repo                 | `core.yaml`        |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/repository`                                           | `bff.repo.get`                             | repo                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PUT`         | `/api/projects/{projectId}/repository`                                           | `bff.repo.connect`                         | repo                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/repository`                                           | `bff.repo.disconnect`                      | repo                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/repository/test`                                      | `bff.repo.connection.test`                 | repo                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/repository/sync`                                      | `bff.repo.sync.request`                    | repo                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/repository/webhook-secret`                            | `bff.repo.webhook-secret.rotate`           | repo                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/repository/workspaces`                                | `bff.repo.workspaces.update`               | repo                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/repository/branches`                                  | `bff.repo.branches.list`                   | repo                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/repository/commits`                                   | `bff.repo.commits.list`                    | repo                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/repository/commits/{commitSha}`                       | `bff.repo.commits.get`                     | repo                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/repository/tree`                                      | `bff.repo.tree.list`                       | repo                 | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/repository/content`                                   | `bff.repo.content.get`                     | repo                 | `web-bff.yaml`     |
| Web BFF     | Webhook       | `POST`        | `/api/webhooks/github/{hookId}`                                                  | `bff.repo.webhooks.github`                 | repo                 | `web-bff.yaml`     |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/uploads`                                     | `artifact.uploads.initialize`              | artifact             | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/artifacts/uploads/{uploadId}`                          | `artifact.uploads.get`                     | artifact             | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/artifacts/uploads/{uploadId}`                          | `artifact.uploads.abort`                   | artifact             | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/uploads/{uploadId}/parts/sign`               | `artifact.uploads.parts.sign`              | artifact             | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/uploads/{uploadId}/confirm`                  | `artifact.uploads.confirm`                 | artifact             | `core.yaml`        |
| Core        | File stream   | `GET`         | `/v1/artifact-transfers/{signedToken}`                                           | `artifact.transfers.get`                   | artifact             | `core.yaml`        |
| Core        | File stream   | `PUT`         | `/v1/artifact-transfers/{signedToken}`                                           | `artifact.transfers.put`                   | artifact             | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/artifacts`                                             | `artifact.list`                            | artifact             | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/artifacts/trash`                                       | `artifact.trash.list`                      | artifact             | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/artifacts/{artifactId}`                                | `artifact.get`                             | artifact             | `core.yaml`        |
| Core        | HTTP          | `PATCH`       | `/v1/projects/{projectId}/artifacts/{artifactId}`                                | `artifact.update`                          | artifact             | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/artifacts/{artifactId}`                                | `artifact.trash`                           | artifact             | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/artifacts/{artifactId}/versions`                       | `artifact.versions.list`                   | artifact             | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/{artifactId}/versions/uploads`               | `artifact.versions.uploads.initialize`     | artifact             | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/{artifactId}/versions/{versionId}/restore`   | `artifact.versions.restore`                | artifact             | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/{artifactId}/download`                       | `artifact.download`                        | artifact             | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/{artifactId}/versions/{versionId}/download`  | `artifact.versions.download`               | artifact             | `core.yaml`        |
| Core        | HTTP          | `GET`         | `/v1/projects/{projectId}/artifacts/{artifactId}/versions/{versionId}/previews`  | `artifact.previews.list`                   | artifact             | `core.yaml`        |
| Core        | HTTP          | `POST`        | `/v1/projects/{projectId}/artifacts/{artifactId}/restore`                        | `artifact.restore`                         | artifact             | `core.yaml`        |
| Core        | HTTP          | `DELETE`      | `/v1/projects/{projectId}/artifacts/{artifactId}/purge`                          | `artifact.purge`                           | artifact             | `core.yaml`        |
| Core        | Internal HTTP | `POST`        | `/v1/internal/artifact-preview-jobs/{jobId}/transfers`                           | `artifact.preview-jobs.transfers.create`   | artifact             | `core.yaml`        |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/uploads`                                    | `bff.artifact.uploads.initialize`          | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/artifacts/uploads/{uploadId}`                         | `bff.artifact.uploads.get`                 | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/artifacts/uploads/{uploadId}`                         | `bff.artifact.uploads.abort`               | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/uploads/{uploadId}/parts/sign`              | `bff.artifact.uploads.parts.sign`          | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/uploads/{uploadId}/confirm`                 | `bff.artifact.uploads.confirm`             | artifact             | `web-bff.yaml`     |
| Web BFF     | File stream   | `GET`         | `/api/artifact-transfers/{signedToken}`                                          | `bff.artifact.transfers.get`               | artifact             | `web-bff.yaml`     |
| Web BFF     | File stream   | `PUT`         | `/api/artifact-transfers/{signedToken}`                                          | `bff.artifact.transfers.put`               | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/artifacts`                                            | `bff.artifact.list`                        | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/artifacts/trash`                                      | `bff.artifact.trash.list`                  | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/artifacts/{artifactId}`                               | `bff.artifact.get`                         | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `PATCH`       | `/api/projects/{projectId}/artifacts/{artifactId}`                               | `bff.artifact.update`                      | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/artifacts/{artifactId}`                               | `bff.artifact.trash`                       | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/artifacts/{artifactId}/versions`                      | `bff.artifact.versions.list`               | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/{artifactId}/versions/uploads`              | `bff.artifact.versions.uploads.initialize` | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/{artifactId}/versions/{versionId}/restore`  | `bff.artifact.versions.restore`            | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/{artifactId}/download`                      | `bff.artifact.download`                    | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/{artifactId}/versions/{versionId}/download` | `bff.artifact.versions.download`           | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `GET`         | `/api/projects/{projectId}/artifacts/{artifactId}/versions/{versionId}/previews` | `bff.artifact.previews.list`               | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `POST`        | `/api/projects/{projectId}/artifacts/{artifactId}/restore`                       | `bff.artifact.restore`                     | artifact             | `web-bff.yaml`     |
| Web BFF     | HTTP          | `DELETE`      | `/api/projects/{projectId}/artifacts/{artifactId}/purge`                         | `bff.artifact.purge`                       | artifact             | `web-bff.yaml`     |
| MCP Gateway | MCP Tool      | `tools/call`  | `/mcp`                                                                           | `system.echo`                              | engineering baseline | `system.echo.json` |

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
