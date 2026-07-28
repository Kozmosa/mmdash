# **MODULE** module starter

This generated directory is a reviewable starting point, not an automatic
cross-layer install.

1. Move `backend/module.go.tmpl` to `backend/internal/__MODULE__/module.go`.
2. Move `bff/routes.ts.tmpl` to
   `apps/web-bff/src/modules/__MODULE__/routes.ts`.
3. Move `web/README.md.tmpl` to `apps/web/src/features/__MODULE__/README.md`.
4. Merge `contracts/openapi.yaml.tmpl` into
   `contracts/openapi/core.yaml`.
5. Register every layer explicitly, update `docs/api/endpoints.md`, and add
   unit plus boundary tests.

This review step keeps module ownership and public contracts intentional.
