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
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
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
      Response.json(
        {
          experiment_id: experimentId,
          project_id: projectId,
          status: "created",
        },
        { status: 201 },
      ),
    );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);
    const request = {
      name: "parameter sweep",
      experiment_type: "box",
      source_commit: "a".repeat(40),
      entrypoint: "python:run.py",
      parameters: { alpha: 0.5 },
      environment: { MMDASH_MODE: "test" },
      inputs: {},
      runtime_policy: "local-docker",
      limits_override: {
        cpu_millis: 500,
        memory_bytes: 1 << 20,
        timeout_seconds: 60,
        disk_bytes: 1 << 20,
        pids: 32,
        network: "disabled",
      },
      idempotency_key: "parameter-sweep-1",
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

  it("forwards comparison IDs and many-to-many Box assignment through the project boundary", async () => {
    const fetchImplementation = vi.fn<typeof fetch>();
    fetchImplementation
      .mockResolvedValueOnce(Response.json({ items: [] }))
      .mockResolvedValueOnce(
        Response.json({ box_id: "00000000-0000-4000-8000-000000000003" }),
      )
      .mockResolvedValueOnce(new Response(null, { status: 204 }));
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
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
      url: `/api/projects/${projectId}/boxes/00000000-0000-4000-8000-000000000003`,
    });
    expect(bound.statusCode).toBe(200);
    expect(fetchImplementation.mock.calls[1]?.[0]).toBe(
      `http://core.test/v1/projects/${projectId}/boxes/00000000-0000-4000-8000-000000000003`,
    );

    const unbound = await app.inject({
      headers: { cookie },
      method: "DELETE",
      url: `/api/projects/${projectId}/boxes/00000000-0000-4000-8000-000000000003?force=true`,
    });
    expect(unbound.statusCode).toBe(204);
    expect(fetchImplementation.mock.calls[2]?.[0]).toBe(
      `http://core.test/v1/projects/${projectId}/boxes/00000000-0000-4000-8000-000000000003?force=true`,
    );
  });

  it("forwards personal Box management without a project context", async () => {
    const boxId = "00000000-0000-4000-8000-000000000003";
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(Response.json({ items: [] }))
      .mockResolvedValueOnce(
        Response.json({ box_id: boxId, name: "compute-1" }),
      )
      .mockResolvedValueOnce(
        Response.json(
          { box: { box_id: boxId }, active_tasks: 0 },
          { status: 202 },
        ),
      );
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    expect(
      (
        await app.inject({
          headers: { cookie },
          method: "GET",
          url: "/api/users/me/boxes",
        })
      ).statusCode,
    ).toBe(200);
    expect(
      (
        await app.inject({
          headers: { cookie },
          method: "PATCH",
          payload: { name: "compute-1" },
          url: `/api/users/me/boxes/${boxId}`,
        })
      ).statusCode,
    ).toBe(200);
    expect(
      (
        await app.inject({
          headers: { cookie },
          method: "POST",
          payload: { mode: "drain" },
          url: `/api/users/me/boxes/${boxId}/revoke`,
        })
      ).statusCode,
    ).toBe(202);
    expect(fetchImplementation.mock.calls.map((call) => call[0])).toEqual([
      "http://core.test/v1/users/me/boxes",
      `http://core.test/v1/users/me/boxes/${boxId}`,
      `http://core.test/v1/users/me/boxes/${boxId}/revoke`,
    ]);
    for (const [, options] of fetchImplementation.mock.calls) {
      expect(
        new Headers(options?.headers).get("x-mmdash-project-id"),
      ).toBeNull();
    }
  });

  it("rewrites system Box installer transfers to the browser BFF path", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      Response.json({
        items: [
          {
            platform: "windows",
            version: "0.1.0",
            artifact_id: "00000000-0000-4000-8000-000000000010",
            version_id: "00000000-0000-4000-8000-000000000011",
            filename: "mmdash-box-windows-amd64.exe",
            sha256: "a".repeat(64),
            size_bytes: 10,
            download: {
              method: "GET",
              url: "http://core.test/v1/artifact-transfers/token.signature",
              headers: {},
              expires_at: "2026-08-16T00:00:00Z",
            },
            install_command: ".\\mmdash-box-windows-amd64.exe",
            instructions: "Install",
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
      url: "/api/box/releases",
    });

    expect(response.statusCode).toBe(200);
    expect(response.headers["cache-control"]).toBe("no-store");
    expect(response.json().items[0].download.url).toBe(
      "/api/artifact-transfers/token.signature",
    );
    expect(fetchImplementation.mock.calls[0]?.[0]).toBe(
      "http://core.test/v1/box/releases",
    );
  });
});
