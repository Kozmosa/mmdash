# JSON Schema contracts

Cross-process schemas live here and are treated as compatibility-controlled
contracts. All schemas use JSON Schema 2020-12, declare a stable HTTPS `$id`,
and are compiled by `pnpm contracts:check`.

`common/dtos.schema.json` owns shared identifiers, timestamps, errors,
pagination, and actors. Feature modules add schemas under their own directory
instead of copying these definitions.

Stage 8 freezes `run_spec.schema.json`, `manifest.schema.json`, and the Box
capability/resource definitions in `box.schema.json`. `manifest.json` is the
only result manifest name; `result_manifest.json` is not a supported alias.

Stage 9 freezes `article-template.schema.json`. A Template Registry entry
always references one immutable Artifact Version; an arbitrary Overleaf ZIP is
not a formal template until conversion, schema validation, security checks,
and a test build succeed.
