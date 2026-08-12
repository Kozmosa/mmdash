import type { CoreClient } from "@mmdash/core-client";
import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("public user API boundary", () => {
  it("forwards the original user token while Core remains private", async () => {
    const currentIdentity = vi.fn().mockResolvedValue({
      kind: "session",
      user: { id: "user-1" },
    });
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ items: [] }), {
        headers: { "content-type": "application/json" },
      }),
    );
    const app = buildApp({
      config: testConfig,
      coreClient: { currentIdentity, fetch } as unknown as CoreClient,
      logger: false,
    });
    apps.push(app);

    const response = await app.inject({
      headers: { authorization: "Bearer user-session-token" },
      method: "GET",
      url: "/v1/projects?limit=25",
    });

    expect(response.statusCode).toBe(200);
    expect(currentIdentity).toHaveBeenCalledWith({
      accessToken: "user-session-token",
      requestId: expect.any(String),
    });
    expect(fetch).toHaveBeenCalledWith(
      "/v1/projects?limit=25",
      expect.objectContaining({ method: "GET" }),
      expect.objectContaining({ accessToken: "user-session-token" }),
    );
  });

  it("rejects Agent tokens instead of exposing the Core HTTP API", async () => {
    const currentIdentity = vi.fn().mockResolvedValue({
      agent_instance_id: "agent-1",
      credential_status: "active",
      kind: "agent",
      project_id: "project-1",
      token_id: "token-1",
    });
    const fetch = vi.fn();
    const app = buildApp({
      config: testConfig,
      coreClient: { currentIdentity, fetch } as unknown as CoreClient,
      logger: false,
    });
    apps.push(app);

    const response = await app.inject({
      headers: { authorization: "Bearer mmdash-agent-token" },
      method: "POST",
      payload: {
        agent_instance_id: "agent-1",
        challenge: "mmdash_challenge_secret",
        mcp_method: "tools/list",
        mcp_session_id: "session-1",
        project_id: "project-1",
        request_id: "request-1",
      },
      url: "/v1/auth/agent-tokens/token-1/verification",
    });

    expect(response.statusCode).toBe(403);
    expect(response.json()).toMatchObject({
      code: "PUBLIC_API_IDENTITY_FORBIDDEN",
    });
    expect(fetch).not.toHaveBeenCalled();
  });

  it("passes public login through without inventing a service credential", async () => {
    const currentIdentity = vi.fn();
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ access_token: "user-token" }), {
        headers: { "content-type": "application/json" },
      }),
    );
    const app = buildApp({
      config: testConfig,
      coreClient: { currentIdentity, fetch } as unknown as CoreClient,
      logger: false,
    });
    apps.push(app);

    const response = await app.inject({
      method: "POST",
      payload: { email: "user@example.com", password: "password" },
      url: "/v1/auth/login",
    });

    expect(response.statusCode).toBe(200);
    expect(currentIdentity).not.toHaveBeenCalled();
    expect(fetch).toHaveBeenCalledWith(
      "/v1/auth/login",
      expect.objectContaining({ method: "POST" }),
      expect.objectContaining({ accessToken: undefined }),
    );
  });
});
