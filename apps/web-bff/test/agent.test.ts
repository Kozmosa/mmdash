import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];
const projectId = "00000000-0000-4000-8000-000000000001";
const instanceId = "00000000-0000-4000-8000-000000000002";
const sessionId = "00000000-0000-4000-8000-000000000003";
const runId = "00000000-0000-4000-8000-000000000004";
const tokenId = "00000000-0000-4000-8000-000000000005";

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("Agent BFF routes", () => {
  it("requires a signed browser session and one consistent project context", async () => {
    const fetchImplementation = vi.fn<typeof fetch>();
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);

    const unauthenticated = await app.inject({
      method: "GET",
      url: `/api/projects/${projectId}/agent-instances`,
    });
    expect(unauthenticated.statusCode).toBe(401);

    const cookie = await signedSessionCookie(app);
    const conflicting = await app.inject({
      headers: {
        cookie,
        "x-mmdash-project-id": "00000000-0000-4000-8000-000000000099",
      },
      method: "GET",
      url: `/api/projects/${projectId}/agent-instances`,
    });
    expect(conflicting.statusCode).toBe(400);
    expect(conflicting.json()).toMatchObject({
      code: "PROJECT_CONTEXT_CONFLICT",
    });
    expect(fetchImplementation).not.toHaveBeenCalled();
  });

  it("validates and proxies Agent creation with browser and project context", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json(
        {
          instance: instanceFixture(),
          one_time_credential: oneTimeCredential(),
        },
        { status: 201 },
      ),
    );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const response = await app.inject({
      headers: { cookie, "x-request-id": "agent-create-request" },
      method: "POST",
      payload: {
        allowed_tools: ["project.get", "data.read", "context.promote"],
        display_name: "Hermes",
        hermes_api_key: "hermes-api-key-input",
        management_mode: "manual",
        management_url: "https://dashboard.example.test/settings/mcp",
        profile: "default",
        request_timeout_seconds: 45,
        runtime_url: "https://hermes.example.test",
      },
      url: `/api/projects/${projectId}/agent-instances`,
    });

    expect(response.statusCode).toBe(201);
    expect(response.json()).toMatchObject({
      one_time_credential: {
        token: "mmdash_agent_plaintext_once",
      },
    });
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe(
      `http://core.test/v1/projects/${projectId}/agent-instances`,
    );
    expect(options?.method).toBe("POST");
    expect(JSON.parse(String(options?.body))).toMatchObject({
      allowed_tools: ["project.get", "data.read", "context.promote"],
      hermes_api_key: "hermes-api-key-input",
      management_mode: "manual",
      request_timeout_seconds: 45,
    });
    const headers = new Headers(options?.headers);
    expect(headers.get("authorization")).toBe("Bearer test-access-token");
    expect(headers.get("x-mmdash-project-id")).toBe(projectId);
    expect(headers.get("x-mmdash-user-id")).toBe("user-1");
    expect(headers.get("x-request-id")).toBe("agent-create-request");
  });

  it("enforces the canonical Agent request boundaries before Core", async () => {
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValue(
        Response.json({ instance: instanceFixture() }, { status: 201 }),
      );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);
    const validCreate = {
      allowed_tools: [
        "project.get",
        "data.list",
        "data.read",
        "context.promote",
      ],
      display_name: "D".repeat(120),
      hermes_api_key: "k".repeat(16),
      management_mode: "manual",
      profile: "p".repeat(64),
      request_timeout_seconds: 300,
      runtime_url: "https://hermes.example.test",
    };

    const accepted = await app.inject({
      headers: { cookie },
      method: "POST",
      payload: validCreate,
      url: `/api/projects/${projectId}/agent-instances`,
    });
    expect(accepted.statusCode).toBe(201);

    const rejectedRequests = [
      {
        method: "POST" as const,
        payload: { ...validCreate, display_name: "D".repeat(121) },
        url: `/api/projects/${projectId}/agent-instances`,
      },
      {
        method: "POST" as const,
        payload: { ...validCreate, hermes_api_key: "k".repeat(15) },
        url: `/api/projects/${projectId}/agent-instances`,
      },
      {
        method: "POST" as const,
        payload: { ...validCreate, profile: "p".repeat(65) },
        url: `/api/projects/${projectId}/agent-instances`,
      },
      ...[
        "Default",
        "research profile",
        "research.profile",
        "research/profile",
        "research\\profile",
        "hermes",
        "test",
        "tmp",
        "root",
        "sudo",
      ].map((profile) => ({
        method: "POST" as const,
        payload: { ...validCreate, profile },
        url: `/api/projects/${projectId}/agent-instances`,
      })),
      {
        method: "POST" as const,
        payload: { ...validCreate, request_timeout_seconds: 301 },
        url: `/api/projects/${projectId}/agent-instances`,
      },
      {
        method: "POST" as const,
        payload: {
          ...validCreate,
          allowed_tools: [...validCreate.allowed_tools, "project.get"],
        },
        url: `/api/projects/${projectId}/agent-instances`,
      },
      {
        method: "POST" as const,
        payload: { ...validCreate, allowed_tools: ["data.*"] },
        url: `/api/projects/${projectId}/agent-instances`,
      },
      {
        method: "POST" as const,
        payload: { ...validCreate, allowed_tools: ["project.list"] },
        url: `/api/projects/${projectId}/agent-instances`,
      },
      {
        method: "POST" as const,
        payload: { name: "r".repeat(121) },
        url: `/api/projects/${projectId}/agent-instances/${instanceId}/tokens/rotate`,
      },
      {
        method: "PATCH" as const,
        payload: { content: "p".repeat(50_001) },
        url: `/api/projects/${projectId}/agent-instances/${instanceId}/prompt`,
      },
      {
        method: "POST" as const,
        payload: { session_type: "main", title: "s".repeat(256) },
        url: `/api/projects/${projectId}/agent-instances/${instanceId}/sessions`,
      },
      {
        method: "POST" as const,
        payload: { reason: "r".repeat(501) },
        url: `/api/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/end`,
      },
      {
        method: "POST" as const,
        payload: { message_id: "m".repeat(501) },
        url: `/api/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runId}/rerun`,
      },
    ];
    for (const request of rejectedRequests) {
      const response = await app.inject({ headers: { cookie }, ...request });
      expect(response.statusCode).toBe(400);
    }
    expect(fetchImplementation).toHaveBeenCalledTimes(1);
  });

  it("keeps Hermes-allowed non-reserved profile names available", async () => {
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockImplementation(async () =>
        Response.json({ instance: instanceFixture() }, { status: 201 }),
      );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);
    for (const profile of ["chat", "profile", "default"]) {
      const response = await app.inject({
        headers: { cookie },
        method: "POST",
        payload: {
          allowed_tools: ["project.get", "data.read", "context.promote"],
          display_name: "Hermes",
          hermes_api_key: "hermes-api-key-input",
          management_mode: "manual",
          profile,
          runtime_url: "https://hermes.example.test",
        },
        url: `/api/projects/${projectId}/agent-instances`,
      });
      expect(response.statusCode).toBe(201);
    }
    expect(fetchImplementation).toHaveBeenCalledTimes(3);
  });

  it("never returns provider secrets and suppresses accidental auto-mode plaintext", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json(
        {
          instance: {
            ...instanceFixture({ management_mode: "auto" }),
            cloudflare_access_client_secret: "cf-secret-leak",
            dashboard_session_token: "dashboard-secret-leak",
            hermes_api_key: "hermes-secret-leak",
            token_hash: "hash-leak",
          },
          one_time_credential: oneTimeCredential(),
        },
        { status: 201 },
      ),
    );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const response = await app.inject({
      headers: { cookie },
      method: "POST",
      payload: {
        allowed_tools: ["project.get", "data.read"],
        dashboard_session_token: "dashboard-secret-input",
        display_name: "Managed Hermes",
        hermes_api_key: "hermes-secret-input",
        management_mode: "auto",
        management_url: "https://dashboard.example.test",
        runtime_url: "https://hermes.example.test",
      },
      url: `/api/projects/${projectId}/agent-instances`,
    });

    expect(response.statusCode).toBe(201);
    expect(response.body).not.toContain("secret-leak");
    expect(response.body).not.toContain("hash-leak");
    expect(response.json()).not.toHaveProperty("one_time_credential");
  });

  it("returns one-time rotation material when a manual scope update requires it", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json({
        instance: {
          ...instanceFixture(),
          grant: {
            ...instanceFixture().grant,
            allowed_tools: ["project.get", "data.read"],
          },
        },
        one_time_credential: oneTimeCredential(),
      }),
    );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const response = await app.inject({
      headers: { cookie },
      method: "PATCH",
      payload: { allowed_tools: ["project.get", "data.read"] },
      url: `/api/projects/${projectId}/agent-instances/${instanceId}`,
    });

    expect(response.statusCode).toBe(200);
    expect(response.json()).toMatchObject({
      instance: {
        grant: { allowed_tools: ["project.get", "data.read"] },
      },
      one_time_credential: { token: "mmdash_agent_plaintext_once" },
    });
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe(
      `http://core.test/v1/projects/${projectId}/agent-instances/${instanceId}`,
    );
    expect(options?.method).toBe("PATCH");
    expect(JSON.parse(String(options?.body))).toEqual({
      allowed_tools: ["project.get", "data.read"],
    });
  });

  it("mirrors session, Run, and two-phase Token actions", async () => {
    const calls: { body?: unknown; method?: string; url: string }[] = [];
    const fetchImplementation = vi.fn<typeof fetch>().mockImplementation(
      async (input, options) => {
        calls.push({
          body: options?.body ? JSON.parse(String(options.body)) : undefined,
          method: options?.method,
          url: String(input),
        });
        if (String(input).endsWith("/tokens/rotate")) {
          return Response.json(
            {
              credential: credentialFixture(),
              old_credential_remains_active: true,
              one_time_credential: oneTimeCredential(),
              rotation_status: "awaiting_user",
            },
            { status: 201 },
          );
        }
        if (String(input).endsWith(`/tokens/${tokenId}/abort`)) {
          return Response.json({
            credential: { ...credentialFixture(), status: "revoked" },
            old_credential_remains_active: true,
          });
        }
        if (String(input).endsWith("/sessions")) {
          return Response.json(sessionFixture(), { status: 201 });
        }
        if (String(input).endsWith("/runs")) {
          return Response.json(
            { run: runFixture(), session: sessionFixture() },
            { status: 202 },
          );
        }
        if (String(input).endsWith("/regenerate")) {
          return Response.json(
            { run: runFixture(), session: sessionFixture() },
            { status: 202 },
          );
        }
        if (String(input).endsWith("/approvals/approval-1")) {
          return Response.json({
            ...runFixture(),
            status: "running",
          });
        }
        return new Response(null, { status: 204 });
      },
    );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const requests = [
      app.inject({
        headers: { cookie },
        method: "POST",
        payload: { name: "manual rotation" },
        url: `/api/projects/${projectId}/agent-instances/${instanceId}/tokens/rotate`,
      }),
      app.inject({
        headers: { cookie },
        method: "POST",
        url: `/api/projects/${projectId}/agent-instances/${instanceId}/tokens/${tokenId}/abort`,
      }),
      app.inject({
        headers: { cookie },
        method: "POST",
        payload: { default: true, session_type: "main", title: "Main" },
        url: `/api/projects/${projectId}/agent-instances/${instanceId}/sessions`,
      }),
      app.inject({
        headers: { cookie },
        method: "POST",
        payload: { message: "Hello Hermes" },
        url: `/api/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs`,
      }),
      app.inject({
        headers: { cookie },
        method: "POST",
        payload: { message_id: "message-1" },
        url: `/api/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runId}/regenerate`,
      }),
      app.inject({
        headers: { cookie },
        method: "POST",
        payload: { choice: "once" },
        url: `/api/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runId}/approvals/approval-1`,
      }),
    ];
    const responses = await Promise.all(requests);

    expect(responses.map((response) => response.statusCode)).toEqual([
      201, 200, 201, 202, 202, 200,
    ]);
    expect(calls).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          body: { name: "manual rotation" },
          method: "POST",
          url: `http://core.test/v1/projects/${projectId}/agent-instances/${instanceId}/tokens/rotate`,
        }),
        expect.objectContaining({
          method: "POST",
          url: `http://core.test/v1/projects/${projectId}/agent-instances/${instanceId}/tokens/${tokenId}/abort`,
        }),
        expect.objectContaining({
          body: { default: true, session_type: "main", title: "Main" },
          url: `http://core.test/v1/projects/${projectId}/agent-instances/${instanceId}/sessions`,
        }),
        expect.objectContaining({
          body: { message: "Hello Hermes" },
          url: `http://core.test/v1/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs`,
        }),
        expect.objectContaining({
          body: { message_id: "message-1" },
          url: `http://core.test/v1/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runId}/regenerate`,
        }),
        expect.objectContaining({
          body: { choice: "once" },
          url: `http://core.test/v1/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runId}/approvals/approval-1`,
        }),
      ]),
    );
  });

  it("streams Run events without buffering and forwards resume/auth context", async () => {
    let signal: AbortSignal | null = null;
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(
          new TextEncoder().encode(
            'id: event-8\ndata: {"event":"message.delta","delta":"Hel"}\n\n',
          ),
        );
        queueMicrotask(() => {
          controller.enqueue(
            new TextEncoder().encode(
              'id: event-9\ndata: {"event":"message.delta","delta":"lo"}\n\n',
            ),
          );
          controller.close();
        });
      },
    });
    const fetchImplementation = vi.fn<typeof fetch>().mockImplementation(
      async (_input, options) => {
        signal = options?.signal ?? null;
        return new Response(body, {
          headers: { "content-type": "text/event-stream" },
        });
      },
    );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const response = await app.inject({
      headers: { cookie, "last-event-id": "event-7" },
      method: "GET",
      url: `/api/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runId}/events`,
    });

    expect(response.statusCode).toBe(200);
    expect(response.headers["content-type"]).toContain("text/event-stream");
    expect(response.headers["cache-control"]).toContain("no-cache");
    expect(response.headers["x-accel-buffering"]).toBe("no");
    expect(response.body).toContain("Hel");
    expect(response.body).toContain("lo");
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe(
      `http://core.test/v1/projects/${projectId}/agent-instances/${instanceId}/sessions/${sessionId}/runs/${runId}/events`,
    );
    const headers = new Headers(options?.headers);
    expect(headers.get("authorization")).toBe("Bearer test-access-token");
    expect(headers.get("last-event-id")).toBe("event-7");
    expect(headers.get("x-mmdash-project-id")).toBe(projectId);
    expect(signal).not.toBeNull();
  });

  it("returns a safe browser error when Core includes a credential", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json(
        {
          code: "AGENT_RUNTIME_REJECTED",
          message:
            "Hermes API key mmdash_agent_super_secret_plaintext was rejected",
        },
        { status: 400 },
      ),
    );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const response = await app.inject({
      headers: { cookie },
      method: "GET",
      url: `/api/projects/${projectId}/agent-instances`,
    });

    expect(response.statusCode).toBe(400);
    expect(response.json()).toMatchObject({
      code: "AGENT_RUNTIME_REJECTED",
      message: "Core rejected the request",
    });
    expect(response.body).not.toContain("mmdash_agent_super_secret_plaintext");
  });
});

