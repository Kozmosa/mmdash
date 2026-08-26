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
| `article.draft.flushed`       | article  | yes            | `resource_id`, `resource_type=article_draft`, `draft_revision`, `actor_kind`, `status`                          | `datahub.projections`          |
| `article.block.reviewed`      | article  | yes            | `resource_id`, `resource_type=article_block`, `block_id`, `status=reviewed`                                    | `datahub.projections`          |
| `article.chapter.created`     | article  | yes            | `chapter_tag_id`, `heading_block_id`, `status`                                                                  | none                           |
| `article.chapter.updated`     | article  | yes            | `chapter_tag_id`, `status`                                                                                      | none                           |
| `article.chapter.deleted`     | article  | yes            | `chapter_tag_id`                                                                                                | none                           |
| `article.chapter.reviewed`    | article  | yes            | `chapter_tag_id`, `status=reviewed`, `reviewed_by`                                                              | none                           |
| `article.patch.proposed`      | article  | yes            | `resource_id`, `resource_type=article_block`, `patch_id`, `base_revision`, `status`                             | `datahub.projections`          |
| `article.patch.reviewed`      | article  | yes            | `resource_id`, `resource_type=article_block`, `patch_id`, `decision`, optional accepted revision               | `datahub.projections`          |
| `article.commit.created`      | article  | yes            | `resource_id`, `resource_type=article_commit`, `commit_id`, `git_commit_sha`, `draft_revision`                  | `datahub.projections`          |
| `article.build.queued`        | article  | yes            | `resource_id`, `resource_type=article_build`, `build_id`, `build_kind`, optional Commit/draft revision          | `datahub.projections`          |
| `article.build.completed`     | article  | yes            | `resource_id`, `resource_type=article_build`, `build_id`, `status=succeeded`, fixed output Version roles        | `datahub.projections`, progress trigger, `notification.events` |
| `article.build.failed`        | article  | yes            | `resource_id`, `resource_type=article_build`, `build_id`, `status=failed`, bounded error code                   | `datahub.projections`, `notification.events` |
| `article.release.created`     | article  | yes            | `resource_id`, `resource_type=article_release`, `release_id`, Commit/Build IDs, tag, fixed output Versions      | `datahub.projections`, `notification.events` |
| `progress.milestone.created`  | progress | yes            | `resource_id`, `resource_type=milestone`, `title`, `status`, `source`, optional `source_run_id`, `proposal_id`  | `datahub.projections`          |
| `progress.milestone.updated`  | progress | yes            | `resource_id`, `resource_type=milestone`, `title`, `status`, `source`, optional `source_run_id`, `proposal_id`  | `datahub.projections`          |
| `progress.task.created`       | progress | yes            | `resource_id`, `resource_type=task`, `title`, `status`, `source`, optional `source_run_id`, `source_evaluation_id`, `proposal_id` | `datahub.projections` |
| `progress.task.updated`       | progress | yes            | `resource_id`, `resource_type=task`, `title`, `status`, `source`, optional `source_run_id`, `source_evaluation_id`, `proposal_id` | `datahub.projections` |
| `progress.task.deleted`       | progress | yes            | `resource_id`, `resource_type=task`, `title`, `status=deleted`, `source`                                        | `datahub.projections`          |
| `progress.dependency.created` | progress | yes            | `resource_id`, `resource_type=dependency`, `title`, `status`, `task_id`, `depends_on_task_id`                   | none                           |
| `progress.dependency.deleted` | progress | yes            | `resource_id`, `resource_type=dependency`, `title`, `status=deleted`, `source`                                  | none                           |
| `progress.reminder.created`   | progress | yes            | `resource_id`, `resource_type=reminder`, `title`, `status`, `remind_at`                                         | none                           |
| `progress.reminder.due`       | progress | yes            | `resource_id`, `resource_type=reminder`, `title`, `status`, `reminder_id`, optional task/milestone IDs          | `notification.events`          |
| `progress.proposal.created`   | progress | yes            | `resource_id`, `resource_type=progress_proposal`, `title`, `status`, `proposal_type`, `source`, `source_run_id`, `source_evaluation_id` | `datahub.projections` |
| `progress.proposal.reviewed`  | progress | yes            | `resource_id`, `resource_type=progress_proposal`, `title`, `status`, `proposal_type`, `decision`                | `datahub.projections`          |
| `progress.evaluation.requested` | progress | yes          | `resource_id`, `resource_type=progress_evaluation`, `trigger_kind`, `scheduled_for`                             | none                           |
| `progress.evaluation.queued`  | progress | yes            | `resource_id`, `resource_type=progress_evaluation`, `job_id`, `input_version`, `trigger_kind`                   | none                           |
| `progress.evaluation.started` | progress | yes            | `resource_id`, `resource_type=progress_evaluation`, `job_id`, `attempt`                                         | none                           |
| `progress.evaluation.completed` | progress | yes          | `resource_id`, `resource_type=progress_evaluation`, `status=succeeded`, detected/effective stage, summary and counts | `datahub.projections`       |
| `progress.evaluation.failed`  | progress | yes            | `resource_id`, `resource_type=progress_evaluation`, `status=failed`, safe `error_code`, `attempts`              | `datahub.projections`          |
| `progress.risk.detected`      | progress | yes            | `resource_id`, `resource_type=progress_risk`, `evaluation_id`, `risk_key`, title, severity, `status=open`       | `datahub.projections`          |
| `progress.settings.updated`   | progress | yes            | `resource_id`, `resource_type=progress_settings`, automatic/Cron/debounce/minimum-interval settings             | none                           |
| `progress.stage.overridden`   | progress | yes            | `resource_id`, `resource_type=progress_stage_override`, stage and summary                                       | none                           |
| `progress.stage.override_cleared` | progress | yes        | `resource_id`, `resource_type=progress_stage_override`, prior stage                                             | none                           |
| `model.sync.requested`        | model    | yes            | `sync_id`, `source_id`, optional `question_id`, `scope`, `trigger`, `job_id`, `requested_at`                    | none                           |
| `model.source.changed`        | model    | yes            | `source_id`, `notion_root_page_id`, `action`, `status`                                                           | `datahub.projections`          |
| `model.question.changed`      | model    | yes            | `question_id`, `source_id`, `code`, `title`, `notion_page_id`, `action`, `status`                               | `datahub.projections`          |
| `model.snapshot.created`      | model    | yes            | `snapshot_id`, `question_id`, `source_id`, `content_hash`, optional `previous_snapshot_id`, `captured_at`       | `datahub.projections`          |
| `agent.instance.created`        | agent    | yes            | `project_id`, `resource_id`, `adapter_type=hermes`, `management_mode`, `status`                                 | none                           |
| `agent.instance.updated`        | agent    | yes            | `project_id`, `resource_id`, `management_mode`, `status`                                                        | none                           |
| `agent.instance.revoked`        | agent    | yes            | `project_id`, `resource_id`, `status=disabled`                                                                  | none                           |
| `agent.prompt.updated`          | agent    | yes            | `project_id`, `resource_id`, `version_changed=true`                                                             | none                           |
| `agent.prompt.reset`            | agent    | yes            | `project_id`, `resource_id`, `version_changed=true`                                                             | none                           |
| `agent.session.created`         | agent    | yes            | `project_id`, `resource_id`, `agent_instance_id`, `session_type`                                                | none                           |
| `agent.session.renamed`         | agent    | yes            | `project_id`, `resource_id`, `status`, `session_type`                                                           | none                           |
| `agent.session.ended`           | agent    | yes            | `project_id`, `resource_id`, `status=ended`, `session_type`                                                     | none                           |
| `agent.session.continued`       | agent    | yes            | `project_id`, `resource_id`, `status=active`, `session_type`                                                    | none                           |
| `agent.session.forked`          | agent    | yes            | `project_id`, `resource_id`, `agent_instance_id`, `parent_session_id`, `session_type`                           | none                           |
| `agent.session.default_changed` | agent    | yes            | `project_id`, `resource_id`, `agent_instance_id`                                                                | none                           |
| `agent.run.started`             | agent    | yes            | `project_id`, `resource_id`, `session_id`, `source`, optional `source_run_id`, `source_evaluation_id`; Stage 6 uses `source=progress_evaluation` | none                     |
| `agent.run.completed`           | agent    | yes            | `project_id`, `resource_id`, `session_id`, `source`, optional `source_run_id`, `source_evaluation_id`, `status=completed`, empty `safe_error_code` | none                  |
| `agent.run.failed`              | agent    | yes            | `project_id`, `resource_id`, `session_id`, `source`, optional `source_run_id`, `source_evaluation_id`, `status=failed`, bounded `safe_error_code` | none                   |
| `agent.run.stopped`             | agent    | yes            | `project_id`, `resource_id`, `session_id`, `source`, optional `source_run_id`, `source_evaluation_id`, `status=stopped`, bounded `safe_error_code` | none                  |
| `agent.token.issued`            | agent    | yes            | `project_id`, Token `resource_id`, `rotation_id`, non-terminal `status`                                         | none                           |
| `agent.token.activated`         | agent    | yes            | `project_id`, Token `resource_id`, `rotation_id`, optional replaced Token ID, `status=active`                   | none                           |
| `agent.token.rotation_failed`   | agent    | yes            | `project_id`, Token `resource_id`, `rotation_id`, safe code, whether an old Token remains active                | none                           |
| `agent.token.revoked`           | agent    | yes            | `project_id`, Token `resource_id`, `status=revoked`                                                             | none                           |
| `experiment.created`            | experiment | yes          | `experiment_id`, type, immutable source/entrypoint, initial execution status, frozen result directory           | `datahub.projections`, progress trigger |
| `experiment.started`            | experiment | yes          | `experiment_id`, task/Box/epoch, actual Runtime/version, execution status                                      | `datahub.projections`          |
| `experiment.phase_changed`      | experiment | yes          | `experiment_id`, previous/current execution status, optional connectivity state, progress                      | `datahub.projections`          |
| `experiment.succeeded`          | experiment | yes          | `experiment_id`, type, verified result Commit/directory/Manifest hash, optional immutable Bundle IDs           | `datahub.projections`          |
| `experiment.failed`             | experiment | yes          | `experiment_id`, optional task/Box, stable failure stage/code/time, retryability, log truncation                | `datahub.projections`          |
| `experiment.rerun_created`      | experiment | yes          | new/previous/root Experiment IDs and immutable retry sequence                                                    | `datahub.projections`          |
| `experiment.result_bound`       | experiment | yes          | `experiment_id`, verified result Commit/directory/Manifest hash                                                  | `datahub.projections`          |
| `experiment.canceled`           | experiment | yes          | `experiment_id`, type, optional task, `execution_status=canceled`                                               | `datahub.projections`          |
| `experiment.archived`           | experiment | yes          | `experiment_id`, type, prior terminal state, `execution_status=archived`                                        | `datahub.projections`          |
| `box.registered`                | boxcontrol | no          | `box_id`, owner, name, version, `status=registering`                                                            | none                           |
| `box.assigned`                  | boxcontrol | yes         | `box_id`, `project_id`, assigning user and time                                                                 | `datahub.projections`          |
| `box.unassigned`                | boxcontrol | yes         | `box_id`, `project_id`, normal/force mode and failed Experiment count                                           | `datahub.projections`          |
| `box.heartbeat.received`        | boxcontrol | no          | `box_id`, owner, version, running tasks, advertised Runtimes and status                                         | none                           |
| `box.offline`                   | boxcontrol | no          | `box_id`, offline start and bounded active Experiment IDs                                                       | `datahub.projections`          |
| `box.recovered`                 | boxcontrol | no          | `box_id`, recovery time and offline duration                                                                    | `datahub.projections`          |
| `box.revoked`                   | boxcontrol | no          | `box_id`, owner, name, drain/force mode, status and failed Experiment count                                     | `datahub.projections`          |

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

