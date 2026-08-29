// @vitest-environment node
import { describe, expect, it } from "vitest";

import { forwardSyncPoint, parseSyncTex, reverseSyncPoint } from "./synctex";

describe("SyncTeX navigation", () => {
  it("maps generated records to immutable source files in both directions", () => {
    const points = parseSyncTex(
      [
        "SyncTeX Version:1",
        "Input:1:/build/template/main.tex",
        "Input:2:C:\\build\\template\\sections\\results.tex",
        "Content:",
        "{1",
        "[1,10:100,1000:200,20,0",
        "x1,20:100,5000",
        "[2,8:100,9000:200,20,0",
        "}",
        "{2",
        "[2,30:100,2000:200,20,0",
        "}",
      ].join("\n"),
      ["main.tex", "sections/results.tex"],
    );

    expect(forwardSyncPoint(points, "main.tex", 18)).toMatchObject({
      line: 20,
      page: 1,
    });
    expect(reverseSyncPoint(points, 1, 100)).toMatchObject({
      file: "sections/results.tex",
      line: 8,
    });
    expect(reverseSyncPoint(points, 2, 50)).toMatchObject({
      file: "sections/results.tex",
      line: 30,
    });
  });
});