function instanceFixture(overrides: Record<string, unknown> = {}) {
  return {
    adapter_type: "hermes",
    agent_instance_id: instanceId,
    capabilities: {
      jobs: true,
      message_history: true,
      project_access: { configure: true, rotate: true, verify: true },
      run_approval: true,
      run_events: true,
      run_status: true,
      run_stop: true,
      runs: true,
      session_chat_stream: true,
      session_fork: true,
      sessions: true,
    },
    created_at: "2026-08-06T00:00:00Z",
    created_by: "00000000-0000-4000-8000-000000000010",
    credentials: [credentialFixture()],
    display_name: "Hermes",
    grant: {
      agent_instance_id: instanceId,
      allowed_tools: ["project.get", "data.read", "context.promote"],
      created_at: "2026-08-06T00:00:00Z",
      grant_id: "00000000-0000-4000-8000-000000000006",
      project_access_status: "verified",
      project_id: projectId,
      status: "active",
      updated_at: "2026-08-06T00:00:00Z",
      version: 1,
    },
    management_mode: "manual",
    management_path: "direct",
    management_url: "https://dashboard.example.test/settings/mcp",
    profile: "default",
    project_id: projectId,
    request_timeout_seconds: 30,
    runtime_url: "https://hermes.example.test",
    secrets: {
      cloudflare_access_configured: false,
      dashboard_session_token_configured: false,
      hermes_api_key_configured: true,
    },
    status: "active",
    updated_at: "2026-08-06T00:00:00Z",
    version: 1,
    ...overrides,
  };
}

