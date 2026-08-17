import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance } from "fastify";
import { rewriteTransferGrant } from "./artifacts.js";

export function registerBoxRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
): void {
  app.get(
    "/api/box/releases",
    { config: { auth: "required", project: "none" } },
    async (request, reply) => {
      reply.header("cache-control", "no-store");
      const releases = await coreClient.listBoxReleases({
        accessToken: request.browserIdentity!.accessToken,
        requestId: request.id,
        userId: request.browserIdentity!.userId,
      });
      return {
        items: releases.items.map((release) => ({
          ...release,
          download: rewriteTransferGrant(release.download),
        })),
      };
    },
  );
}
