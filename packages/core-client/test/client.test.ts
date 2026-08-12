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

  it("records pending Agent Token challenge evidence with that Agent token", async () => {
    const evidence = {
      agent_instance_id: "11111111-1111-4111-8111-111111111111",
      evidence_id: "22222222-2222-4222-8222-222222222222",
      mcp_method: "tools/list" as const,
      mcp_session_id: "mcp-session-1",
      project_id: "33333333-3333-4333-8333-333333333333",
      request_id: "request-1",
      token_id: "44444444-4444-4444-8444-444444444444",
      verified_at: "2026-08-06T12:00:00Z",
    };
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify(evidence), {
        headers: { "content-type": "application/json" },
      }),
    );
    const client = new CoreClient("http://core.test", fetchImplementation);
    const context = {
      accessToken: "pending-agent-token",
      projectId: evidence.project_id,
      requestId: evidence.request_id,
      userId: "gateway-service",
    };
    const input = {
      agent_instance_id: evidence.agent_instance_id,
      challenge: "mmdash_challenge_one-time-material",
      mcp_method: evidence.mcp_method,
      mcp_session_id: evidence.mcp_session_id,
      project_id: evidence.project_id,
      request_id: evidence.request_id,
    };

    await expect(
      client.recordAgentTokenVerification(evidence.token_id, input, context),
    ).resolves.toEqual(evidence);
    expectRequest(fetchImplementation.mock.calls[0], {
      body: input,
      context,
      method: "POST",
      url: `http://core.test/v1/auth/agent-tokens/${evidence.token_id}/verification`,
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

  it("uses immutable Repo read routes and omits browser write surfaces", async () => {
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
      projectId: "project-1",
      requestId: "request-1",
      userId: "user-1",
    };
    const revision = "a".repeat(40);

    await client.listRepositoryCommits("project-1", "code", context, {
      cursor: "next",
      limit: 25,
    });
    await client.listRepositoryTree(
      "project-1",
      {
        cursor: "tree-next",
        limit: 100,
        path: "src/lib",
        revision,
        workspace: "code",
      },
      context,
    );
    await client.getRepositoryContent(
      "project-1",
      { path: "src/a b.ts", revision, workspace: "code" },
      context,
    );

    expectRequest(fetchImplementation.mock.calls[0], {
      context,
      method: "GET",
      url: "http://core.test/v1/projects/project-1/repository/commits?workspace=code&cursor=next&limit=25",
    });
    expectRequest(fetchImplementation.mock.calls[1], {
      context,
      method: "GET",
      url: `http://core.test/v1/projects/project-1/repository/tree?revision=${revision}&workspace=code&cursor=tree-next&limit=100&path=src%2Flib`,
    });
    expectRequest(fetchImplementation.mock.calls[2], {
      context,
      method: "GET",
      url: `http://core.test/v1/projects/project-1/repository/content?path=src%2Fa+b.ts&revision=${revision}&workspace=code`,
    });
  });

  it("uses the frozen Artifact multipart and library routes", async () => {
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
      projectId: "project-1",
      requestId: "request-1",
      userId: "user-1",
    };
    const upload = {
      filename: "problem.pdf",
      idempotency_key: "upload-1",
      kind: "problem" as const,
      sha256: "a".repeat(64),
      size_bytes: 42,
    };

    await client.initializeArtifactUpload("project-1", upload, context);
    await client.signArtifactUploadParts(
      "project-1",
      "upload-1",
      { part_numbers: [1, 2] },
      context,
    );
    await client.confirmArtifactUpload(
      "project-1",
      "upload-1",
      { parts: [{ etag: "etag-1", part_number: 1 }] },
      context,
    );
    await client.listArtifacts("project-1", context, {
      kind: "problem",
      limit: 25,
      source: "user_upload",
      status: "available",
      tag: "source",
    });
    await client.downloadArtifact(
      "project-1",
      "artifact-1",
      context,
      "version-1",
    );
    const agentUpload = {
      filename: "agent-plot.png",
      idempotency_key: "agent-upload-1",
      mime_type: "image/png",
      sha256: "b".repeat(64),
      size_bytes: 64,
    };
    await client.initializeAgentArtifactUpload(
      "project-1",
      agentUpload,
      context,
    );

    expectRequest(fetchImplementation.mock.calls[0], {
      body: upload,
      context,
      method: "POST",
      url: "http://core.test/v1/projects/project-1/artifacts/uploads",
    });
    expectRequest(fetchImplementation.mock.calls[1], {
      body: { part_numbers: [1, 2] },
      context,
      method: "POST",
      url: "http://core.test/v1/projects/project-1/artifacts/uploads/upload-1/parts/sign",
    });
    expectRequest(fetchImplementation.mock.calls[2], {
      body: { parts: [{ etag: "etag-1", part_number: 1 }] },
      context,
      method: "POST",
      url: "http://core.test/v1/projects/project-1/artifacts/uploads/upload-1/confirm",
    });
    expectRequest(fetchImplementation.mock.calls[3], {
      context,
      method: "GET",
      url: "http://core.test/v1/projects/project-1/artifacts?kind=problem&limit=25&source=user_upload&status=available&tag=source",
    });
    expectRequest(fetchImplementation.mock.calls[4], {
      context,
      method: "POST",
      url: "http://core.test/v1/projects/project-1/artifacts/artifact-1/versions/version-1/download",
    });
    expectRequest(fetchImplementation.mock.calls[5], {
      body: agentUpload,
      context,
      method: "POST",
      url: "http://core.test/v1/projects/project-1/artifacts/agent-uploads",
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
