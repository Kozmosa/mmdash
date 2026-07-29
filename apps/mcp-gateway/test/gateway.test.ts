import {
  Client,
  StreamableHTTPClientTransport,
} from "@modelcontextprotocol/client";
import { afterEach, describe, expect, it } from "vitest";

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
      const response = await gateway.fetch(
        new Request(original, { headers }),
      );
      sessionId = response.headers.get(gatewaySessionHeader) ?? undefined;
      return response;
    },
    get sessionId() {
      return sessionId;
    },
  };
}
