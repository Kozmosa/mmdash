import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { sessionCookieName } from "../src/auth/browser-auth.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("auth and collaborative project routes", () => {
  it("accepts signed browser sessions created before persistent cookie expiry was added", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json({
        kind: "session",
        session_id: "session-1",
        user: {
          created_at: "2026-07-28T00:00:00Z",
          display_name: "Test User",
          email: "test@example.com",
          id: "user-1",
          status: "active",
          system_role: "admin",
        },
      }),
    );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);

    const response = await app.inject({
      headers: {
        cookie: await signedSessionCookie(app, {
          session_expires_at: undefined,
        }),
      },
      method: "GET",
      url: "/api/auth/me",
    });

    expect(response.statusCode).toBe(200);
    expect(response.json().user.id).toBe("user-1");
  });

  it("requires a browser session and forwards CLI device approval to Core", async () => {
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValue(new Response(null, { status: 204 }));
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);

    const unauthorized = await app.inject({
      method: "POST",
      payload: { approve: true, user_code: "ABCD-EFGH" },
      url: "/api/auth/device/verify",
    });
    expect(unauthorized.statusCode).toBe(401);

    const response = await app.inject({
      headers: { cookie: await signedSessionCookie(app) },
      method: "POST",
      payload: { approve: true, user_code: "abcd-efgh" },
      url: "/api/auth/device/verify",
    });

    expect(response.statusCode).toBe(204);
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe("http://core.test/v1/auth/device/verify");
    expect(new Headers(options?.headers).get("authorization")).toBe(
      "Bearer test-access-token",
    );
    expect(JSON.parse(String(options?.body))).toEqual({
      approve: true,
      user_code: "abcd-efgh",
    });
  });

  it("registers a browser account and creates a signed session", async () => {
    const registerResult = {
      access_token: "new-core-session-token",
      expires_at: new Date(Date.now() + 3_600_000).toISOString(),
      refresh_token: "new-refresh-token-that-is-at-least-32-characters",
      session_expires_at: new Date(
        Date.now() + 30 * 24 * 3_600_000,
      ).toISOString(),
      session_id: "session-new",
      user: {
        created_at: "2026-07-29T08:00:00Z",
        display_name: "New Member",
        email: "member@example.com",
        id: "user-new",
        status: "active",
        system_role: "member",
      },
    };
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValue(Response.json(registerResult, { status: 201 }));
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);

    const response = await app.inject({
      method: "POST",
      payload: {
        display_name: "New Member",
        email: "member@example.com",
        password: "password-123",
      },
      url: "/api/auth/register",
    });

    expect(response.statusCode).toBe(201);
    expect(response.headers["set-cookie"]).toContain(`${sessionCookieName}=`);
    expect(response.json().user).toMatchObject({
      display_name: "New Member",
      email: "member@example.com",
    });
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe("http://core.test/v1/auth/register");
    expect(options?.method).toBe("POST");
    expect(JSON.parse(String(options?.body))).toEqual({
      display_name: "New Member",
      email: "member@example.com",
      password: "password-123",
    });
  });

  it("creates an HTTP-only signed browser session from Core timestamps with offsets", async () => {
    const loginResult = {
      access_token: "core-session-token",
      expires_at: new Date(Date.now() + 3_600_000).toISOString(),
      refresh_token: "login-refresh-token-that-is-at-least-32-characters",
      session_expires_at: new Date(
        Date.now() + 30 * 24 * 3_600_000,
      ).toISOString(),
      session_id: "session-1",
      user: {
        created_at: "2026-07-28T08:00:00+08:00",
        display_name: "Team Owner",
        email: "owner@example.com",
        id: "user-1",
        status: "active",
        system_role: "admin",
      },
    };
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(Response.json(loginResult))
      .mockResolvedValueOnce(
        Response.json({
          kind: "session",
          session_id: "session-1",
          user: loginResult.user,
        }),
      );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);

    const response = await app.inject({
      method: "POST",
      payload: { email: "owner@example.com", password: "secret" },
      url: "/api/auth/login",
    });

    expect(response.statusCode).toBe(200);
    expect(response.headers["set-cookie"]).toContain(`${sessionCookieName}=`);
    expect(response.headers["set-cookie"]).toContain("HttpOnly");
    expect(response.headers["set-cookie"]).toContain("SameSite=Lax");
    expect(response.headers["set-cookie"]).toContain("Expires=");
    const cookie = String(response.headers["set-cookie"]).split(";", 1)[0]!;
    const identity = await app.inject({
      headers: { cookie },
      method: "GET",
      url: "/api/auth/me",
    });
    expect(identity.statusCode).toBe(200);
    expect(identity.json().user).toMatchObject({
      created_at: "2026-07-28T08:00:00+08:00",
      email: "owner@example.com",
      id: "user-1",
      system_role: "admin",
    });
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe("http://core.test/v1/auth/login");
    expect(options?.method).toBe("POST");
  });

  it("rotates an expired browser access token before forwarding a request", async () => {
    const refreshed = {
      access_token: "rotated-access-token",
      expires_at: new Date(Date.now() + 3_600_000).toISOString(),
      refresh_token: "rotated-refresh-token-that-is-at-least-32-characters",
      session_expires_at: new Date(
        Date.now() + 30 * 24 * 3_600_000,
      ).toISOString(),
      session_id: "session-refreshed",
      user: {
        created_at: "2026-07-28T08:00:00Z",
        display_name: "Team Owner",
        email: "owner@example.com",
        id: "user-1",
        status: "active",
        system_role: "admin",
      },
    };
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(Response.json(refreshed))
      .mockResolvedValueOnce(
        Response.json({
          kind: "session",
          session_id: refreshed.session_id,
          user: refreshed.user,
        }),
      );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app, {
      expires_at: new Date(Date.now() - 1_000).toISOString(),
      refresh_token: "stale-refresh-token-that-is-at-least-32-characters",
    });

    const response = await app.inject({
      headers: { cookie },
      method: "GET",
      url: "/api/auth/me",
    });

    expect(response.statusCode).toBe(200);
    expect(response.headers["set-cookie"]).toContain(`${sessionCookieName}=`);
    const [refreshUrl, refreshOptions] = fetchImplementation.mock.calls[0]!;
    expect(refreshUrl).toBe("http://core.test/v1/auth/refresh");
    expect(JSON.parse(String(refreshOptions?.body))).toEqual({
      refresh_token: "stale-refresh-token-that-is-at-least-32-characters",
    });
    const [, identityOptions] = fetchImplementation.mock.calls[1]!;
    expect(new Headers(identityOptions?.headers).get("authorization")).toBe(
      "Bearer rotated-access-token",
    );
  });

  it("forwards the session token when listing team projects", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json({
        items: [
          {
            created_at: "2026-07-28T00:00:00Z",
            created_by: "user-1",
            id: "project-1",
            name: "Modeling Team",
            problem_constraints: [],
            problem_summary: "",
            problem_title: "",
            project_constraints: [],
            role: "owner",
            source_artifact_ids: [],
            updated_at: "2026-07-28T00:00:00Z",
          },
        ],
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
      method: "GET",
      url: "/api/projects",
    });

    expect(response.statusCode).toBe(200);
    expect(response.json().items[0]).toMatchObject({
      id: "project-1",
      role: "owner",
    });
    const [, options] = fetchImplementation.mock.calls[0]!;
    expect(new Headers(options?.headers).get("authorization")).toBe(
      "Bearer test-access-token",
    );
  });

  it("preserves the Core contract methods for project and member mutations", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockImplementation(() =>
      Promise.resolve(
        Response.json({
          created_at: "2026-07-28T00:00:00Z",
          display_name: "Member",
          email: "member@example.com",
          joined_at: "2026-07-28T00:00:00Z",
          role: "viewer",
          user_id: "00000000-0000-4000-8000-000000000002",
        }),
      ),
    );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);
    const projectId = "00000000-0000-4000-8000-000000000001";
    const userId = "00000000-0000-4000-8000-000000000002";

    const memberResponse = await app.inject({
      headers: { cookie },
      method: "PUT",
      payload: { role: "viewer" },
      url: `/api/projects/${projectId}/members/${userId}`,
    });
    expect(memberResponse.statusCode).toBe(200);
    const [memberUrl, memberOptions] = fetchImplementation.mock.calls[0]!;
    expect(memberUrl).toBe(
      `http://core.test/v1/projects/${projectId}/members/${userId}`,
    );
    expect(memberOptions?.method).toBe("PUT");

    const projectResponse = await app.inject({
      headers: { cookie },
      method: "PATCH",
      payload: { name: "Renamed" },
      url: `/api/projects/${projectId}`,
    });
    expect(projectResponse.statusCode).toBe(200);
    const [projectUrl, projectOptions] = fetchImplementation.mock.calls[1]!;
    expect(projectUrl).toBe(`http://core.test/v1/projects/${projectId}`);
    expect(projectOptions?.method).toBe("PATCH");

    const trashListResponse = await app.inject({
      headers: { cookie },
      method: "GET",
      url: "/api/projects/trash",
    });
    expect(trashListResponse.statusCode).toBe(200);
    const [trashListUrl, trashListOptions] = fetchImplementation.mock.calls[2]!;
    expect(trashListUrl).toBe("http://core.test/v1/projects/trash");
    expect(trashListOptions?.method).toBe("GET");

    const trashResponse = await app.inject({
      headers: { cookie },
      method: "DELETE",
      url: `/api/projects/${projectId}`,
    });
    expect(trashResponse.statusCode).toBe(204);
    const [trashUrl, trashOptions] = fetchImplementation.mock.calls[3]!;
    expect(trashUrl).toBe(`http://core.test/v1/projects/${projectId}`);
    expect(trashOptions?.method).toBe("DELETE");

    const restoreResponse = await app.inject({
      headers: { cookie },
      method: "POST",
      url: `/api/projects/${projectId}/restore`,
    });
    expect(restoreResponse.statusCode).toBe(200);
    const [restoreUrl, restoreOptions] = fetchImplementation.mock.calls[4]!;
    expect(restoreUrl).toBe(
      `http://core.test/v1/projects/${projectId}/restore`,
    );
    expect(restoreOptions?.method).toBe("POST");
  });
});
