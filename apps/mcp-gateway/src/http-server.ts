import { toNodeHandler } from "@modelcontextprotocol/node";
import { createServer, type Server as HttpServer } from "node:http";

import type { McpGatewayConfig } from "./config.js";
import type { GatewayFetchHandler } from "./gateway.js";

export function createGatewayHttpServer(
  gateway: GatewayFetchHandler,
): HttpServer {
  const handler = toNodeHandler(gateway, {
    onerror(error) {
      process.stderr.write(
        `${JSON.stringify({
          error_name: error.name,
          event: "mcp.http.error",
          message: "MCP HTTP transport failed",
        })}\n`,
      );
    },
  });
  return createServer((request, response) => {
    void handler(request, response);
  });
}

export async function listenGateway(
  server: HttpServer,
  config: McpGatewayConfig,
): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(config.port, config.host, () => {
      server.off("error", reject);
      resolve();
    });
  });
}
