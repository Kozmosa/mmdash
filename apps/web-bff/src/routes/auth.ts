import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance } from "fastify";
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

export function registerAuthRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
  config: BffConfig,
): void {
  app.post(
    "/api/auth/login",
    { config: { auth: "public", project: "none" } },
    async (request, reply) => {
      const credentials = loginSchema.parse(request.body);
      const result = await coreClient.login(credentials, {
        requestId: request.id,
      });
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
      return { user: result.user };
    },
  );

  app.get(
    "/api/auth/me",
    { config: { auth: "required", project: "none" } },
    async (request) => ({
      user: {
        created_at: request.browserIdentity!.createdAt,
        display_name: request.browserIdentity!.displayName,
        email: request.browserIdentity!.email,
        id: request.browserIdentity!.userId,
        status: request.browserIdentity!.status,
        system_role: request.browserIdentity!.systemRole,
      },
    }),
  );

  app.post(
    "/api/auth/logout",
    { config: { auth: "required", project: "none" } },
    async (request, reply) => {
      await coreClient.logout({
        accessToken: request.browserIdentity!.accessToken,
        requestId: request.id,
      });
      reply.clearCookie(sessionCookieName, { path: "/" });
      return reply.code(204).send();
    },
  );
}
