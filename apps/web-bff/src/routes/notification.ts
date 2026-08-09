import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance } from "fastify";
import { z } from "zod";

const project = z.object({ projectId: z.string().uuid() });
const inbox = z.object({ inboxItemId: z.string().uuid() });
const delivery = project.extend({ deliveryId: z.string().uuid() });
const channel = project.extend({ channelKey: z.string().min(1).max(200) });
const rule = project.extend({ typeKey: z.string().min(1).max(200) });
const query = z.object({
  project_id: z.string().uuid().optional(),
  type_key: z.string().optional(),
  read_state: z.enum(["read", "unread"]).optional(),
  archived: z.enum(["true", "false"]).optional(),
  outcome: z.enum(["active", "resolved", "revoked", "expired"]).optional(),
  outcome_group: z.enum(["processed"]).optional(),
  occurred_from: z.string().datetime({ offset: true }).optional(),
  occurred_to: z.string().datetime({ offset: true }).optional(),
  cursor: z.string().optional(),
  limit: z.coerce.number().int().min(1).max(200).optional(),
});

export function registerNotificationRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
): void {
  app.get(
    "/api/inbox",
    { config: { auth: "required", project: "none" } },
    async (request) =>
      coreClient.listInbox(query.parse(request.query), context(request)),
  );
  app.post(
    "/api/projects/invitations/:invitationId/accept",
    { config: { auth: "required", project: "none" } },
    async (request) => {
      const params = z
        .object({ invitationId: z.string().uuid() })
        .parse(request.params);
      return coreClient.acceptInvitationById(
        params.invitationId,
        context(request),
      );
    },
  );
  app.get(
    "/api/inbox/unread-count",
    { config: { auth: "required", project: "none" } },
    async (request) =>
      coreClient.unreadInboxCount(
        z
          .object({ project_id: z.string().uuid().optional() })
          .parse(request.query).project_id,
        context(request),
      ),
  );
  app.post(
    "/api/inbox/mark-all-read",
    { config: { auth: "required", project: "none" } },
    async (request, reply) => {
      await coreClient.markAllInboxRead(
        z
          .object({
            project_id: z.string().uuid().optional(),
            type_key: z.string().optional(),
          })
          .parse(request.body ?? {}),
        context(request),
      );
      return reply.code(204).send();
    },
  );
  app.get(
    "/api/inbox/:inboxItemId",
    { config: { auth: "required", project: "none" } },
    async (request) => {
      const params = inbox.parse(request.params);
      return coreClient.getInbox(params.inboxItemId, context(request));
    },
  );
  app.patch(
    "/api/inbox/:inboxItemId",
    { config: { auth: "required", project: "none" } },
    async (request) => {
      const params = inbox.parse(request.params);
      return coreClient.updateInbox(
        params.inboxItemId,
        z
          .object({
            read_state: z.enum(["read", "unread"]).optional(),
            archived: z.boolean().optional(),
          })
          .refine((v) => v.read_state !== undefined || v.archived !== undefined)
          .parse(request.body),
        context(request),
      );
    },
  );

  app.get(
    "/api/projects/:projectId/notification-channels/:channelKey",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const params = channel.parse(request.params);
      return coreClient.getNotificationChannel(
        params.projectId,
        params.channelKey,
        context(request, params.projectId),
      );
    },
  );
  app.patch(
    "/api/projects/:projectId/notification-channels/:channelKey",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const params = channel.parse(request.params);
      return coreClient.updateNotificationChannel(
        params.projectId,
        params.channelKey,
        z
          .object({ values: z.record(z.string(), z.unknown()) })
          .parse(request.body),
        context(request, params.projectId),
      );
    },
  );
  app.delete(
    "/api/projects/:projectId/notification-channels/:channelKey",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const params = channel.parse(request.params);
      await coreClient.deleteNotificationChannel(
        params.projectId,
        params.channelKey,
        context(request, params.projectId),
      );
      return reply.code(204).send();
    },
  );
  app.post(
    "/api/projects/:projectId/notification-channels/:channelKey/test",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const params = channel.parse(request.params);
      return coreClient.testNotificationChannel(
        params.projectId,
        params.channelKey,
        context(request, params.projectId),
      );
    },
  );
  app.get(
    "/api/projects/:projectId/notification-rules/:typeKey",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const params = rule.parse(request.params);
      return coreClient.getNotificationRule(
        params.projectId,
        params.typeKey,
        context(request, params.projectId),
      );
    },
  );
  app.put(
    "/api/projects/:projectId/notification-rules/:typeKey",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const params = rule.parse(request.params);
      return coreClient.updateNotificationRule(
        params.projectId,
        params.typeKey,
        z
          .object({
            external_enabled: z.boolean(),
            channel_keys: z
              .array(
                z.enum([
                  "notification.feishu_webhook",
                  "notification.generic_webhook",
                ]),
              )
              .optional(),
            minimum_priority: z
              .enum(["low", "normal", "high", "urgent"])
              .optional(),
            version: z.number().int().min(0),
          })
          .strict()
          .parse(request.body),
        context(request, params.projectId),
      );
    },
  );
  app.get(
    "/api/projects/:projectId/notification-deliveries",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const params = project.parse(request.params);
      const input = z
        .object({
          channel_key: z.string().optional(),
          cursor: z.string().optional(),
          limit: z.coerce.number().int().min(1).max(200).optional(),
        })
        .parse(request.query);
      return coreClient.listNotificationDeliveries(
        params.projectId,
        input,
        context(request, params.projectId),
      );
    },
  );
  app.post(
    "/api/projects/:projectId/notification-deliveries/:deliveryId/retry",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const params = delivery.parse(request.params);
      return coreClient.retryNotificationDelivery(
        params.projectId,
        params.deliveryId,
        z
          .object({ reason: z.string().trim().min(1).max(1000) })
          .parse(request.body),
        context(request, params.projectId),
      );
    },
  );
}

function context(
  request: {
    browserIdentity?: { accessToken: string; userId: string };
    id: string;
  },
  projectId?: string,
) {
  return {
    accessToken: request.browserIdentity!.accessToken,
    projectId,
    requestId: request.id,
    userId: request.browserIdentity!.userId,
  };
}
