import { describe, expect, it } from "vitest";

import { workspaceHref, workspaceRoutes } from "@/lib/navigation";

describe("workspace route registry", () => {
  it("registers exactly the seven design navigation items", () => {
    expect(workspaceRoutes.map((route) => route.label)).toEqual([
      "首页",
      "mmdash Agent",
      "进度跟踪",
      "模型版本",
      "论文写作",
      "求解记录",
      "设置",
    ]);
    expect(new Set(workspaceRoutes.map((route) => route.id)).size).toBe(7);
  });

  it("encodes project ids in workspace links", () => {
    expect(workspaceHref("project / 1", "models")).toBe(
      "/projects/project%20%2F%201/models",
    );
  });
});
