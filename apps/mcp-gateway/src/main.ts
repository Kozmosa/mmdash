import { loadConfig } from "./config.js";
import { buildGateway } from "./gateway.js";
import { createGatewayHttpServer, listenGateway } from "./http-server.js";

const config = loadConfig();
const gateway = buildGateway({ config });
const server = createGatewayHttpServer(gateway);

await listenGateway(server, config);
process.stderr.write(
  `${JSON.stringify({
    event: "service.started",
    host: config.host,
    port: config.port,
    service: "mcp-gateway",
  })}\n`,
);

async function shutdown(): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    server.close((error) => (error ? reject(error) : resolve()));
  });
  await gateway.close();
}

process.once("SIGINT", () => void shutdown());
process.once("SIGTERM", () => void shutdown());
