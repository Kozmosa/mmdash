import {
  Client,
  StreamableHTTPClientTransport,
} from "@modelcontextprotocol/client";
import type { CoreClient } from "@mmdash/core-client";
import { afterEach, describe, expect, it, vi } from "vitest";

import { MemoryAuditSink } from "../src/audit/audit.js";
import { buildGateway, type GatewayFetchHandler } from "../src/gateway.js";
import { gatewaySessionHeader } from "../src/sessions/session-registry.js";
import { agentToken, cliToken, testConfig } from "./helpers.js";

const gateways: GatewayFetchHandler[] = [];

afterEach(async () => {
  await Promise.all(gateways.splice(0).map((gateway) => gateway.close()));
});

describe("MCP Gateway", () => {
  it("rejects unauthenticated requests before MCP dispatch", async () => {
    const gateway = buildGateway({ config: testConfig });
    gateways.push(gateway);

    const response = await gateway.fetch(
      new Request("http://test.local/mcp", {
        headers: { "content-type": "application/json" },
        method: "POST",
        body: JSON.stringify({
          id: 1,
          jsonrpc: "2.0",
          method: "ping",
        }),
      }),
    );

    expect(response.status).toBe(401);
    expect(response.headers.get("www-authenticate")).toContain("Bearer");
    await expect(response.json()).resolves.toMatchObject({
      code: "UNAUTHENTICATED",
    });
  });

  it("lists and calls the test tool over modern Streamable HTTP", async () => {
    const audit = new MemoryAuditSink();
    const gateway = buildGateway({ audit, config: testConfig });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, cliToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-gateway-test", version: "0.1.0" },
      {
        versionNegotiation: {
          mode: { pin: "2026-07-28" },
        },
      },
    );

    await client.connect(transport);
    const listed = await client.listTools();
    const result = await client.callTool({
      arguments: {
        message: "hello",
        project_id: "project-1",
      },
      name: "system.echo",
    });
    await client.close();

    expect(listed.tools.map((tool) => tool.name)).toEqual([
      "data.list",
      "data.read",
      "project.get",
      "project.list",
      "project.member.get",
      "project.member.list",
      "system.echo",
    ]);
    expect(result.isError).not.toBe(true);
    expect(result.structuredContent).toMatchObject({
      message: "hello",
      principal_kind: "cli",
      project_id: "project-1",
      session_id: sessionFetch.sessionId,
    });
    expect(audit.events).toHaveLength(1);
    expect(audit.events[0]).toMatchObject({
      outcome: "success",
      projectId: "project-1",
      toolName: "system.echo",
    });
  });

  it("lists and reads projects through the delegated Core boundary", async () => {
    const audit = new MemoryAuditSink();
    const listProjects = vi.fn().mockResolvedValue({
      items: [
        { id: "allowed-project", name: "Allowed" },
        { id: "blocked-project", name: "Blocked" },
      ],
    });
    const getProject = vi.fn().mockResolvedValue({
      id: "allowed-project",
      name: "Allowed",
      problem_title: "Optimization",
    });
    const gateway = buildGateway({
      audit,
      config: { ...testConfig, cliProjects: ["allowed-project"] },
      coreClient: { getProject, listProjects } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, cliToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-project-test", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const listed = await client.callTool({
      arguments: {},
      name: "project.list",
    });
    const read = await client.callTool({
      arguments: { project_id: "allowed-project" },
      name: "project.get",
    });
    await client.close();

    expect(listed.structuredContent).toMatchObject({
      items: [{ id: "allowed-project" }],
    });
    expect(read.structuredContent).toMatchObject({ id: "allowed-project" });
    expect(getProject).toHaveBeenCalledWith(
      "allowed-project",
      expect.objectContaining({
        accessToken: "test-core-access-token-that-is-at-least-32-characters",
      }),
    );
    expect(audit.events.map((event) => event.toolName)).toEqual([
      "project.list",
      "project.get",
    ]);
  });

  it("reads Data Hub objects through Core with project scope and audit", async () => {
    const audit = new MemoryAuditSink();
    const listDataObjects = vi.fn().mockResolvedValue({
      has_more: false,
      items: [{ object_id: "00000000-0000-4000-8000-000000000091" }],
    });
    const readDataObject = vi.fn().mockResolvedValue({
      content: { resolved_revision: "a".repeat(40) },
      object: { object_id: "00000000-0000-4000-8000-000000000091" },
    });
    const gateway = buildGateway({
      audit,
      config: testConfig,
      coreClient: { listDataObjects, readDataObject } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, cliToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-data-test", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const listed = await client.callTool({
      arguments: {
        limit: 25,
        project_id: "project-1",
        type: "repo_file",
      },
      name: "data.list",
    });
    const read = await client.callTool({
      arguments: {
        object_id: "00000000-0000-4000-8000-000000000091",
        project_id: "project-1",
      },
      name: "data.read",
    });
    await client.close();

    expect(listed.isError).not.toBe(true);
    expect(read.isError).not.toBe(true);
    expect(listDataObjects).toHaveBeenCalledWith(
      "project-1",
      expect.objectContaining({
        accessToken: "test-core-access-token-that-is-at-least-32-characters",
        projectId: "project-1",
      }),
      { cursor: undefined, limit: 25, type: "repo_file" },
    );
    expect(readDataObject).toHaveBeenCalledWith(
      "project-1",
      "00000000-0000-4000-8000-000000000091",
      expect.objectContaining({
        accessToken: "test-core-access-token-that-is-at-least-32-characters",
      }),
    );
    expect(audit.events).toHaveLength(2);
    expect(audit.events.map((event) => event.toolName)).toEqual([
      "data.list",
      "data.read",
    ]);
  });

  it("reads Artifact metadata and a short-lived controlled download through generic data tools", async () => {
    const listDataObjects = vi.fn().mockResolvedValue({
      has_more: false,
      items: [
        {
          object_id: "00000000-0000-4000-8000-000000000092",
          object_type: "artifact",
        },
      ],
    });
    const readDataObject = vi.fn().mockResolvedValue({
      content: {
        detail: {
          artifact: {
            artifact_id: "00000000-0000-4000-8000-000000000102",
          },
        },
        download: {
          transfer: {
            expires_at: "2026-07-30T12:01:00Z",
            method: "GET",
            url: "http://core.local/v1/artifact-transfers/short-lived-token",
          },
        },
      },
      object: {
        object_id: "00000000-0000-4000-8000-000000000092",
        object_type: "artifact",
      },
    });
    const gateway = buildGateway({
      config: testConfig,
      coreClient: { listDataObjects, readDataObject } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, cliToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-artifact-data-test", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const listed = await client.callTool({
      arguments: { project_id: "project-1", type: "artifact" },
      name: "data.list",
    });
    const read = await client.callTool({
      arguments: {
        object_id: "00000000-0000-4000-8000-000000000092",
        project_id: "project-1",
      },
      name: "data.read",
    });
    await client.close();

    expect(listed.structuredContent).toMatchObject({
      items: [{ object_type: "artifact" }],
    });
    expect(read.structuredContent).toMatchObject({
      content: {
        download: {
          transfer: {
            expires_at: "2026-07-30T12:01:00Z",
            method: "GET",
          },
        },
      },
    });
    expect(listDataObjects).toHaveBeenCalledWith(
      "project-1",
      expect.any(Object),
      expect.objectContaining({ type: "artifact" }),
    );
  });

  it("denies out-of-scope Data Hub reads before Core and audits the denial", async () => {
    const audit = new MemoryAuditSink();
    const listDataObjects = vi.fn();
    const gateway = buildGateway({
      audit,
      config: { ...testConfig, cliProjects: ["allowed-project"] },
      coreClient: { listDataObjects } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, cliToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-data-denial-test", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const result = await client.callTool({
      arguments: { project_id: "blocked-project", type: "repository" },
      name: "data.list",
    });
    await client.close();

    expect(result.isError).toBe(true);
    expect(listDataObjects).not.toHaveBeenCalled();
    expect(audit.events).toHaveLength(1);
    expect(audit.events[0]).toMatchObject({
      errorCode: "PROJECT_ACCESS_DENIED",
      outcome: "denied",
      projectId: "blocked-project",
      toolName: "data.list",
    });
  });

  it("enforces project permissions and records denials", async () => {
    const audit = new MemoryAuditSink();
    const gateway = buildGateway({ audit, config: testConfig });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, agentToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-agent-test", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const result = await client.callTool({
      arguments: {
        message: "blocked",
        project_id: "another-project",
      },
      name: "system.echo",
    });
    await client.close();

    expect(result.isError).toBe(true);
    expect(result.content).toEqual([
      expect.objectContaining({
        text: expect.stringContaining("PROJECT_ACCESS_DENIED"),
        type: "text",
      }),
    ]);
    expect(audit.events[0]).toMatchObject({
      errorCode: "PROJECT_ACCESS_DENIED",
      outcome: "denied",
    });
  });
});

function createSessionFetch(
  gateway: GatewayFetchHandler,
  token: string,
): {
  fetch: typeof fetch;
  readonly sessionId: string | undefined;
} {
  let sessionId: string | undefined;
  return {
    fetch: async (input, init) => {
      const original = new Request(input, init);
      const headers = new Headers(original.headers);
      headers.set("authorization", `Bearer ${token}`);
      if (sessionId) {
        headers.set(gatewaySessionHeader, sessionId);
      }
      const response = await gateway.fetch(new Request(original, { headers }));
      sessionId = response.headers.get(gatewaySessionHeader) ?? undefined;
      return response;
    },
    get sessionId() {
      return sessionId;
    },
  };
}
