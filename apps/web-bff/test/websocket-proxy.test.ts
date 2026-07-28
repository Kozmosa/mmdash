import { once } from "node:events";
import { afterEach, describe, expect, it } from "vitest";
import WebSocket, { WebSocketServer } from "ws";

import { buildApp } from "../src/app.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];
const servers: WebSocketServer[] = [];

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
  await Promise.all(
    servers.splice(0).map(
      (server) =>
        new Promise<void>((resolve) => {
          for (const client of server.clients) {
            client.terminate();
          }
          server.close(() => resolve());
        }),
    ),
  );
});

describe("WebSocket proxy", () => {
  it("forwards messages bidirectionally after auth hooks", async () => {
    const upstream = new WebSocketServer({ host: "127.0.0.1", port: 0 });
    servers.push(upstream);
    await once(upstream, "listening");
    const address = upstream.address();
    if (typeof address === "string" || address === null) {
      throw new Error("Expected a TCP WebSocket address");
    }
    upstream.on("connection", (socket) => {
      socket.on("message", (message) => {
        socket.send(`upstream:${message.toString()}`);
      });
    });

    const app = buildApp({
      config: {
        ...testConfig,
        coreBaseUrl: `http://127.0.0.1:${address.port}`,
      },
      logger: false,
    });
    apps.push(app);
    const cookie = await signedSessionCookie(app);
    await app.listen({ host: "127.0.0.1", port: 0 });
    const bffAddress = app.server.address();
    if (typeof bffAddress === "string" || bffAddress === null) {
      throw new Error("Expected a TCP BFF address");
    }
    const socket = new WebSocket(
      `ws://127.0.0.1:${bffAddress.port}/api/projects/project-1/socket`,
      { headers: { cookie } },
    );
    await waitForOpen(socket);
    const message = once(socket, "message");

    socket.send("hello");

    const [payload] = await message;
    expect(payload.toString()).toBe("upstream:hello");
    socket.close();
  });
});

function waitForOpen(socket: WebSocket): Promise<void> {
  return new Promise((resolve, reject) => {
    socket.once("open", resolve);
    socket.once("error", reject);
    socket.once("unexpected-response", (_request, response) => {
      const chunks: Buffer[] = [];
      response.on("data", (chunk: Buffer) => chunks.push(chunk));
      response.on("end", () => {
        reject(
          new Error(
            `Unexpected WebSocket response ${response.statusCode}: ${Buffer.concat(chunks).toString("utf8")}`,
          ),
        );
      });
    });
  });
}
