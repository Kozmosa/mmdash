import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("settings routes", () => {
  it("forwards project scope and credentials when listing config types", async () => {
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValue(Response.json({ items: [] }));
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
      url: "/api/projects/project-1/settings/types",
    });

    expect(response.statusCode).toBe(200);
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe(
      "http://core.test/v1/settings/types?scope=project&project_id=project-1",
    );
    const headers = new Headers(options?.headers);
    expect(headers.get("authorization")).toBe("Bearer test-access-token");
    expect(headers.get("x-mmdash-project-id")).toBe("project-1");
  });

  it("forwards project rendering setting reads to Core", async () => {
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValue(
        Response.json({ type_key: "article.rendering", values: {} }),
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
      url: "/api/projects/project-1/settings/article.rendering",
    });

    expect(response.statusCode).toBe(200);
    expect(fetchImplementation.mock.calls[0]?.[0]).toBe(
      "http://core.test/v1/settings/projects/project-1/article.rendering",
    );
  });

  it("preserves a redacted secret placeholder when updating settings", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json({
        scope: "project",
        scope_id: "project-1",
        type_key: "fixture.provider",
        updated_at: "2026-07-28T00:00:00Z",
        updated_by: "user-1",
        values: { token: "********" },
        version: 2,
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
      payload: { values: { token: "********" } },
      url: "/api/projects/project-1/settings/fixture.provider",
    });

    expect(response.statusCode).toBe(200);
    const [, options] = fetchImplementation.mock.calls[0]!;
    expect(JSON.parse(String(options?.body))).toEqual({
      values: { token: "********" },
    });
  });

  it("replaces a redacted secret with a new secret", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json({
        scope: "project",
        scope_id: "project-1",
        type_key: "fixture.provider",
        updated_at: "2026-07-28T00:00:00Z",
        updated_by: "user-1",
        values: { token: "********" },
        version: 2,
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
      payload: { values: { token: "new-secret" } },
      url: "/api/projects/project-1/settings/fixture.provider",
    });

    expect(response.statusCode).toBe(200);
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe(
      "http://core.test/v1/settings/projects/project-1/fixture.provider",
    );
    expect(JSON.parse(String(options?.body))).toEqual({
      values: { token: "new-secret" },
    });
  });
});
