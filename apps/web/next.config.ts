import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  experimental: {
    optimizePackageImports: ["lucide-react"],
  },
  output: "standalone",
  async rewrites() {
    const bffBaseUrl = process.env.BFF_INTERNAL_URL ?? "http://localhost:3001";
    return [
      {
        source: "/api/:path*",
        destination: `${bffBaseUrl}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
