# JSON Schema contracts

Cross-process schemas live here and are treated as compatibility-controlled
contracts. All schemas use JSON Schema 2020-12, declare a stable HTTPS `$id`,
and are compiled by `pnpm contracts:check`.

`common/dtos.schema.json` owns shared identifiers, timestamps, errors,
pagination, and actors. Feature modules add schemas under their own directory
instead of copying these definitions.
