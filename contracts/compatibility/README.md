# Contract compatibility

`baseline.json` is the stage 3.10 compatibility floor. CI compares current
OpenAPI and JSON Schema signatures against it.

Rejected changes include:

- removing or moving an existing operation;
- removing a documented response status;
- making an existing request or parameter newly required;
- removing schemas, definitions, properties, or enum values;
- changing a stable property type, format, or reference;
- adding required fields to an existing schema.

Additive operations, optional properties, new schemas, and new enum-bearing
types are allowed. If a breaking change is intentional, publish a new API or
schema version and review the baseline update separately.

Run `pnpm contracts:baseline` only for an explicitly approved compatibility
floor change.
