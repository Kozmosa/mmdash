import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance } from "fastify";
import WebSocket, { type RawData } from "ws";

export function registerWebSocketRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
): void {
  app.get(
    "/api/projects/:projectId/socket",
    {
      config: { auth: "required", project: "required" },
      websocket: true,
    },
    (socket, request) => {
      const projectId = request.currentProjectId!;
      const upstream = new WebSocket(
        toWebSocketUrl(
          coreClient.baseUrl,
          `/v1/projects/${encodeURIComponent(projectId)}/socket`,
        ),
        {
          headers: {
            "x-mmdash-project-id": request.currentProjectId!,
            "x-mmdash-user-id": request.browserIdentity!.userId,
            "x-request-id": request.id,
          },
        },
      );
      const pending: RawData[] = [];

      socket.on("message", (message) => {
        if (upstream.readyState === WebSocket.OPEN) {
          upstream.send(message);
        } else if (pending.length < 100) {
          pending.push(message);
        } else {
          socket.close(1013, "Upstream is unavailable");
        }
      });
      upstream.on("open", () => {
        for (const message of pending) {
          upstream.send(message);
        }
        pending.length = 0;
      });
      upstream.on("message", (message) => {
        if (socket.readyState === WebSocket.OPEN) {
          socket.send(message);
        }
      });
      upstream.on("close", (code, reason) => {
        if (socket.readyState === WebSocket.OPEN) {
          socket.close(code, reason.toString());
        }
      });
      upstream.on("error", (error) => {
        request.log.error({ err: error }, "upstream websocket failed");
        if (socket.readyState === WebSocket.OPEN) {
          socket.close(1011, "Upstream connection failed");
        }
      });
      socket.on("close", () => {
        if (
          upstream.readyState === WebSocket.OPEN ||
          upstream.readyState === WebSocket.CONNECTING
        ) {
          upstream.close(1000, "Client disconnected");
        }
      });
    },
  );
}

function toWebSocketUrl(baseUrl: string, path: string): string {
  const url = new URL(path, `${baseUrl}/`);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.toString();
}
