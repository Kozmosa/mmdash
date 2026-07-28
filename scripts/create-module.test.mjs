import { mkdtemp, mkdir, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

import { describe, expect, it } from "vitest";

import { createModule } from "./create-module.mjs";

describe("module generator", () => {
  it("creates all process-boundary starters with replaced tokens", async () => {
    const root = await mkdtemp(path.join(tmpdir(), "mmdash-module-"));
    await mkdir(path.join(root, "templates", "module", "backend"), {
      recursive: true,
    });
    await writeFile(
      path.join(root, "templates", "module", "backend", "module.go.tmpl"),
      "package __MODULE__\n",
      "utf8",
    );

    const destination = await createModule("sample", root);

    expect(
      await readFile(
        path.join(destination, "backend", "module.go.tmpl"),
        "utf8",
      ),
    ).toBe("package sample\n");
  });

  it("rejects unsafe names", async () => {
    await expect(createModule("../escape")).rejects.toThrow(
      "Module name must start",
    );
  });
});
