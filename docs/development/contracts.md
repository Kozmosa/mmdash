# Contract development

Change a cross-process boundary in this order:

1. edit its OpenAPI or JSON Schema source under `contracts/`;
2. add/update examples and Mock Server fixtures;
3. run `pnpm contracts:generate`;
4. use generated Go request types and `httpx.DecodeJSON` in Core handlers;
5. update `docs/api/endpoints.md` for new operations;
6. run `pnpm contracts:check` and the owning module tests.

Do not edit generated files. Do not update the compatibility baseline to make
an unexplained breaking change pass.

The Mock Server is contract-driven development infrastructure; it is not a
substitute for the PostgreSQL/Core integration checks performed at the end of
stage 3.15.
