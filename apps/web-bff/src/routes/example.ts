import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance } from "fastify";

export function registerExampleRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
): void {
  app.get(
    "/api/example",
    { config: { auth: "public", project: "none" } },
    async (request) =>
      coreClient.checkExample({
        requestId: request.id,
      }),
  );
}
