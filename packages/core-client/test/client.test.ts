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
    const fetchImplementation = vi.fn<typeof fetch>().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({ items: [], has_more: false }), {
          headers: { "content-type": "application/json" },
        }),
      ),
    );
    const client = new CoreClient("http://core.test", fetchImplementation);
    const context = {
      accessToken: "session-token",
      projectId: "project/1",
      requestId: "request-1",
      userId: "user-1",
    };

    await client.listDataObjects("project/1", context, {
      cursor: "next",
      limit: 25,
      type: "project context",
    });
    await client.readDataObject("project/1", "object/1", context);
    await client.getProjectHome("project/1", context);

    expect(fetchImplementation.mock.calls).toHaveLength(3);
    expectRequest(fetchImplementation.mock.calls[0], {
      method: "GET",
      url: "http://core.test/v1/data/projects/project%2F1/objects?cursor=next&limit=25&type=project+context",
      context,
    });
    expectRequest(fetchImplementation.mock.calls[1], {
      method: "GET",
      url: "http://core.test/v1/data/projects/project%2F1/objects/object%2F1",
      context,
    });
    expectRequest(fetchImplementation.mock.calls[2], {
      method: "GET",
      url: "http://core.test/v1/data/projects/project%2F1/home",
      context,
    });
  });

  it("uses stable audit ingestion and search routes", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({ items: [], has_more: false }), {
          headers: { "content-type": "application/json" },
        }),
      ),
    );
    const client = new CoreClient("http://core.test", fetchImplementation);
    const context = {
      accessToken: "audit-token",
      projectId: "project-1",
      requestId: "request-1",
      userId: "user-1",
    };
    const event = {
      action: "mcp.tool.called",
      category: "mcp",
      outcome: "success" as const,
      source: "mcp-gateway",
    };

    await client.recordAuditEvent(event, context);
    await client.listAuditEvents(context, {
      action: "mcp.tool.called",
      limit: 25,
      projectId: "project-1",
    });

    expectRequest(fetchImplementation.mock.calls[0], {
      body: event,
      context,
      method: "POST",
      url: "http://core.test/v1/audit/events",
    });
    expectRequest(fetchImplementation.mock.calls[1], {
      context,
      method: "GET",
      url: "http://core.test/v1/audit/events?action=mcp.tool.called&limit=25&project_id=project-1",
    });
  });
});

function expectRequest(
  call: readonly [RequestInfo | URL, RequestInit | undefined] | undefined,
  expected: {
    body?: unknown;
    context: {
      accessToken: string;
      projectId: string;
      requestId: string;
      userId: string;
    };
    method: string;
    url: string;
  },
): void {
  expect(call?.[0]).toBe(expected.url);
  expect(call?.[1]?.method).toBe(expected.method);
  expect(call?.[1]?.body).toBe(
    expected.body === undefined ? undefined : JSON.stringify(expected.body),
  );
  const headers = new Headers(call?.[1]?.headers);
  expect(headers.get("authorization")).toBe(
    `Bearer ${expected.context.accessToken}`,
  );
  expect(headers.get("x-mmdash-project-id")).toBe(expected.context.projectId);
  expect(headers.get("x-mmdash-request-id")).toBeNull();
  expect(headers.get("x-request-id")).toBe(expected.context.requestId);
  expect(headers.get("x-mmdash-user-id")).toBe(expected.context.userId);
}
