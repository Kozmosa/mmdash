import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { sessionCookieName } from "../src/auth/browser-auth.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("auth and collaborative project routes", () => {
  it("creates an HTTP-only signed browser session from Core timestamps with offsets", async () => {
    const loginResult = {
      access_token: "core-session-token",
      expires_at: new Date(Date.now() + 60_000).toISOString(),
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
    const fetchImplementation = vi.fn<typeof fetch>()
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
});
