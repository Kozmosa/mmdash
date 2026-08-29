import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

import { describe, expect, it } from "vitest";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const generatedFile = path.join(root, "packages/api-types/src/generated.ts");

function checkGeneratedContracts() {
  return spawnSync(
    process.execPath,
    ["scripts/generate-contracts.mjs", "--check"],
    {
      cwd: root,
      encoding: "utf8",
    },
  );
}

describe("generated contract freshness", () => {
  it("ignores platform line endings but rejects changed generated content", async () => {
    const original = await readFile(generatedFile, "utf8");
    const normalized = original.replace(/\r\n/g, "\n");
    try {
      await writeFile(generatedFile, normalized.replace(/\n/g, "\r\n"), "utf8");
      expect(checkGeneratedContracts().status).toBe(0);

      await writeFile(generatedFile, `${normalized}\n// stale`, "utf8");
      expect(checkGeneratedContracts().status).toBe(1);
    } finally {
      await writeFile(generatedFile, original, "utf8");
    }
  });
});
