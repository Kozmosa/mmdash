# Box Gateway

The independently deployed Go Box gateway speaks only the Core Box Control
contract. It owns no business persistence: registration, leases, status,
logs, artifacts, Audit, Outbox, and Data Hub projections remain Core-owned.
Per-task execution state is recoverable from a user-only Box state file and
all generated output is removed after completion.
