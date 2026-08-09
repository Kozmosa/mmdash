import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("Model browser routes", () => {
  it("validates and forwards a question binding", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json({
        question_id: "00000000-0000-4000-8000-000000000003",
        project_id: "00000000-0000-4000-8000-000000000001",
        code: "Q1",
        title: "模型一",
      }),
    );
    const app = buildApp({ config: testConfig, fetchImplementation, logger: false });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const response = await app.inject({
      headers: { cookie },
      method: "POST",
      payload: {
        code: "Q1",
        title: "模型一",
        notion_page_id: "00000000-0000-4000-8000-000000000002",
      },
      url: "/api/projects/00000000-0000-4000-8000-000000000001/models/questions",
    });

    expect(response.statusCode).toBe(201);
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe("http://core.test/v1/projects/00000000-0000-4000-8000-000000000001/models/questions");
    expect(JSON.parse(String(options?.body))).toEqual({
      code: "Q1",
      title: "模型一",
      notion_page_id: "00000000-0000-4000-8000-000000000002",
    });
  });

  it("returns 202 for manual whole-model synchronization", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json({ sync_id: "sync", status: "queued" }),
    );
    const app = buildApp({ config: testConfig, fetchImplementation, logger: false });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const response = await app.inject({
      headers: { cookie },
      method: "POST",
      url: "/api/projects/project-1/models/source/sync",
    });

    expect(response.statusCode).toBe(202);
    expect(fetchImplementation.mock.calls[0]?.[0]).toBe(
      "http://core.test/v1/projects/project-1/models/source/sync",
    );
  });

  it("starts a state-bound Notion OAuth authorization", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json({ authorization_url: "https://api.notion.com/v1/oauth/authorize?state=opaque", expires_at: "2026-08-09T10:10:00.000Z" }),
    );
    const app = buildApp({ config: testConfig, fetchImplementation, logger: false });
    apps.push(app);
    const cookie = await signedSessionCookie(app);
    const projectId = "00000000-0000-4000-8000-000000000001";

    const response = await app.inject({
      headers: { cookie }, method: "POST",
      payload: { root_page_url: "https://nyaku.notion.site/3a4df00a545d801cae41e79dc52fbb51", auto_sync_enabled: true, auto_sync_interval_seconds: 300 },
      url: `/api/projects/${projectId}/models/notion/oauth/authorizations`,
    });

    expect(response.statusCode).toBe(201);
    expect(fetchImplementation.mock.calls[0]?.[0]).toBe(`http://core.test/v1/projects/${projectId}/models/notion/oauth/authorizations`);
    expect(JSON.parse(String(fetchImplementation.mock.calls[0]?.[1]?.body))).toMatchObject({ auto_sync_interval_seconds: 300 });
  });

  it("completes the Notion callback and redirects only to the state-bound Project", async () => {
    const projectId = "00000000-0000-4000-8000-000000000001";
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(Response.json({ project_id: projectId, status: "connected" }));
    const app = buildApp({ config: testConfig, fetchImplementation, logger: false });
    apps.push(app);
    const cookie = await signedSessionCookie(app);
    const state = "s".repeat(43);

    const response = await app.inject({ headers: { cookie }, method: "GET", url: `/api/integrations/notion/oauth/callback?state=${state}&code=one-time-code` });

    expect(response.statusCode).toBe(302);
    expect(response.headers.location).toBe(`/projects/${projectId}/settings?notion_oauth=connected#model-settings`);
    expect(fetchImplementation.mock.calls[0]?.[0]).toBe("http://core.test/v1/model-notion/oauth/callback");
    expect(JSON.parse(String(fetchImplementation.mock.calls[0]?.[1]?.body))).toEqual({ state, code: "one-time-code" });
  });
});
