import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance } from "fastify";
import { z } from "zod";

import type { PageAggregatorRegistry } from "../aggregation/page-aggregator.js";

const paramsSchema = z.object({
  pageId: z.string().regex(/^[a-z][a-z0-9-]{1,63}$/),
  projectId: z.string(),
});

export function registerPageRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
  registry: PageAggregatorRegistry,
): void {
  app.get(
    "/api/projects/:projectId/pages/:pageId",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const params = paramsSchema.parse(request.params);
      return registry.aggregate(params.pageId, {
        coreClient,
        identity: request.browserIdentity!,
        projectId: request.currentProjectId!,
        requestId: request.id,
      });
    },
  );
}
