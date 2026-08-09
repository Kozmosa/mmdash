# Event catalog

Search by event type, producer, consumer, payload field, or schema version.
All entries currently use envelope schema version `1`.

| Event type                    | Producer | Project-scoped | Payload keys                                                                                                    | Current consumer               |
| ----------------------------- | -------- | -------------- | --------------------------------------------------------------------------------------------------------------- | ------------------------------ |
| `project.created`             | project  | yes            | `project_id`, `name`                                                                                            | `datahub.projections`          |
| `project.updated`             | project  | yes            | `project_id`                                                                                                    | `datahub.projections`          |
| `project.member.updated`      | project  | yes            | `project_id`, `user_id`, `role`                                                                                 | none                           |
| `project.member.removed`      | project  | yes            | `project_id`, `user_id`                                                                                         | none                           |
| `project.member.invited`      | project  | yes            | `project_id`, `invitation_id`, `normalized_email`, `role`, `invited_by_user_id`, `expires_at`                  | `notification.events`          |
| `project.member.joined`       | project  | yes            | `project_id`, `invitation_id` when invitation-backed, `user_id`, `role`                                         | `notification.events`          |
| `project.invitation.revoked`  | project  | yes            | `project_id`, `invitation_id`, optional reason                                                                  | `notification.events`          |
| `project.invitation.expired`  | project  | yes            | `project_id`, `invitation_id`                                                                                   | `notification.events`          |
| `user.registered`             | auth     | yes            | `user_id`, normalized `email`, `display_name`                                                                   | `notification.events`          |
| `settings.updated`            | settings | conditional    | `scope`, `scope_id`, `type_key`, `version`                                                                      | none                           |
| `settings.deleted`            | settings | conditional    | `scope`, `scope_id`, `type_key`                                                                                 | none                           |
| `job.created`                 | jobs     | yes            | `job_id`, `job_type`                                                                                            | none                           |
| `job.cancel.requested`        | jobs     | yes            | `job_id`, `status`                                                                                              | none                           |
| `job.queued`                  | jobs     | yes            | `job_id`, `job_type`, `status`, `attempts`                                                                      | none                           |
| `job.succeeded`               | jobs     | yes            | `job_id`, `job_type`, `status`, `attempts`                                                                      | none                           |
| `job.failed`                  | jobs     | yes            | `job_id`, `job_type`, `status`, `attempts`                                                                      | none                           |
| `job.cancelled`               | jobs     | yes            | `job_id`, `job_type`, `status`, `attempts`                                                                      | none                           |
| `job.timed.out`               | jobs     | yes            | `job_id`, `job_type`, `status`, `attempts`                                                                      | none                           |
| `system.test.emitted`         | system   | no             | `message`, caller-provided fields                                                                               | `platform.system-test-receipt` |
| `context.proposal.created`    | datahub  | yes            | `proposal_id`, `context_type`                                                                                   | none                           |
| `context.confirmed`           | datahub  | yes            | `proposal_id`, `context_id`                                                                                     | none                           |
| `context.proposal.rejected`   | datahub  | yes            | `proposal_id`, empty `context_id`                                                                               | none                           |
| `repo.connected`              | repo     | yes            | `repository_id`, `provider`, `workspaces`                                                                       | `datahub.projections`          |
| `repo.commit.created`         | repo     | yes            | `repository_id`, `workspace`, `branch`, `commit_sha`, `previous_commit_sha`, `history_rewritten`, `source`      | `datahub.projections`          |
| `repo.commit.detected`        | repo     | yes            | `repository_id`, `workspace`, `branch`, `commit_sha`, `previous_commit_sha`, `history_rewritten`, `source`      | `datahub.projections`          |
| `artifact.created`            | artifact | yes            | `artifact_id`, `version_id`, `kind`, `source`, `name`, `filename`, `sha256`, `size_bytes`, `status`             | `datahub.projections`          |
| `artifact.available`          | artifact | yes            | `artifact_id`, `version_id`, `version_no`, `sha256`, `size_bytes`, `mime_type`, `reason`, `available_at`        | `datahub.projections`          |
| `artifact.deleted`            | artifact | yes            | `artifact_id`, `current_version_id`, `reason`, `trashed_at`                                                     | `datahub.projections`          |
| `progress.milestone.created`  | progress | yes            | `resource_id`, `resource_type=milestone`, `title`, `status`, `source`, optional `source_run_id`, `proposal_id`  | `datahub.projections`          |
| `progress.milestone.updated`  | progress | yes            | `resource_id`, `resource_type=milestone`, `title`, `status`, `source`, optional `source_run_id`, `proposal_id`  | `datahub.projections`          |
| `progress.task.created`       | progress | yes            | `resource_id`, `resource_type=task`, `title`, `status`, `source`, optional `source_run_id`, `proposal_id`       | `datahub.projections`          |
| `progress.task.updated`       | progress | yes            | `resource_id`, `resource_type=task`, `title`, `status`, `source`, optional `source_run_id`, `proposal_id`       | `datahub.projections`          |
| `progress.task.deleted`       | progress | yes            | `resource_id`, `resource_type=task`, `title`, `status=deleted`, `source`                                        | `datahub.projections`          |
| `progress.dependency.created` | progress | yes            | `resource_id`, `resource_type=dependency`, `title`, `status`, `task_id`, `depends_on_task_id`                   | none                           |
| `progress.dependency.deleted` | progress | yes            | `resource_id`, `resource_type=dependency`, `title`, `status=deleted`, `source`                                  | none                           |
| `progress.reminder.created`   | progress | yes            | `resource_id`, `resource_type=reminder`, `title`, `status`, `remind_at`                                         | none                           |
| `progress.reminder.due`       | progress | yes            | `resource_id`, `resource_type=reminder`, `title`, `status`, `reminder_id`, optional task/milestone IDs          | `notification.events`          |
| `progress.proposal.created`   | progress | yes            | `resource_id`, `resource_type=progress_proposal`, `title`, `status`, `proposal_type`, `source`, `source_run_id` | `datahub.projections`          |
| `progress.proposal.reviewed`  | progress | yes            | `resource_id`, `resource_type=progress_proposal`, `title`, `status`, `proposal_type`, `decision`                | `datahub.projections`          |
| `model.sync.requested`        | model    | yes            | `sync_id`, `source_id`, optional `question_id`, `scope`, `trigger`, `job_id`, `requested_at`                    | none                           |
| `model.source.changed`        | model    | yes            | `source_id`, `notion_root_page_id`, `action`, `status`                                                           | `datahub.projections`          |
| `model.question.changed`      | model    | yes            | `question_id`, `source_id`, `code`, `title`, `notion_page_id`, `action`, `status`                               | `datahub.projections`          |
| `model.snapshot.created`      | model    | yes            | `snapshot_id`, `question_id`, `source_id`, `content_hash`, optional `previous_snapshot_id`, `captured_at`       | `datahub.projections`          |

