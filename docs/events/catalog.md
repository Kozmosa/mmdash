# Event catalog

Search by event type, producer, consumer, payload field, or schema version.
All entries currently use envelope schema version `1`.

| Event type               | Producer | Project-scoped | Payload keys                               | Current consumer               |
| ------------------------ | -------- | -------------- | ------------------------------------------ | ------------------------------ |
| `project.created`        | project  | yes            | `project_id`, `name`                       | none                           |
| `project.updated`        | project  | yes            | `project_id`                               | none                           |
| `project.member.updated` | project  | yes            | `project_id`, `user_id`, `role`            | none                           |
| `project.member.removed` | project  | yes            | `project_id`, `user_id`                    | none                           |
| `settings.updated`       | settings | conditional    | `scope`, `scope_id`, `type_key`, `version` | none                           |
| `settings.deleted`       | settings | conditional    | `scope`, `scope_id`, `type_key`            | none                           |
| `job.created`            | jobs     | yes            | `job_id`, `job_type`                       | none                           |
| `job.cancel.requested`   | jobs     | yes            | `job_id`, `status`                         | none                           |
| `job.queued`             | jobs     | yes            | `job_id`, `job_type`, `status`, `attempts` | none                           |
| `job.succeeded`          | jobs     | yes            | `job_id`, `job_type`, `status`, `attempts` | none                           |
| `job.failed`             | jobs     | yes            | `job_id`, `job_type`, `status`, `attempts` | none                           |
| `job.cancelled`          | jobs     | yes            | `job_id`, `job_type`, `status`, `attempts` | none                           |
| `job.timed.out`          | jobs     | yes            | `job_id`, `job_type`, `status`, `attempts` | none                           |
| `system.test.emitted`    | system   | no             | `message`, caller-provided fields          | `platform.system-test-receipt` |

`conditional` means project settings carry `project_id`, while system settings
use a null project scope.

The engineering consumer `platform.system-test-receipt` intentionally performs
no domain mutation. Its successful durable delivery and consumption records
prove the Event Bus and Outbox path during stage-3.15 smoke checks.

When adding an event:

1. choose a stable lowercase dotted event type;
2. write state and the event in one transaction;
3. document payload fields and sensitivity;
4. register each consumer with a stable name and exact/prefix pattern;
5. make consumer side effects idempotent by event and delivery key;
6. add failure, retry, and replay tests.
