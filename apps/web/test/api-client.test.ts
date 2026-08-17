import { describe, expect, it, vi } from "vitest";

import { ApiClient, shouldRedirectToLogin } from "@/lib/api-client";

describe("ApiClient", () => {
  it("serializes JSON, query parameters, and browser credentials", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ id: "project-1" }), {
        headers: { "content-type": "application/json" },
      }),
    );
    const client = new ApiClient("/api", fetchImplementation);

    await expect(
      client.request<{ id: string }>("/projects", {
        body: { name: "Test" },
        method: "POST",
        query: { archived: false, cursor: null },
      }),
    ).resolves.toEqual({ id: "project-1" });
    expect(fetchImplementation).toHaveBeenCalledWith(
      "/api/projects?archived=false",
      expect.objectContaining({
        body: JSON.stringify({ name: "Test" }),
        credentials: "include",
        method: "POST",
      }),
    );
  });

  it("maps safe API errors and request ids", async () => {
    const fetchImplementation = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(
        JSON.stringify({
          code: "PROJECT_NOT_FOUND",
          message: "Project not found",
          request_id: "request-1",
        }),
        {
          headers: { "content-type": "application/json" },
          status: 404,
        },
      ),
    );
    const client = new ApiClient("/api", fetchImplementation);

    await expect(client.request("/projects/missing")).rejects.toMatchObject({
      code: "PROJECT_NOT_FOUND",
      message: "Project not found",
      requestId: "request-1",
      status: 404,
    });
  });

  it("does not recursively redirect public authentication pages", () => {
    expect(shouldRedirectToLogin(401, "/auth/me", "/login")).toBe(false);
    expect(shouldRedirectToLogin(401, "/auth/me", "/register")).toBe(false);
    expect(shouldRedirectToLogin(401, "/auth/me", "/invite")).toBe(false);
    expect(shouldRedirectToLogin(401, "/auth/me", "/")).toBe(false);
    expect(shouldRedirectToLogin(401, "/auth/me", "/downloads")).toBe(false);
    expect(shouldRedirectToLogin(401, "/projects", "/projects")).toBe(true);
  });
});
