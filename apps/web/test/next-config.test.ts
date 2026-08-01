import { afterEach, describe, expect, it, vi } from "vitest";

import nextConfig from "../next.config";

describe("local unified entrypoint rewrites", () => {
  afterEach(() => vi.unstubAllEnvs());

  it("routes API, Core, and MCP paths to their owning services", async () => {
    vi.stubEnv("MMDASH_LOCAL_UNIFIED_PROXY", "true");
    vi.stubEnv("BFF_INTERNAL_URL", "http://bff.test:3101");
    vi.stubEnv("CORE_INTERNAL_URL", "http://core.test:8180");
    vi.stubEnv("MCP_INTERNAL_URL", "http://mcp.test:3102");

    const rewrites = await nextConfig.rewrites?.();

    expect(rewrites).toEqual([
      {
        source: "/api/:path*",
        destination: "http://bff.test:3101/api/:path*",
      },
      {
        source: "/v1/:path*",
        destination: "http://core.test:8180/v1/:path*",
      },
      {
        source: "/mcp",
        destination: "http://mcp.test:3102/mcp",
      },
      {
        source: "/mcp/:path*",
        destination: "http://mcp.test:3102/mcp/:path*",
      },
    ]);
  });

  it("keeps production routing behind Caddy", async () => {
    vi.stubEnv("MMDASH_LOCAL_UNIFIED_PROXY", "false");
    vi.stubEnv("BFF_INTERNAL_URL", "http://web-bff:3001");

    await expect(nextConfig.rewrites?.()).resolves.toEqual([
      {
        source: "/api/:path*",
        destination: "http://web-bff:3001/api/:path*",
      },
    ]);
  });
});
