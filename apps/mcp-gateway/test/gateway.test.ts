import {
  Client,
  StreamableHTTPClientTransport,
} from "@modelcontextprotocol/client";
import { CoreClientError, type CoreClient } from "@mmdash/core-client";
import { afterEach, describe, expect, it, vi } from "vitest";

import { MemoryAuditSink } from "../src/audit/audit.js";
import { buildGateway, type GatewayFetchHandler } from "../src/gateway.js";
import {
  gatewaySessionHeader,
  mcpSessionHeader,
} from "../src/sessions/session-registry.js";
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

  it("rejects conflicting standard and application session headers", async () => {
    const gateway = buildGateway({ config: testConfig });
    gateways.push(gateway);
    const response = await gateway.fetch(
      new Request("http://test.local/mcp", {
        body: JSON.stringify({ id: 1, jsonrpc: "2.0", method: "ping" }),
        headers: {
          accept: "application/json, text/event-stream",
          authorization: `Bearer ${cliToken}`,
          "content-type": "application/json",
          [gatewaySessionHeader]: "application-session",
          [mcpSessionHeader]: "standard-session",
        },
        method: "POST",
      }),
    );

    expect(response.status).toBe(400);
    await expect(response.json()).resolves.toEqual({
      code: "MCP_SESSION_HEADER_CONFLICT",
      message: "MCP session headers do not match",
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
      "artifact.read",
      "artifact.upload",
      "context.promote",
      "data.list",
      "data.read",
      "experiment.create",
      "experiment.result.bind",
      "experiment.run",
      "experiment.status",
      "progress.get",
      "progress.recalculate",
      "project.get",
      "project.list",
      "project.member.get",
      "project.member.list",
      "result.get",
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
        accessToken: cliToken,
      }),
    );
    expect(audit.events.map((event) => event.toolName)).toEqual([
      "project.list",
      "project.get",
    ]);
  });

  it("returns the newest bounded persisted logs with experiment status", async () => {
    const projectId = "00000000-0000-4000-8000-000000000081";
    const experimentId = "00000000-0000-4000-8000-000000000082";
    const getExperiment = vi.fn().mockResolvedValue({
      execution_status: "running",
      experiment_id: experimentId,
      logs_truncated: false,
      project_id: projectId,
      retry: { latest_experiment_id: experimentId, retry_sequence: 0 },
    });
    const listExperimentLogs = vi.fn().mockResolvedValue({
      has_more: true,
      items: [{ message: "latest", sequence: 42, stream: "stdout" }],
      next_cursor: "42",
    });
    const gateway = buildGateway({
      config: testConfig,
      coreClient: {
        getExperiment,
        listExperimentLogs,
      } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, cliToken);
    const client = new Client(
      { name: "mmdash-experiment-status-test", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );
    await client.connect(
      new StreamableHTTPClientTransport(new URL("http://test.local/mcp"), {
        fetch: sessionFetch.fetch,
      }),
    );

    const result = await client.callTool({
      arguments: {
        experiment_id: experimentId,
        log_tail: 25,
        project_id: projectId,
      },
      name: "experiment.status",
    });
    await client.close();

    expect(result.structuredContent).toMatchObject({
      execution_status: "running",
      logs: [{ message: "latest", sequence: 42 }],
      logs_has_more: true,
    });
    expect(listExperimentLogs).toHaveBeenCalledWith(
      projectId,
      experimentId,
      expect.objectContaining({ accessToken: cliToken, projectId }),
      { limit: 25, tail: true },
    );
  });

  it("reads and recalculates Progress through the delegated Core boundary", async () => {
    const audit = new MemoryAuditSink();
    const getProgress = vi.fn().mockResolvedValue({
      project_id: "project-1",
      tracking: { effective_stage: "execution" },
    });
    const recalculateProgress = vi.fn().mockResolvedValue({
      merged: false,
      request_id: "00000000-0000-4000-8000-000000000001",
      scheduled_for: "2026-08-10T00:00:00Z",
      status: "pending",
    });
    const gateway = buildGateway({
      audit,
      config: testConfig,
      coreClient: { getProgress, recalculateProgress } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, cliToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-progress-test", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const read = await client.callTool({
      arguments: { project_id: "project-1" },
      name: "progress.get",
    });
    const recalculate = await client.callTool({
      arguments: {
        force: false,
        project_id: "project-1",
        trigger_kind: "manual",
      },
      name: "progress.recalculate",
    });
    await client.close();

    expect(read.structuredContent).toMatchObject({
      tracking: { effective_stage: "execution" },
    });
    expect(recalculate.structuredContent).toMatchObject({ status: "pending" });
    expect(recalculateProgress).toHaveBeenCalledWith(
      "project-1",
      { force: false, trigger_kind: "manual" },
      expect.objectContaining({
        accessToken: cliToken,
        projectId: "project-1",
      }),
    );
    expect(audit.events.map((event) => event.toolName)).toEqual([
      "progress.get",
      "progress.recalculate",
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
        accessToken: cliToken,
        projectId: "project-1",
      }),
      { cursor: undefined, limit: 25, type: "repo_file" },
    );
    expect(readDataObject).toHaveBeenCalledWith(
      "project-1",
      "00000000-0000-4000-8000-000000000091",
      expect.objectContaining({
        accessToken: cliToken,
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

  it("exposes only the exact product Agent grant", async () => {
    const currentIdentity = vi.fn().mockResolvedValue(productAgentIdentity());
    const gateway = buildGateway({
      config: productAgentConfig(),
      coreClient: { currentIdentity } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(
      gateway,
      productAgentToken,
      mcpSessionHeader,
    );
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-product-agent-tools", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const listed = await client.listTools();
    await client.close();

    expect(listed.tools.map((tool) => tool.name)).toEqual([
      "context.promote",
      "data.list",
      "data.read",
      "project.get",
    ]);
    expect(listed.tools.map((tool) => tool.name)).not.toContain(
      "project.member.list",
    );
  });

  it("records pending Agent verification only after an initialized exact tools/list", async () => {
    const currentIdentity = vi
      .fn()
      .mockResolvedValue(productAgentIdentity("pending"));
    const recordAgentTokenVerification = vi.fn(
      async (
        tokenId: string,
        input: Record<string, string>,
        _context: unknown,
      ) => {
        void _context;
        return {
          ...input,
          evidence_id: "00000000-0000-4000-8000-000000000041",
          token_id: tokenId,
          verified_at: "2026-08-06T00:00:00Z",
        };
      },
    );
    const getProject = vi.fn();
    const gateway = buildGateway({
      audit: new MemoryAuditSink(),
      config: productAgentConfig(),
      coreClient: {
        currentIdentity,
        getProject,
        recordAgentTokenVerification,
      } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(
      gateway,
      productAgentToken,
      mcpSessionHeader,
    );
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp?mmdash_challenge=one-time-challenge"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-pending-agent-verification", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const initializedSessionId = sessionFetch.sessionId;
    expect(recordAgentTokenVerification).not.toHaveBeenCalled();

    const blocked = await gateway.fetch(
      mcpRequest(productAgentToken, {
        body: {
          id: 2,
          jsonrpc: "2.0",
          method: "tools/call",
          params: {
            arguments: { project_id: productProjectId },
            name: "project.get",
          },
        },
        sessionId: initializedSessionId,
      }),
    );
    expect(blocked.status).toBe(403);
    await expect(blocked.json()).resolves.toEqual({
      code: "AGENT_CREDENTIAL_PENDING",
      message:
        "The pending Agent credential can only initialize and list tools",
    });
    expect(getProject).not.toHaveBeenCalled();
    expect(recordAgentTokenVerification).not.toHaveBeenCalled();

    const blockedResourceList = await gateway.fetch(
      mcpRequest(productAgentToken, {
        body: { id: 3, jsonrpc: "2.0", method: "resources/list" },
        sessionId: initializedSessionId,
      }),
    );
    expect(blockedResourceList.status).toBe(403);
    await expect(blockedResourceList.json()).resolves.toMatchObject({
      code: "AGENT_CREDENTIAL_PENDING",
    });

    const blockedBatch = await gateway.fetch(
      mcpRequest(productAgentToken, {
        body: [
          {
            id: 4,
            jsonrpc: "2.0",
            method: "tools/call",
            params: {
              arguments: { project_id: productProjectId },
              name: "project.get",
            },
          },
        ],
        sessionId: initializedSessionId,
      }),
    );
    expect(blockedBatch.status).toBe(403);
    expect(getProject).not.toHaveBeenCalled();

    const listed = await client.listTools();
    await client.close();

    expect(listed.tools.map((tool) => tool.name)).toEqual([
      "context.promote",
      "data.list",
      "data.read",
      "project.get",
    ]);
    expect(recordAgentTokenVerification).toHaveBeenCalledTimes(1);
    const [tokenId, input, context] =
      recordAgentTokenVerification.mock.calls[0]!;
    expect(tokenId).toBe("00000000-0000-4000-8000-000000000031");
    expect(input).toEqual({
      agent_instance_id: productAgentInstanceId,
      challenge: "one-time-challenge",
      mcp_method: "tools/list",
      mcp_session_id: initializedSessionId,
      project_id: productProjectId,
      request_id: expect.any(String),
    });
    expect(context).toEqual({
      accessToken: productAgentToken,
      projectId: productProjectId,
      requestId: input.request_id,
    });
  });

  it("accepts Core first-write verification evidence on repeated tools/list", async () => {
    let firstEvidence: Record<string, string> | undefined;
    const recordAgentTokenVerification = vi.fn(
      async (tokenId: string, input: Record<string, string>) => {
        firstEvidence ??= {
          ...input,
          evidence_id: "00000000-0000-4000-8000-000000000042",
          token_id: tokenId,
          verified_at: "2026-08-06T00:00:00Z",
        };
        return firstEvidence;
      },
    );
    const gateway = buildGateway({
      config: productAgentConfig(),
      coreClient: {
        currentIdentity: vi
          .fn()
          .mockResolvedValue(productAgentIdentity("pending")),
        recordAgentTokenVerification,
      } as unknown as CoreClient,
    });
    gateways.push(gateway);

    for (const name of ["first", "second"]) {
      const sessionFetch = createSessionFetch(gateway, productAgentToken);
      const transport = new StreamableHTTPClientTransport(
        new URL("http://test.local/mcp?mmdash_challenge=one-time-challenge"),
        { fetch: sessionFetch.fetch },
      );
      const client = new Client(
        { name: `mmdash-pending-agent-${name}`, version: "0.1.0" },
        { versionNegotiation: { mode: { pin: "2026-07-28" } } },
      );
      await client.connect(transport);
      await expect(client.listTools()).resolves.toMatchObject({
        tools: expect.any(Array),
      });
      await client.close();
    }

    expect(recordAgentTokenVerification).toHaveBeenCalledTimes(2);
    const firstInput = recordAgentTokenVerification.mock.calls[0]![1];
    const secondInput = recordAgentTokenVerification.mock.calls[1]![1];
    expect(secondInput.mcp_session_id).not.toBe(firstInput.mcp_session_id);
    expect(secondInput.request_id).not.toBe(firstInput.request_id);
  });

  it("does not verify an uninitialized or incomplete pending Agent tool list", async () => {
    const recordAgentTokenVerification = vi.fn();
    const gateway = buildGateway({
      config: productAgentConfig(),
      coreClient: {
        currentIdentity: vi.fn().mockResolvedValue({
          ...productAgentIdentity("pending"),
          allowed_tools: ["project.get", "future.missing"],
        }),
        recordAgentTokenVerification,
      } as unknown as CoreClient,
    });
    gateways.push(gateway);

    const uninitialized = await gateway.fetch(
      mcpRequest(productAgentToken, {
        body: { id: 1, jsonrpc: "2.0", method: "tools/list" },
      }),
    );
    expect(uninitialized.status).toBe(409);
    expect(recordAgentTokenVerification).not.toHaveBeenCalled();

    const sessionFetch = createSessionFetch(gateway, productAgentToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-incomplete-agent-grant", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );
    await client.connect(transport);

    const incomplete = await gateway.fetch(
      mcpRequest(productAgentToken, {
        body: { id: 3, jsonrpc: "2.0", method: "tools/list" },
        sessionId: sessionFetch.sessionId,
      }),
    );
    await client.close();

    expect(incomplete.status).toBe(503);
    await expect(incomplete.json()).resolves.toEqual({
      code: "AGENT_VERIFICATION_UNAVAILABLE",
      message: "Agent credential verification is temporarily unavailable",
    });
    expect(recordAgentTokenVerification).not.toHaveBeenCalled();
  });

  it("keeps pending verification failures fixed and secret-free", async () => {
    const secret = "trusted-core-provider-secret-must-not-leak";
    const recordAgentTokenVerification = vi.fn().mockRejectedValue(
      new CoreClientError(503, {
        code: "PROVIDER_FAILED",
        message: `upstream rejected ${secret}`,
      }),
    );
    const gateway = buildGateway({
      audit: new MemoryAuditSink(),
      config: productAgentConfig(),
      coreClient: {
        currentIdentity: vi
          .fn()
          .mockResolvedValue(productAgentIdentity("pending")),
        recordAgentTokenVerification,
      } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, productAgentToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-agent-verification-failure", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );
    await client.connect(transport);

    const response = await gateway.fetch(
      mcpRequest(productAgentToken, {
        body: { id: 4, jsonrpc: "2.0", method: "tools/list" },
        sessionId: sessionFetch.sessionId,
      }),
    );
    await client.close();

    expect(response.status).toBe(503);
    const body = await response.text();
    expect(body).toContain("AGENT_VERIFICATION_UNAVAILABLE");
    expect(body).not.toContain(secret);
    expect(body).not.toContain(productAgentToken);
  });

  it("binds MCP sessions to the specific Agent credential during rotation", async () => {
    const pendingToken =
      "rotated-pending-agent-token-that-is-at-least-32-characters";
    const recordAgentTokenVerification = vi.fn();
    const currentIdentity = vi.fn(async (context: { accessToken?: string }) =>
      context.accessToken === pendingToken
        ? {
            ...productAgentIdentity("pending"),
            token_id: "00000000-0000-4000-8000-000000000032",
          }
        : productAgentIdentity("active"),
    );
    const gateway = buildGateway({
      config: productAgentConfig(),
      coreClient: {
        currentIdentity,
        recordAgentTokenVerification,
      } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const activeSessionFetch = createSessionFetch(gateway, productAgentToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: activeSessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-agent-session-owner", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );
    await client.connect(transport);

    const reused = await gateway.fetch(
      mcpRequest(pendingToken, {
        body: { id: 5, jsonrpc: "2.0", method: "tools/list" },
        sessionId: activeSessionFetch.sessionId,
      }),
    );
    await client.close();

    expect(reused.status).toBe(403);
    await expect(reused.json()).resolves.toEqual({
      code: "SESSION_PRINCIPAL_MISMATCH",
      message: "The gateway session belongs to another principal",
    });
    expect(recordAgentTokenVerification).not.toHaveBeenCalled();
  });

  it("promotes context with the inbound product Agent token and safe audit", async () => {
    const audit = new MemoryAuditSink();
    const currentIdentity = vi.fn().mockResolvedValue(productAgentIdentity());
    const createContextProposal = vi.fn().mockResolvedValue({
      content: "Use the robust bound in the final model.",
      context_type: "decision",
      created_at: "2026-08-06T00:00:00Z",
      project_id: productProjectId,
      proposal_id: "00000000-0000-4000-8000-000000000051",
      proposed_by: "00000000-0000-4000-8000-000000000061",
      rationale: "The experiment remained feasible under perturbation.",
      review_note: "",
      source_object_ids: [],
      status: "pending",
      title: "Adopt robust bound",
      updated_at: "2026-08-06T00:00:00Z",
    });
    const gateway = buildGateway({
      audit,
      config: productAgentConfig(),
      coreClient: {
        createContextProposal,
        currentIdentity,
      } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, productAgentToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-context-promote", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const result = await client.callTool({
      arguments: {
        agent_run_id: "00000000-0000-4000-8000-000000000053",
        agent_session_id: "00000000-0000-4000-8000-000000000052",
        content: "Use the robust bound in the final model.",
        context_type: "decision",
        project_id: productProjectId,
        rationale: "The experiment remained feasible under perturbation.",
        title: "Adopt robust bound",
      },
      name: "context.promote",
    });
    await client.close();

    expect(result.isError).not.toBe(true);
    expect(result.structuredContent).toMatchObject({
      proposal_id: "00000000-0000-4000-8000-000000000051",
      status: "pending",
    });
    expect(createContextProposal).toHaveBeenCalledWith(
      productProjectId,
      {
        agent_run_id: "00000000-0000-4000-8000-000000000053",
        agent_session_id: "00000000-0000-4000-8000-000000000052",
        content: "Use the robust bound in the final model.",
        context_type: "decision",
        rationale: "The experiment remained feasible under perturbation.",
        source_object_ids: undefined,
        title: "Adopt robust bound",
      },
      {
        accessToken: productAgentToken,
        projectId: productProjectId,
        requestId: expect.any(String),
      },
    );
    expect(audit.events).toEqual([
      expect.objectContaining({
        actorId: `agent:${productAgentInstanceId}`,
        actorKind: "agent",
        outcome: "success",
        projectId: productProjectId,
        toolName: "context.promote",
      }),
    ]);
  });

  it("creates and completes an Agent Artifact through direct multipart grants", async () => {
    const audit = new MemoryAuditSink();
    const uploadId = "00000000-0000-4000-8000-000000000071";
    const initializeAgentArtifactUpload = vi.fn().mockResolvedValue({
      artifact_id: "00000000-0000-4000-8000-000000000072",
      completed_parts: [],
      created_at: "2026-08-11T00:00:00Z",
      expires_at: "2026-08-11T01:00:00Z",
      part_count: 1,
      part_size_bytes: 5_242_880,
      sha256: "a".repeat(64),
      size_bytes: 4,
      status: "initialized",
      transfer_mode: "direct",
      updated_at: "2026-08-11T00:00:00Z",
      upload_id: uploadId,
      upload_mode: "multipart",
      version_id: "00000000-0000-4000-8000-000000000073",
    });
    const signArtifactUploadParts = vi.fn().mockResolvedValue({
      items: [
        {
          part_number: 1,
          size_bytes: 4,
          transfer: {
            expires_at: "2026-08-11T00:10:00Z",
            headers: { "content-type": "image/png" },
            method: "PUT",
            url: "http://object-store.test/agent-part-1",
          },
        },
      ],
    });
    const confirmArtifactUpload = vi.fn().mockResolvedValue({
      artifact: {
        artifact_id: "00000000-0000-4000-8000-000000000072",
        kind: "agent",
        source: "agent",
      },
    });
    const gateway = buildGateway({
      audit,
      config: productAgentConfig(),
      coreClient: {
        confirmArtifactUpload,
        currentIdentity: vi.fn().mockResolvedValue({
          ...productAgentIdentity(),
          allowed_tools: ["artifact.upload"],
        }),
        getArtifactUpload: vi
          .fn()
          .mockResolvedValue({ transfer_mode: "direct" }),
        initializeAgentArtifactUpload,
        signArtifactUploadParts,
      } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, productAgentToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-agent-artifact", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const begun = await client.callTool({
      arguments: {
        action: "begin",
        filename: "plot.png",
        idempotency_key: "run-1-plot",
        mime_type: "image/png",
        project_id: productProjectId,
        sha256: "a".repeat(64),
        size_bytes: 4,
      },
      name: "artifact.upload",
    });
    const completed = await client.callTool({
      arguments: {
        action: "complete",
        parts: [{ etag: "part-etag", part_number: 1 }],
        project_id: productProjectId,
        upload_id: uploadId,
      },
      name: "artifact.upload",
    });
    await client.close();

    expect(begun.isError).not.toBe(true);
    expect(begun.structuredContent).toMatchObject({
      part_grants: [
        {
          part_number: 1,
          transfer: { method: "PUT" },
        },
      ],
      upload: { upload_id: uploadId },
    });
    expect(completed.structuredContent).toMatchObject({
      artifact: { artifact: { kind: "agent", source: "agent" } },
      upload_id: uploadId,
    });
    expect(initializeAgentArtifactUpload).toHaveBeenCalledWith(
      productProjectId,
      expect.objectContaining({ filename: "plot.png" }),
      {
        accessToken: productAgentToken,
        projectId: productProjectId,
        requestId: expect.any(String),
      },
    );
    expect(signArtifactUploadParts).toHaveBeenCalledWith(
      productProjectId,
      uploadId,
      { part_numbers: [1] },
      expect.objectContaining({ accessToken: productAgentToken }),
    );
    expect(confirmArtifactUpload).toHaveBeenCalledWith(
      productProjectId,
      uploadId,
      { parts: [{ etag: "part-etag", part_number: 1 }] },
      expect.objectContaining({ accessToken: productAgentToken }),
    );
    expect(audit.events).toHaveLength(2);
    expect(audit.events).toEqual([
      expect.objectContaining({
        outcome: "success",
        toolName: "artifact.upload",
      }),
      expect.objectContaining({
        outcome: "success",
        toolName: "artifact.upload",
      }),
    ]);
  });

  it("returns a short-lived Artifact download grant to a product Agent", async () => {
    const audit = new MemoryAuditSink();
    const artifactId = "00000000-0000-4000-8000-000000000072";
    const versionId = "00000000-0000-4000-8000-000000000073";
    const downloadArtifact = vi.fn().mockResolvedValue({
      artifact_id: artifactId,
      filename: "notes.txt",
      mime_type: "text/plain",
      transfer: {
        expires_at: "2026-08-11T00:10:00Z",
        headers: {},
        method: "GET",
        url: "http://object-store.test/signed-notes",
      },
      version_id: versionId,
    });
    const gateway = buildGateway({
      audit,
      config: productAgentConfig(),
      coreClient: {
        currentIdentity: vi.fn().mockResolvedValue({
          ...productAgentIdentity(),
          allowed_tools: ["artifact.read"],
        }),
        downloadArtifact,
      } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, productAgentToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-agent-artifact-read", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const result = await client.callTool({
      arguments: {
        artifact_id: artifactId,
        project_id: productProjectId,
        version_id: versionId,
      },
      name: "artifact.read",
    });
    await client.close();

    expect(result.isError).not.toBe(true);
    expect(result.structuredContent).toMatchObject({
      artifact_id: artifactId,
      transfer: { method: "GET" },
      version_id: versionId,
    });
    expect(downloadArtifact).toHaveBeenCalledWith(
      productProjectId,
      artifactId,
      expect.objectContaining({ accessToken: productAgentToken }),
      versionId,
    );
    expect(audit.events).toEqual([
      expect.objectContaining({
        outcome: "success",
        toolName: "artifact.read",
      }),
    ]);
  });

  it("aborts an Agent Artifact when only a Core-local transfer is available", async () => {
    const uploadId = "00000000-0000-4000-8000-000000000081";
    const abortArtifactUpload = vi.fn().mockResolvedValue(undefined);
    const gateway = buildGateway({
      audit: new MemoryAuditSink(),
      config: productAgentConfig(),
      coreClient: {
        abortArtifactUpload,
        currentIdentity: vi.fn().mockResolvedValue({
          ...productAgentIdentity(),
          allowed_tools: ["artifact.upload"],
        }),
        initializeAgentArtifactUpload: vi.fn().mockResolvedValue({
          part_count: 1,
          transfer_mode: "local_proxy",
          upload_id: uploadId,
          upload_mode: "multipart",
        }),
      } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, productAgentToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-agent-artifact-local", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const result = await client.callTool({
      arguments: {
        action: "begin",
        filename: "local.txt",
        idempotency_key: "local-transfer",
        project_id: productProjectId,
        sha256: "c".repeat(64),
        size_bytes: 5,
      },
      name: "artifact.upload",
    });
    await client.close();

    expect(result.isError).toBe(true);
    expect(JSON.stringify(result.content)).toContain(
      "ARTIFACT_DIRECT_TRANSFER_REQUIRED",
    );
    expect(abortArtifactUpload).toHaveBeenCalledWith(
      productProjectId,
      uploadId,
      expect.objectContaining({ accessToken: productAgentToken }),
    );
  });

  it("requires Agent Session and Run provenance as a pair", async () => {
    const createContextProposal = vi.fn();
    const gateway = buildGateway({
      audit: new MemoryAuditSink(),
      config: productAgentConfig(),
      coreClient: {
        createContextProposal,
        currentIdentity: vi.fn().mockResolvedValue(productAgentIdentity()),
      } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, productAgentToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-context-provenance", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const result = await client.callTool({
      arguments: {
        agent_session_id: "00000000-0000-4000-8000-000000000052",
        content: "Incomplete provenance",
        context_type: "finding",
        project_id: productProjectId,
        title: "Incomplete provenance",
      },
      name: "context.promote",
    });
    await client.close();

    expect(result.isError).toBe(true);
    expect(createContextProposal).not.toHaveBeenCalled();
  });

  it("denies out-of-scope context promotion before Core", async () => {
    const audit = new MemoryAuditSink();
    const createContextProposal = vi.fn();
    const gateway = buildGateway({
      audit,
      config: productAgentConfig(),
      coreClient: {
        createContextProposal,
        currentIdentity: vi.fn().mockResolvedValue(productAgentIdentity()),
      } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, productAgentToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-context-denied", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const result = await client.callTool({
      arguments: {
        content: "Out of scope",
        context_type: "decision",
        project_id: "00000000-0000-4000-8000-000000000099",
        title: "Blocked proposal",
      },
      name: "context.promote",
    });
    await client.close();

    expect(result.isError).toBe(true);
    expect(JSON.stringify(result.content)).toContain("PROJECT_ACCESS_DENIED");
    expect(createContextProposal).not.toHaveBeenCalled();
    expect(audit.events[0]).toMatchObject({
      errorCode: "PROJECT_ACCESS_DENIED",
      outcome: "denied",
      projectId: "00000000-0000-4000-8000-000000000099",
    });
  });

  it("redacts Core context errors from product Agent results", async () => {
    const secret = "hermes-api-key-must-not-leak";
    const gateway = buildGateway({
      audit: new MemoryAuditSink(),
      config: productAgentConfig(),
      coreClient: {
        createContextProposal: vi.fn().mockRejectedValue(
          new CoreClientError(503, {
            code: "HERMES_FAILED",
            message: `upstream rejected ${secret}`,
          }),
        ),
        currentIdentity: vi.fn().mockResolvedValue(productAgentIdentity()),
      } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, productAgentToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-context-safe-error", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    const result = await client.callTool({
      arguments: {
        content: "Safe failure",
        context_type: "finding",
        project_id: productProjectId,
        title: "Safe failure",
      },
      name: "context.promote",
    });
    await client.close();

    expect(result.isError).toBe(true);
    expect(JSON.stringify(result)).toContain("CORE_UNAVAILABLE");
    expect(JSON.stringify(result)).not.toContain(secret);
  });

  it("uses a trusted service credential for Core audit persistence", async () => {
    const stderr = vi
      .spyOn(process.stderr, "write")
      .mockImplementation(() => true);
    const recordAuditEvent = vi.fn().mockResolvedValue({});
    const gateway = buildGateway({
      config: productAgentConfig(),
      coreClient: {
        createContextProposal: vi.fn().mockResolvedValue({
          project_id: productProjectId,
          proposal_id: "00000000-0000-4000-8000-000000000051",
          status: "pending",
        }),
        currentIdentity: vi.fn().mockResolvedValue(productAgentIdentity()),
        recordAuditEvent,
      } as unknown as CoreClient,
    });
    gateways.push(gateway);
    const sessionFetch = createSessionFetch(gateway, productAgentToken);
    const transport = new StreamableHTTPClientTransport(
      new URL("http://test.local/mcp"),
      { fetch: sessionFetch.fetch },
    );
    const client = new Client(
      { name: "mmdash-agent-audit-token", version: "0.1.0" },
      { versionNegotiation: { mode: { pin: "2026-07-28" } } },
    );

    await client.connect(transport);
    await client.callTool({
      arguments: {
        content: "Audit this proposal",
        context_type: "finding",
        project_id: productProjectId,
        title: "Audit credential",
      },
      name: "context.promote",
    });
    await client.close();
    stderr.mockRestore();

    expect(recordAuditEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        action: "mcp.tool.called",
        project_id: productProjectId,
        resource_id: "context.promote",
      }),
      expect.objectContaining({
        accessToken: "gateway-audit-service-token-that-is-at-least-32-chars",
      }),
    );
  });
});

const productAgentToken = "product-agent-token-that-is-at-least-32-characters";
const productAgentInstanceId = "00000000-0000-4000-8000-000000000021";
const productProjectId = "00000000-0000-4000-8000-000000000011";

function productAgentIdentity(
  credentialStatus: "active" | "pending" = "active",
) {
  return {
    agent_instance_id: productAgentInstanceId,
    allowed_tools: ["project.get", "data.list", "data.read", "context.promote"],
    credential_status: credentialStatus,
    kind: "agent",
    project_id: productProjectId,
    token_id: "00000000-0000-4000-8000-000000000031",
  };
}

function productAgentConfig() {
  return {
    ...testConfig,
    agentToken: undefined,
    cliToken: undefined,
    coreAuditToken: "gateway-audit-service-token-that-is-at-least-32-chars",
  };
}

function mcpRequest(
  token: string,
  options: {
    body: Record<string, unknown> | Record<string, unknown>[];
    sessionId?: string;
  },
): Request {
  const headers = new Headers({
    accept: "application/json, text/event-stream",
    authorization: `Bearer ${token}`,
    "content-type": "application/json",
    "mcp-protocol-version": "2026-07-28",
  });
  if (options.sessionId) {
    headers.set(gatewaySessionHeader, options.sessionId);
  }
  return new Request("http://test.local/mcp", {
    body: JSON.stringify(options.body),
    headers,
    method: "POST",
  });
}

function createSessionFetch(
  gateway: GatewayFetchHandler,
  token: string,
  sessionHeader = gatewaySessionHeader,
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
        headers.set(sessionHeader, sessionId);
      }
      const response = await gateway.fetch(new Request(original, { headers }));
      sessionId = response.headers.get(sessionHeader) ?? undefined;
      return response;
    },
    get sessionId() {
      return sessionId;
    },
  };
}
