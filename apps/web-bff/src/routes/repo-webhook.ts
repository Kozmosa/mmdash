import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance } from "fastify";
import { z } from "zod";

const parametersSchema = z.object({
  hookId: z.string().uuid(),
});

const headersSchema = z.object({
  "x-github-delivery": z.string().min(1).max(200),
  "x-github-event": z.string().min(1).max(200),
  "x-hub-signature-256": z.string().min(1).max(200),
});

const maximumBodyBytes = 1024 * 1024;

export function registerRepoWebhookRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
): void {
  app.register(async function repoWebhookScope(scopedApp) {
    scopedApp.addContentTypeParser(
      "application/json",
      { bodyLimit: maximumBodyBytes, parseAs: "buffer" },
      (_request, body, done) => done(null, body),
    );
    scopedApp.post(
      "/api/webhooks/github/:hookId",
      {
        config: { auth: "public", project: "none" },
        bodyLimit: maximumBodyBytes,
      },
      async (request, reply) => {
        const { hookId } = parametersSchema.parse(request.params);
        const headers = headersSchema.parse(request.headers);
        const body = request.body;
        if (!Buffer.isBuffer(body) || body.length === 0) {
          return reply.code(400).send({
            code: "INVALID_REQUEST",
            message: "Webhook payload is invalid",
            request_id: request.id,
          });
        }
        const response = await coreClient.fetch(
          `/v1/repo/webhooks/github/${encodeURIComponent(hookId)}`,
          {
            body,
            headers: {
              "content-type": "application/json",
              "x-github-delivery": headers["x-github-delivery"],
              "x-github-event": headers["x-github-event"],
              "x-hub-signature-256": headers["x-hub-signature-256"],
            },
            method: "POST",
          },
          { requestId: request.id },
        );
        reply.code(response.status);
        const contentType = response.headers.get("content-type");
        if (contentType) {
          reply.header("content-type", contentType);
        }
        return reply.send(Buffer.from(await response.arrayBuffer()));
      },
    );
  });
}