`conditional` means project settings carry `project_id`, while system settings
use a null project scope.

The engineering consumer `platform.system-test-receipt` intentionally performs
no domain mutation. Its successful durable delivery and consumption records
prove the Event Bus and Outbox path during stage-3.15 smoke checks.

`datahub.projections` turns registered domain events into searchable Data Hub
objects and activity. A projector must be idempotent by `event_id`; replaying
`project.created` therefore does not duplicate its object or activity.

Repo payloads never contain credentials, file content, provider error bodies,
commands, or server paths. `repo.connected` is emitted only after all three
workspaces are ready. Commit events identify immutable full SHAs; consumers
must not persist a branch name as the durable version reference.

Artifact payloads never contain signed transfer URLs, provider upload IDs,
object keys, credentials, file content, or preview output. `artifact.available`
is emitted only after full size and SHA-256 verification, except
`reason=restored`, which re-exposes an already verified immutable Version.
`artifact.deleted` is a recoverable trash transition and does not imply that
Version bytes were deleted.

Progress events carry only bounded resource metadata and source references.
Critical Milestone writes are human-session operations; accepted non-human
changes are applied by Progress and carry their Proposal/source run. Reminder
Notification consumes stable invitation lifecycle, registration, and reminder
events into its canonical Notification/Recipient/Inbox model. External delivery
remains behind the Core Delivery Processor and registered Feishu/Generic Webhook
adapters; Progress never sends provider requests.

Model events never contain the Notion integration token, temporary Notion file
URLs, raw block content, or Artifact transfer credentials. A source or question
change updates the mutable `model_source` or `model_question` projection;
`model.snapshot.created` creates an immutable `model_snapshot` projection
addressable through MCP `data.list/read`. An unchanged content Hash completes
the synchronization as `unchanged` and deliberately emits no Snapshot event.

When adding an event:

1. choose a stable lowercase dotted event type;
2. write state and the event in one transaction;
3. document payload fields and sensitivity;
4. register each consumer with a stable name and exact/prefix pattern;
5. make consumer side effects idempotent by event and delivery key;
6. add failure, retry, and replay tests.
