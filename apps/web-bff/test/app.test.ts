import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("BFF application", () => {
  it("proxies the public example route with request context", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(
        JSON.stringify({
          status: "ok",
          storage: "postgres",
          checked_at: "2026-07-28T00:00:00Z",
        }),
        {
          headers: { "content-type": "application/json" },
          status: 200,
        },
      ),
    );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);

    const response = await app.inject({
      headers: { "x-request-id": "request-test" },
      method: "GET",
      url: "/api/example",
    });

    expect(response.statusCode).toBe(200);
    expect(response.headers["x-request-id"]).toBe("request-test");
    expect(response.json()).toMatchObject({
      status: "ok",
      storage: "postgres",
    });
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe("http://core.test/v1/example");
    expect(new Headers(options?.headers).get("x-request-id")).toBe(
      "request-test",
    );
  });

  it("requires a signed browser session for page aggregation", async () => {
    const app = buildApp({ config: testConfig, logger: false });
    apps.push(app);

    const unauthorized = await app.inject({
      method: "GET",
      url: "/api/projects/project-1/pages/workspace-shell",
    });
    expect(unauthorized.statusCode).toBe(401);
    expect(unauthorized.json()).toMatchObject({
      code: "UNAUTHENTICATED",
    });

    const cookie = await signedSessionCookie(app);
    const authorized = await app.inject({
      headers: { cookie },
      method: "GET",
      url: "/api/projects/project-1/pages/workspace-shell",
    });
    expect(authorized.statusCode).toBe(200);
    expect(authorized.json()).toMatchObject({
      fragments: {
        context: {
          project: { id: "project-1" },
          user: { id: "user-1" },
        },
      },
      page_id: "workspace-shell",
      project_id: "project-1",
    });
  });

  it("rejects conflicting project context", async () => {
    const app = buildApp({ config: testConfig, logger: false });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const response = await app.inject({
      headers: {
        cookie,
        "x-mmdash-project-id": "project-2",
      },
      method: "GET",
      url: "/api/projects/project-1/pages/workspace-shell",
    });

    expect(response.statusCode).toBe(400);
    expect(response.json()).toMatchObject({
      code: "PROJECT_CONTEXT_CONFLICT",
    });
  });

  it("converts Core failures without leaking internals", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(
        JSON.stringify({
          code: "DATABASE_DOWN",
          message: "postgres dial tcp secret-host",
        }),
        {
          headers: { "content-type": "application/json" },
          status: 503,
        },
      ),
    );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);

    const response = await app.inject({
      method: "GET",
      url: "/api/example",
    });

    expect(response.statusCode).toBe(502);
    expect(response.json()).toMatchObject({
      code: "CORE_UNAVAILABLE",
      message: "Core service is temporarily unavailable",
    });
    expect(response.body).not.toContain("secret-host");
  });
});
