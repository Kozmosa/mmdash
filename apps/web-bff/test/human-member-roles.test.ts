import { afterEach, describe, expect, it, vi } from "vitest";

import { buildApp } from "../src/app.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("human project member roles", () => {
  it.each([
    {
      method: "PUT",
      payload: { role: "agent" },
      suffix: "/members/00000000-0000-4000-8000-000000000002",
    },
    {
      method: "POST",
      payload: { email: "human@example.com", role: "box" },
      suffix: "/invitations",
    },
  ])("rejects machine roles on $method $suffix", async (testCase) => {
    const fetchImplementation = vi.fn<typeof fetch>();
    const app = buildApp({
      config: testConfig,
      fetchImplementation,
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);
    const projectId = "00000000-0000-4000-8000-000000000001";

    const response = await app.inject({
      headers: { cookie },
      method: testCase.method,
      payload: testCase.payload,
      url: `/api/projects/${projectId}${testCase.suffix}`,
    });

    expect(response.statusCode).toBe(400);
    expect(fetchImplementation).not.toHaveBeenCalled();
  });
});
