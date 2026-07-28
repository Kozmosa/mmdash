import { describe, expect, it } from "vitest";

import { PageAggregatorRegistry } from "../src/aggregation/page-aggregator.js";

describe("PageAggregatorRegistry", () => {
  it("composes named fragments", async () => {
    const registry = new PageAggregatorRegistry();
    registry.register("dashboard", [
      { id: "summary", load: async () => ({ total: 2 }) },
      { id: "status", load: async () => "ready" },
    ]);

    await expect(
      registry.aggregate("dashboard", {
        coreClient: {} as never,
        identity: {
          displayName: "Test",
          email: "test@example.com",
          sessionId: "session-1",
          userId: "user-1",
        },
        projectId: "project-1",
        requestId: "request-1",
      }),
    ).resolves.toMatchObject({
      fragments: {
        status: "ready",
        summary: { total: 2 },
      },
    });
  });

  it("rejects duplicate page and fragment registrations", () => {
    const registry = new PageAggregatorRegistry();
    registry.register("dashboard", []);
    expect(() => registry.register("dashboard", [])).toThrow(
      'Page aggregator "dashboard" is already registered',
    );
    expect(() =>
      new PageAggregatorRegistry().register("bad", [
        { id: "same", load: async () => null },
        { id: "same", load: async () => null },
      ]),
    ).toThrow('Page aggregator "bad" has duplicate fragments');
  });
});
