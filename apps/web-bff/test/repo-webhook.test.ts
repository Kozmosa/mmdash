import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("Repo webhook proxy", () => {
  it("forwards the exact unauthenticated body and signature headers", async () => {
    const body = '{\n  "ref": "refs/heads/main",\n  "after": "abc"\n}\n';
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(
        JSON.stringify({
          accepted: true,
          duplicate: false,
          event: "push",
        }),
        {
          headers: { "content-type": "application/json" },
          status: 202,
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
      headers: {
        "content-type": "application/json",
        "x-github-delivery": "delivery-1",
        "x-github-event": "push",
        "x-hub-signature-256": `sha256=${"a".repeat(64)}`,
        "x-request-id": "repo-webhook-request",
      },
      method: "POST",
      payload: body,
      url: "/api/webhooks/github/00000000-0000-4000-8000-000000000012",
    });

    expect(response.statusCode).toBe(202);
    expect(response.json()).toMatchObject({
      accepted: true,
      duplicate: false,
    });
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe(
      "http://core.test/v1/repo/webhooks/github/00000000-0000-4000-8000-000000000012",
    );
    expect(Buffer.from(options?.body as Uint8Array).toString("utf8")).toBe(
      body,
    );
    const forwarded = new Headers(options?.headers);
    expect(forwarded.get("x-github-delivery")).toBe("delivery-1");
    expect(forwarded.get("x-github-event")).toBe("push");
    expect(forwarded.get("authorization")).toBeNull();
  });

  it("rejects an oversized payload before calling Core", async () => {
    const fetchImplementation = vi.fn<typeof fetch>();
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);

    const response = await app.inject({
      headers: {
        "content-type": "application/json",
        "x-github-delivery": "delivery-1",
        "x-github-event": "push",
        "x-hub-signature-256": `sha256=${"a".repeat(64)}`,
      },
      method: "POST",
      payload: Buffer.alloc(1024 * 1024 + 1, "x"),
      url: "/api/webhooks/github/00000000-0000-4000-8000-000000000012",
    });

    expect(response.statusCode).toBe(413);
    expect(fetchImplementation).not.toHaveBeenCalled();
  });
});
