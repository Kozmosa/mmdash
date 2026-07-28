import { describe, expect, it, vi } from "vitest";

import { CoreClient } from "../src/client.js";

describe("CoreClient", () => {
  it("propagates request context", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(
        JSON.stringify({
          status: "ok",
          storage: "postgres",
          checked_at: "2026-07-28T00:00:00Z",
        }),
        { headers: { "content-type": "application/json" } },
      ),
    );
    const client = new CoreClient("http://core.test/", fetchImplementation);

    await client.checkExample({
      projectId: "project-1",
      requestId: "request-1",
      userId: "user-1",
    });

    expect(fetchImplementation).toHaveBeenCalledWith(
      "http://core.test/v1/example",
      expect.objectContaining({
        method: "GET",
        headers: expect.any(Headers),
      }),
    );
    const request = fetchImplementation.mock.calls[0]?.[1];
    const headers = new Headers(request?.headers);
    expect(headers.get("x-request-id")).toBe("request-1");
    expect(headers.get("x-mmdash-project-id")).toBe("project-1");
    expect(headers.get("x-mmdash-user-id")).toBe("user-1");
  });

  it("maps Core errors", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ code: "NOT_FOUND", message: "Missing" }), {
        headers: { "content-type": "application/json" },
        status: 404,
      }),
    );
    const client = new CoreClient("http://core.test", fetchImplementation);

    await expect(
      client.request("/v1/missing", { method: "GET" }, { requestId: "r1" }),
    ).rejects.toMatchObject({
      body: { code: "NOT_FOUND", message: "Missing" },
      status: 404,
    });
  });

  it("uses stable Data Hub list, read, and home routes", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ items: [], has_more: false }), {
        headers: { "content-type": "application/json" },
      }),
    );
    const client = new CoreClient("http://core.test", fetchImplementation);
    const context = { requestId: "request-1" };

    await client.listDataObjects("project/1", context, {
      cursor: "next",
      limit: 25,
      type: "project context",
    });
    await client.readDataObject("project/1", "object/1", context);
    await client.getProjectHome("project/1", context);

    expect(fetchImplementation.mock.calls.map(([url]) => url)).toEqual([
      "http://core.test/v1/data/projects/project%2F1/objects?cursor=next&limit=25&type=project+context",
      "http://core.test/v1/data/projects/project%2F1/objects/object%2F1",
      "http://core.test/v1/data/projects/project%2F1/home",
    ]);
  });
});
