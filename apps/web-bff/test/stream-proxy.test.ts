import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("stream proxies", () => {
  it("streams SSE with the project and user context", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      new Response("id: 1\ndata: ready\n\n", {
        headers: { "content-type": "text/event-stream" },
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
      url: "/api/projects/project-1/events?cursor=next",
    });

    expect(response.statusCode).toBe(200);
    expect(response.headers["content-type"]).toContain("text/event-stream");
    expect(response.body).toContain("data: ready");
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe(
      "http://core.test/v1/projects/project-1/events?cursor=next",
    );
    const headers = new Headers(options?.headers);
    expect(headers.get("authorization")).toBe("Bearer test-access-token");
    expect(headers.get("x-mmdash-project-id")).toBe("project-1");
    expect(headers.get("x-mmdash-user-id")).toBe("user-1");
  });

  it("streams file downloads and preserves safe headers", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      new Response("file contents", {
        headers: {
          "content-disposition": 'attachment; filename="result.txt"',
          "content-type": "text/plain",
          etag: '"hash"',
        },
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
      headers: { cookie, range: "bytes=0-10" },
      method: "GET",
      url: "/api/projects/project-1/files/results/output.txt",
    });

    expect(response.statusCode).toBe(200);
    expect(response.body).toBe("file contents");
    expect(response.headers.etag).toBe('"hash"');
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe(
      "http://core.test/v1/projects/project-1/files/results/output.txt",
    );
    expect(new Headers(options?.headers).get("range")).toBe("bytes=0-10");
  });
});
