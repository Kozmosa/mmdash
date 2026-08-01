import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance } from "fastify";
import { z } from "zod";

import {
  sessionCookieName,
  setBrowserSessionCookie,
} from "../auth/browser-auth.js";
import type { BffConfig } from "../config.js";

const loginSchema = z.object({
  email: z.string().email(),
  password: z.string().min(1),
});
const registerSchema = loginSchema.extend({
  display_name: z.string().trim().min(1).max(120),
  invitation_token: z.string().optional(),
  password: z.string().min(8),
});

export function registerAuthRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
  config: BffConfig,
): void {
  app.post(
    "/api/auth/login",
    { config: { auth: "public", project: "none" } },
    async (request, reply) => {
      const result = await coreClient.login(loginSchema.parse(request.body), {
        requestId: request.id,
      });
      setBrowserSessionCookie(reply, result, config);
      return { user: result.user };
    },
  );

  app.post(
    "/api/auth/register",
    { config: { auth: "public", project: "none" } },
    async (request, reply) => {
      const result = await coreClient.register(
        registerSchema.parse(request.body),
        { requestId: request.id },
      );
      setBrowserSessionCookie(reply, result, config);
      return reply.code(201).send({ user: result.user });
    },
  );

  app.post(
    "/api/auth/device/verify",
    { config: { auth: "required", project: "none" } },
    async (request, reply) => {
      const input = z
        .object({
          approve: z.boolean(),
          user_code: z
            .string()
            .trim()
            .regex(/^[A-Za-z0-9]{4}-[A-Za-z0-9]{4}$/),
        })
        .parse(request.body);
      await coreClient.verifyDeviceAuthorization(input, authContext(request));
      return reply.code(204).send();
    },
  );

  app.post(
    "/api/auth/invitations/preview",
    { config: { auth: "public", project: "none" } },
    async (request) =>
      coreClient.previewInvitation(
        z.object({ token: z.string().min(1) }).parse(request.body).token,
        { requestId: request.id },
      ),
  );

  app.post(
    "/api/auth/invitations/accept",
    { config: { auth: "required", project: "none" } },
    async (request) =>
      coreClient.acceptInvitation(
        z.object({ token: z.string().min(1) }).parse(request.body).token,
        authContext(request),
      ),
  );

  app.post(
    "/api/auth/invitations/reject",
    { config: { auth: "public", project: "none" } },
    async (request, reply) => {
      await coreClient.rejectInvitation(
        z.object({ token: z.string().min(1) }).parse(request.body).token,
        { requestId: request.id },
      );
      return reply.code(204).send();
    },
  );

  app.get(
    "/api/auth/me",
    { config: { auth: "required", project: "none" } },
    async (request) => ({
      user: (await coreClient.currentIdentity(authContext(request))).user,
    }),
  );

  app.patch(
    "/api/auth/me",
    { config: { auth: "required", project: "none" } },
    async (request) => ({
      user: await coreClient.updateProfile(
        z
          .object({
            display_name: z.string().trim().min(1).max(120).optional(),
            email: z.string().email().optional(),
            current_password: z.string().optional(),
          })
          .parse(request.body),
        authContext(request),
      ),
    }),
  );

  app.post(
    "/api/auth/me/password",
    { config: { auth: "required", project: "none" } },
    async (request, reply) => {
      await coreClient.changePassword(
        z
          .object({
            current_password: z.string().min(1),
            new_password: z.string().min(8),
          })
          .parse(request.body),
        authContext(request),
      );
      return reply.code(204).send();
    },
  );

  app.post(
    "/api/auth/logout",
    { config: { auth: "required", project: "none" } },
    async (request, reply) => {
      await coreClient.logout(authContext(request));
      reply.clearCookie(sessionCookieName, { path: "/" });
      return reply.code(204).send();
    },
  );
}

function authContext(request: {
  browserIdentity?: { accessToken: string; userId: string };
  id: string;
}) {
  return {
    accessToken: request.browserIdentity!.accessToken,
    requestId: request.id,
    userId: request.browserIdentity!.userId,
  };
}
