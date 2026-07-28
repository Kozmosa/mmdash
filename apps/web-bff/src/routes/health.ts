import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance } from "fastify";

export function registerHealthRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
  version: string,
): void {
  app.get(
    "/health/live",
    { config: { auth: "public", project: "none" } },
    async () => ({
      service: "web-bff",
      status: "ok",
      version,
    }),
  );

  app.get(
    "/health/ready",
    { config: { auth: "public", project: "none" } },
    async (request, reply) => {
      try {
        const response = await coreClient.fetch(
          "/health/ready",
          { method: "GET" },
          { requestId: request.id },
        );
        if (!response.ok) {
          return reply.code(503).send({
            dependencies: { core: "unavailable" },
            status: "not_ready",
          });
        }
      } catch {
        return reply.code(503).send({
          dependencies: { core: "unavailable" },
          status: "not_ready",
        });
      }
      return {
        dependencies: { core: "ready" },
        status: "ready",
      };
    },
  );
}
