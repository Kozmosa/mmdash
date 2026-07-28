import { describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";

describe("example route", () => {
  it("proxies the Core response", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(
        JSON.stringify({
          status: "ok",
          storage: "postgres",
          checked_at: "2026-07-28T00:00:00Z",
        }),
        {
          headers: { "content-type": "application/json" },
          status: 200,
        },
      ),
    );
    const app = buildApp({
      coreBaseUrl: "http://core.test",
      fetchImplementation,
    });

    const response = await app.inject({
      method: "GET",
      url: "/api/example",
      headers: { "x-request-id": "request-test" },
    });

    expect(response.statusCode).toBe(200);
    expect(response.headers["x-request-id"]).toBe("request-test");
    expect(response.json()).toMatchObject({
      status: "ok",
      storage: "postgres",
    });
    expect(fetchImplementation).toHaveBeenCalledWith(
      "http://core.test/v1/example",
      { headers: { "x-request-id": "request-test" } },
    );
    await app.close();
  });
});
