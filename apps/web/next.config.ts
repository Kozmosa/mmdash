import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  experimental: {
    optimizePackageImports: ["lucide-react"],
  },
  output: "standalone",
  async rewrites() {
    const bffBaseUrl = process.env.BFF_INTERNAL_URL ?? "http://localhost:3001";
    const routes = [
      {
        source: "/api/:path*",
        destination: `${bffBaseUrl}/api/:path*`,
      },
    ];
    if (process.env.MMDASH_LOCAL_UNIFIED_PROXY !== "true") {
      return routes;
    }
    const coreBaseUrl =
      process.env.CORE_INTERNAL_URL ?? "http://localhost:8080";
    const mcpBaseUrl =
      process.env.MCP_INTERNAL_URL ?? "http://localhost:3002";
    return [
      ...routes,
      {
        source: "/v1/:path*",
        destination: `${coreBaseUrl}/v1/:path*`,
      },
      {
        source: "/mcp",
        destination: `${mcpBaseUrl}/mcp`,
      },
      {
        source: "/mcp/:path*",
        destination: `${mcpBaseUrl}/mcp/:path*`,
      },
    ];
  },
};

export default nextConfig;
