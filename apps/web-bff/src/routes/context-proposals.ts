import type { CoreClient } from "@mmdash/core-client";
import type { components } from "@mmdash/core-client/generated";
import type { FastifyInstance, FastifyRequest } from "fastify";
import { z } from "zod";

const proposalParamsSchema = z.object({
  projectId: z.string().uuid(),
  proposalId: z.string().uuid(),
});
const reviewContextProposalSchema = z
  .object({
    decision: z.enum(["accepted", "rejected"]),
    review_note: z.string().max(2_000).optional(),
  })
  .strict();

export function registerContextProposalRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
): void {
  app.get(
    "/api/projects/:projectId/context/proposals",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.listContextProposals(
        request.currentProjectId!,
        coreContext(request),
      ),
  );

  app.post(
    "/api/projects/:projectId/context/proposals/:proposalId/review",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { proposalId } = proposalParamsSchema.parse(request.params);
      const input = reviewContextProposalSchema.parse(
        request.body,
      ) satisfies components["schemas"]["ReviewContextProposalRequest"];
      return coreClient.reviewContextProposal(
        request.currentProjectId!,
        proposalId,
        input,
        coreContext(request),
      );
    },
  );
}

function coreContext(request: FastifyRequest) {
  return {
    accessToken: request.browserIdentity!.accessToken,
    projectId: request.currentProjectId!,
    requestId: request.id,
    userId: request.browserIdentity!.userId,
  };
}
