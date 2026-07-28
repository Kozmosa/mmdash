import { spawnSync } from "node:child_process";
import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { format } from "prettier";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const output = path.join(root, "packages/core-client/src/generated/core.ts");
const cli = path.join(
  root,
  "packages/core-client/node_modules/openapi-typescript/bin/cli.js",
);
const generated = spawnSync(
  process.execPath,
  [cli, "contracts/openapi/core.yaml"],
  { cwd: root, encoding: "utf8" },
);
if (generated.status !== 0) {
  console.error(generated.stderr || generated.stdout);
  process.exit(1);
}
const expected = await format(generated.stdout, {
  parser: "typescript",
});

if (process.argv.includes("--check")) {
  const actual = await readFile(output, "utf8").catch(() => "");
  if (normalize(actual) !== normalize(expected)) {
    console.error(
      "packages/core-client/src/generated/core.ts is stale; run pnpm contracts:generate.",
    );
    process.exit(1);
  }
} else {
  await writeFile(output, expected, "utf8");
  console.log("generated packages/core-client/src/generated/core.ts");
}

function normalize(value) {
  return value.replace(/\r\n/g, "\n");
}
