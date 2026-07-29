import { describe, expect, it } from "vitest";

import {
  parseRepoLocation,
  repoLocationQuery,
} from "@/features/repo-browser/location";

describe("Repo browser URL state", () => {
  it("accepts only logical workspaces, full SHAs, and safe repository paths", () => {
    const sha = "a".repeat(40);
    expect(
      parseRepoLocation(
        new URLSearchParams({
          path: "src/模型 #1.ts",
          revision: sha,
          workspace: "article",
        }),
      ),
    ).toEqual({
      path: "src/模型 #1.ts",
      revision: sha,
      workspace: "article",
    });
    expect(
      parseRepoLocation(
        new URLSearchParams({
          path: "../secret",
          revision: "main",
          workspace: "other",
        }),
      ),
    ).toEqual({ path: "", revision: null, workspace: "code" });
  });

  it("serializes stable workspace, revision, and path query state", () => {
    const sha = "b".repeat(64);
    expect(
      repoLocationQuery({
        path: "a b/结果.md",
        revision: sha,
        workspace: "result",
      }),
    ).toBe(`workspace=result&revision=${sha}&path=a+b%2F%E7%BB%93%E6%9E%9C.md`);
  });
});
