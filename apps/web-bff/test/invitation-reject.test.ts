import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("invitation rejection", () => {
  it("forwards a public rejection without requiring a browser session", async () => {
    const fetchImplementation = vi
      .fn<typeof fetch>()
      .mockResolvedValue(new Response(null, { status: 204 }));
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);

    const response = await app.inject({
      method: "POST",
      payload: { token: "invitation-token" },
      url: "/api/auth/invitations/reject",
    });

    expect(response.statusCode).toBe(204);
    const [url, options] = fetchImplementation.mock.calls[0]!;
    expect(url).toBe("http://core.test/v1/auth/invitations/reject");
    expect(options?.method).toBe("POST");
    expect(JSON.parse(String(options?.body))).toEqual({
      token: "invitation-token",
    });
  });
});
