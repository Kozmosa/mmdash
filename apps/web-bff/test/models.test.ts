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
});
