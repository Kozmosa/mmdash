import type { Server } from "node:http";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { GatewayFetchHandler } from "../src/gateway.js";
import { createGatewayHttpServer } from "../src/http-server.js";

const servers: Server[] = [];

afterEach(async () => {
  await Promise.all(
    servers.splice(0).map(
      (server) =>
        new Promise<void>((resolve) => {
          server.close(() => resolve());
        }),
    ),
  );
  vi.restoreAllMocks();
});

describe("MCP HTTP server logging", () => {
  it("logs a fixed transport error without the thrown secret", async () => {
    const secret = "dashboard-session-token-must-not-leak";
    const stderr = vi
      .spyOn(process.stderr, "write")
      .mockImplementation(() => true);
    const gateway: GatewayFetchHandler = {
      close: async () => undefined,
      fetch: async () => {
        throw new Error(`transport failed with ${secret}`);
      },
      tools: [],
    };
    const server = createGatewayHttpServer(gateway);
    servers.push(server);
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(0, "127.0.0.1", () => resolve());
    });
    const address = server.address();
    if (typeof address === "string" || address === null) {
      throw new Error("Expected a TCP address");
    }

    await fetch(`http://127.0.0.1:${address.port}/mcp`, {
      body: JSON.stringify({ id: 1, jsonrpc: "2.0", method: "ping" }),
      headers: { "content-type": "application/json" },
      method: "POST",
    }).catch(() => undefined);

    const output = stderr.mock.calls.map(([value]) => String(value)).join("");
    expect(output).toContain("MCP HTTP transport failed");
    expect(output).not.toContain(secret);
  });
});
