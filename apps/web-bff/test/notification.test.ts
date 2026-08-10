import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("notification routes", () => {
  it("forwards Inbox grouping and time filters to Core", async () => {
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValue(
        Response.json({ has_more: false, items: [], next_cursor: "" }),
      );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);

    const response = await app.inject({
      headers: { cookie: await signedSessionCookie(app) },
      method: "GET",
      url: "/api/inbox?outcome_group=processed&occurred_from=2026-08-01T00%3A00%3A00%2B08%3A00&occurred_to=2026-08-09T23%3A59%3A59%2B08%3A00&limit=25",
    });

    expect(response.statusCode).toBe(200);
    const [url] = fetchImplementation.mock.calls[0]!;
    const forwarded = new URL(String(url));
    expect(forwarded.pathname).toBe("/v1/inbox");
    expect(Object.fromEntries(forwarded.searchParams)).toMatchObject({
      limit: "25",
      occurred_from: "2026-08-01T00:00:00+08:00",
      occurred_to: "2026-08-09T23:59:59+08:00",
      outcome_group: "processed",
    });
  });

  it("forwards the mark-all-read scope in the request body", async () => {
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValue(new Response(null, { status: 204 }));
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const projectId = "00000000-0000-4000-8000-000000000001";

    const response = await app.inject({
      headers: { cookie: await signedSessionCookie(app) },
      method: "POST",
      payload: {
        project_id: projectId,
        type_key: "progress.reminder.due",
      },
      url: "/api/inbox/mark-all-read",
    });

    expect(response.statusCode).toBe(204);
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe("http://core.test/v1/inbox/mark-all-read");
    expect(JSON.parse(String(options?.body))).toEqual({
      project_id: projectId,
      type_key: "progress.reminder.due",
    });
  });

  it("rejects the removed project-level Inbox switch", async () => {
    const fetchImplementation = vi.fn<typeof fetch>();
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const projectId = "00000000-0000-4000-8000-000000000001";

    const response = await app.inject({
      headers: { cookie: await signedSessionCookie(app) },
      method: "PUT",
      payload: {
        external_enabled: false,
        inbox_enabled: false,
        version: 0,
      },
      url: `/api/projects/${projectId}/notification-rules/progress.reminder.due`,
    });

    expect(response.statusCode).toBe(400);
    expect(fetchImplementation).not.toHaveBeenCalled();
  });
});