Agent lifecycle payloads never contain Agent Token plaintext or hashes,
Hermes API Keys, Dashboard or Cloudflare credentials, runtime or management
URLs, message bodies, Tool arguments/results, reasoning, or raw provider
errors. Token rotation failure is an explicit state event and records that the
old Token remains active. Hermes Run SSE is a request-scoped browser stream,
not an Outbox domain event and not a Stage 6 progress trigger.

Progress events carry only bounded resource metadata and source references.
Critical Milestone writes are human-session operations; accepted non-human
changes are applied by Progress and carry their Proposal/source run. Reminder
Notification consumes stable invitation lifecycle, registration, and reminder
events into its canonical Notification/Recipient/Inbox model. External delivery
remains behind the Core Delivery Processor and registered Feishu/Generic Webhook
adapters; Progress never sends provider requests.

Automatic tracking ignores `progress.*` mutations carrying
`source_evaluation_id` and Agent Run events with
`source=progress_evaluation`, preventing its own Task/Proposal/Run output from
recursively scheduling another evaluation. Evaluation and risk projections
remain readable in Data Hub but are excluded from the next semantic input
hash; the previous normalized output is supplied explicitly instead.

Model events never contain the Notion integration token, temporary Notion file
URLs, raw block content, or Artifact transfer credentials. A source or question
change updates the mutable `model_source` or `model_question` projection;
`model.snapshot.created` creates an immutable `model_snapshot` projection
addressable through MCP `data.list/read`. An unchanged content Hash completes
the synchronization as `unchanged` and deliberately emits no Snapshot event.

`progress.reminder.due` uses the Reminder UUID as its stable event ID. Progress
commits the Reminder `triggered` state and Outbox row together; automatic and
manual triggering share the same leased PostgreSQL claim path.

When adding an event:

1. choose a stable lowercase dotted event type;
2. write state and the event in one transaction;
3. document payload fields and sensitivity;
4. register each consumer with a stable name and exact/prefix pattern;
5. make consumer side effects idempotent by event and delivery key;
6. add failure, retry, and replay tests.
