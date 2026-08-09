import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];
const projectId = "00000000-0000-4000-8000-000000000001";
const evaluationId = "00000000-0000-4000-8000-000000000002";

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("Progress automatic tracking routes", () => {
  it("accepts disabled tracking settings without an Agent binding", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(Response.json({ project_id: projectId }));
    const app = buildApp({ config: testConfig, fetchImplementation, logger: false });
    apps.push(app);
    const cookie = await signedSessionCookie(app);
    const payload = {
      auto_task_changes: true,
      auto_tracking_enabled: false,
      cron_enabled: false,
      cron_schedule: "0 */6 * * *",
      debounce_seconds: 60,
      event_triggers_enabled: true,
      min_interval_seconds: 300,
    };

    const response = await app.inject({ headers: { cookie }, method: "PATCH", payload, url: `/api/projects/${projectId}/progress/settings` });

    expect(response.statusCode).toBe(200);
    expect(fetchImplementation.mock.calls[0]?.[0]).toBe(`http://core.test/v1/projects/${projectId}/progress/settings`);
    expect(JSON.parse(String(fetchImplementation.mock.calls[0]?.[1]?.body))).toEqual(payload);
  });

  it("forwards recalculate, history and retry with the expected status codes", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockImplementation(async () => Response.json({ has_more: false, items: [], request_id: evaluationId, status: "pending" }));
    const app = buildApp({ config: testConfig, fetchImplementation, logger: false });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const recalculate = await app.inject({ headers: { cookie }, method: "POST", payload: { force: false, trigger_kind: "manual" }, url: `/api/projects/${projectId}/progress/recalculate` });
    const history = await app.inject({ headers: { cookie }, method: "GET", url: `/api/projects/${projectId}/progress/evaluations?cursor=next&limit=10` });
    const retry = await app.inject({ headers: { cookie }, method: "POST", url: `/api/projects/${projectId}/progress/evaluations/${evaluationId}/retry` });

    expect(recalculate.statusCode).toBe(202);
    expect(history.statusCode).toBe(200);
    expect(retry.statusCode).toBe(202);
    expect(fetchImplementation.mock.calls.map(([url]) => url)).toEqual([
      `http://core.test/v1/projects/${projectId}/progress/recalculate`,
      `http://core.test/v1/projects/${projectId}/progress/evaluations?cursor=next&limit=10`,
      `http://core.test/v1/projects/${projectId}/progress/evaluations/${evaluationId}/retry`,
    ]);
  });

  it("validates and forwards stage override set and clear", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockImplementation(async () => Response.json({ active: true, override_id: evaluationId, project_id: projectId, stage: "review" }));
    const app = buildApp({ config: testConfig, fetchImplementation, logger: false });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const set = await app.inject({ headers: { cookie }, method: "POST", payload: { note: "Human review", stage: "review", summary: "Ready for review" }, url: `/api/projects/${projectId}/progress/stage-override` });
    const clear = await app.inject({ headers: { cookie }, method: "DELETE", url: `/api/projects/${projectId}/progress/stage-override` });

    expect(set.statusCode).toBe(200);
    expect(clear.statusCode).toBe(200);
    expect(JSON.parse(String(fetchImplementation.mock.calls[0]?.[1]?.body))).toEqual({ note: "Human review", stage: "review", summary: "Ready for review" });
    expect(fetchImplementation.mock.calls[1]?.[1]?.method).toBe("DELETE");
  });
});