function credentialFixture() {
  return {
    agent_instance_id: instanceId,
    allowed_tools: ["project.get", "data.read", "context.promote"],
    created_at: "2026-08-06T00:00:00Z",
    grant_id: "00000000-0000-4000-8000-000000000006",
    id: tokenId,
    name: "Hermes project token",
    project_id: projectId,
    status: "active",
  };
}

function oneTimeCredential() {
  return {
    credential: { ...credentialFixture(), status: "pending" },
    mcp_endpoint: "https://mcp.example.test/mcp",
    server_name: "mmdash",
    token: "mmdash_agent_plaintext_once",
  };
}

function sessionFixture() {
  return {
    agent_instance_id: instanceId,
    created_at: "2026-08-06T00:00:00Z",
    default: true,
    project_id: projectId,
    remote_session_id: "hermes-session-1",
    session_id: sessionId,
    session_type: "main",
    status: "active",
    title: "Main",
    updated_at: "2026-08-06T00:00:00Z",
    version: 1,
  };
}

function runFixture() {
  return {
    created_at: "2026-08-06T00:00:00Z",
    remote_run_id: "hermes-run-1",
    run_id: runId,
    session_id: sessionId,
    source: "message",
    status: "running",
    tool_calls: [],
    updated_at: "2026-08-06T00:00:00Z",
    version: 1,
  };
}
