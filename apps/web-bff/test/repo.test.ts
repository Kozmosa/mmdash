import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];
const revision = "a".repeat(40);

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("Repo browser routes", () => {
  it("requires browser authentication and one consistent project context", async () => {
    const fetchImplementation = vi.fn<typeof fetch>();
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);

    const unauthenticated = await app.inject({
      method: "GET",
      url: "/api/projects/project-1/repository",
    });
    expect(unauthenticated.statusCode).toBe(401);

    const cookie = await signedSessionCookie(app);
    const conflicting = await app.inject({
      headers: { cookie, "x-mmdash-project-id": "project-2" },
      method: "GET",
      url: "/api/projects/project-1/repository",
    });
    expect(conflicting.statusCode).toBe(400);
    expect(conflicting.json()).toMatchObject({
      code: "PROJECT_CONTEXT_CONFLICT",
    });
    expect(fetchImplementation).not.toHaveBeenCalled();
  });

  it("pins tree and content reads to a validated full commit SHA", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json({
        branch: "main",
        has_more: false,
        items: [],
        next_cursor: null,
        path: "src",
        resolved_revision: revision,
        workspace: "code",
      }),
    );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const invalid = await app.inject({
      headers: { cookie },
      method: "GET",
      url: "/api/projects/project-1/repository/tree?workspace=code&revision=main",
    });
    expect(invalid.statusCode).toBe(400);
    expect(fetchImplementation).not.toHaveBeenCalled();

    const response = await app.inject({
      headers: { cookie, "x-request-id": "repo-browser-request" },
      method: "GET",
      url: `/api/projects/project-1/repository/tree?workspace=code&revision=${revision}&path=src&limit=100`,
    });
    expect(response.statusCode).toBe(200);
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe(
      `http://core.test/v1/projects/project-1/repository/tree?revision=${revision}&workspace=code&limit=100&path=src`,
    );
    const headers = new Headers(options?.headers);
    expect(headers.get("authorization")).toBe("Bearer test-access-token");
    expect(headers.get("x-mmdash-project-id")).toBe("project-1");
    expect(headers.get("x-request-id")).toBe("repo-browser-request");
  });

  it("forwards provider availability without deployment paths", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json({
        providers: [
          { disabled_reason: null, enabled: true, provider: "managed" },
          { disabled_reason: null, enabled: true, provider: "github" },
          {
            disabled_reason:
              "Current deployment has not enabled server repository access",
            enabled: false,
            provider: "server_existing",
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
      url: "/api/projects/project-1/repository/capabilities",
    });
    expect(response.statusCode).toBe(200);
    expect(response.json()).toMatchObject({
      providers: [
        { enabled: true, provider: "managed" },
        { enabled: true, provider: "github" },
        { enabled: false, provider: "server_existing" },
      ],
    });
    expect(JSON.stringify(response.json())).not.toContain(
      "REPO_LOCAL_ALLOWED_ROOTS",
    );
    expect(fetchImplementation).toHaveBeenCalledWith(
      "http://core.test/v1/projects/project-1/repository/capabilities",
      expect.objectContaining({ method: "GET" }),
    );
  });

  it("exposes management status codes but no commit or checkout writes", async () => {
    const repository = {
      created_at: "2026-07-29T00:00:00Z",
      default_branch: "main",
      display_name: "acme/model",
      last_error_code: null,
      last_error_message: null,
      last_synced_at: null,
      project_id: "project-1",
      provider: "github",
      remote_url: "https://github.com/acme/model",
      repository_id: "00000000-0000-4000-8000-000000000011",
      settings_version: 1,
      status: "pending",
      updated_at: "2026-07-29T00:00:00Z",
      webhook: {
        hook_id: "00000000-0000-4000-8000-000000000012",
        public_url:
          "https://mmdash.example/api/webhooks/github/00000000-0000-4000-8000-000000000012",
        secret_configured: true,
      },
      workspaces: [],
    };
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValue(Response.json(repository));
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const connected = await app.inject({
      headers: { cookie },
      method: "PUT",
      payload: { replace_disconnected: true, settings_version: 1 },
      url: "/api/projects/project-1/repository",
    });
    expect(connected.statusCode).toBe(202);
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe("http://core.test/v1/projects/project-1/repository");
    expect(options?.method).toBe("PUT");
    expect(JSON.parse(String(options?.body))).toEqual({
      replace_disconnected: true,
      settings_version: 1,
    });

    for (const path of [
      "/api/projects/project-1/repository/commits",
      "/api/projects/project-1/repository/checkouts",
    ]) {
      const blocked = await app.inject({
        headers: { cookie },
        method: "POST",
        payload: {},
        url: path,
      });
      expect(blocked.statusCode).toBe(404);
    }
    expect(fetchImplementation).toHaveBeenCalledTimes(1);
  });
});
