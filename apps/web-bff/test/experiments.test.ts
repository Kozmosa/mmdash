import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];
const projectId = "00000000-0000-4000-8000-000000000001";
const experimentId = "00000000-0000-4000-8000-000000000002";

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("Experiment and Box BFF routes", () => {
  it("requires a signed browser session before reaching Core", async () => {
    const fetchImplementation = vi.fn<typeof fetch>();
    const app = buildApp({ config: testConfig, fetchImplementation, logger: false });
    apps.push(app);

    const response = await app.inject({
      method: "GET",
      url: `/api/projects/${projectId}/experiments`,
    });

    expect(response.statusCode).toBe(401);
    expect(fetchImplementation).not.toHaveBeenCalled();
  });

  it("validates and forwards a frozen experiment create request", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json({ experiment_id: experimentId, project_id: projectId, status: "created" }, { status: 201 }),
    );
    const app = buildApp({ config: testConfig, fetchImplementation, logger: false });
    apps.push(app);
    const cookie = await signedSessionCookie(app);
    const request = {
      name: "parameter sweep",
      source_commit: "a".repeat(40),
      entrypoint: "python:run.py",
      parameters: { alpha: 0.5 },
      environment: { MMDASH_MODE: "test" },
      inputs: {},
      runtime: "local-docker",
      limits: {
        cpu_millis: 500,
        memory_bytes: 1 << 20,
        timeout_seconds: 60,
        disk_bytes: 1 << 20,
        pids: 32,
        network: "disabled",
      },
      idempotency_key: "parameter-sweep-1",
      max_attempts: 1,
    };

    const response = await app.inject({
      headers: { cookie, "x-request-id": "experiment-create-request" },
      method: "POST",
      payload: request,
      url: `/api/projects/${projectId}/experiments`,
    });

    expect(response.statusCode).toBe(201);
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe(`http://core.test/v1/projects/${projectId}/experiments`);
    expect(options?.method).toBe("POST");
    expect(JSON.parse(String(options?.body))).toEqual(request);
    const headers = new Headers(options?.headers);
    expect(headers.get("authorization")).toBe("Bearer test-access-token");
    expect(headers.get("x-mmdash-project-id")).toBe(projectId);
    expect(headers.get("x-request-id")).toBe("experiment-create-request");
  });

  it("forwards comparison IDs and Box binding through the project boundary", async () => {
    const fetchImplementation = vi.fn<typeof fetch>();
    fetchImplementation
      .mockResolvedValueOnce(Response.json({ items: [] }))
      .mockResolvedValueOnce(Response.json({ box_id: "00000000-0000-4000-8000-000000000003" }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));
    const app = buildApp({ config: testConfig, fetchImplementation, logger: false });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const comparison = await app.inject({
      headers: { cookie },
      method: "GET",
      url: `/api/projects/${projectId}/experiments/compare?experiment_id=${experimentId},00000000-0000-4000-8000-000000000004`,
    });
    expect(comparison.statusCode).toBe(200);
    expect(fetchImplementation.mock.calls[0]?.[0]).toBe(
      `http://core.test/v1/projects/${projectId}/experiments/compare?experiment_id=${experimentId}%2C00000000-0000-4000-8000-000000000004`,
    );

    const bound = await app.inject({
      headers: { cookie },
      method: "PUT",
      payload: { box_id: "00000000-0000-4000-8000-000000000003" },
      url: `/api/projects/${projectId}/box`,
    });
    expect(bound.statusCode).toBe(200);
    expect(fetchImplementation.mock.calls[1]?.[0]).toBe(
      `http://core.test/v1/projects/${projectId}/box`,
    );

    const unbound = await app.inject({
      headers: { cookie },
      method: "DELETE",
      url: `/api/projects/${projectId}/box`,
    });
    expect(unbound.statusCode).toBe(204);
    expect(fetchImplementation.mock.calls[2]?.[0]).toBe(
      `http://core.test/v1/projects/${projectId}/box`,
    );
  });
});
