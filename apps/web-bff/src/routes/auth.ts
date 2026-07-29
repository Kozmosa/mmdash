import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance, FastifyReply } from "fastify";
import { z } from "zod";

import {
  encodeSessionAssertion,
  sessionCookieName,
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
      setSessionCookie(reply, result, config);
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
      setSessionCookie(reply, result, config);
      return reply.code(201).send({ user: result.user });
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

function setSessionCookie(
  reply: FastifyReply,
  result: Awaited<ReturnType<CoreClient["login"]>>,
  config: BffConfig,
): void {
  const assertion = encodeSessionAssertion({
    access_token: result.access_token,
    created_at: result.user.created_at,
    display_name: result.user.display_name,
    email: result.user.email,
    expires_at: result.expires_at,
    session_id: result.session_id,
    status: result.user.status,
    system_role: result.user.system_role,
    user_id: result.user.id,
  });
  reply.setCookie(sessionCookieName, assertion, {
    httpOnly: true,
    path: "/",
    sameSite: "lax",
    secure: config.nodeEnv === "production",
    signed: true,
  });
}
