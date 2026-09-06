import cookie from "@fastify/cookie";
import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance, FastifyReply } from "fastify";
import { z } from "zod";

import type { BffConfig } from "../config.js";
import { BffError } from "../errors/bff-error.js";

export const sessionCookieName = "mmdash_session";

const sessionAssertionSchema = z.object({
  access_token: z.string().min(1),
  created_at: z.string().datetime({ offset: true }),
  display_name: z.string().min(1).max(200),
  email: z.string().email(),
  expires_at: z.string().datetime({ offset: true }),
  refresh_token: z.string().min(32),
  session_expires_at: z.string().datetime({ offset: true }).optional(),
  session_id: z.string().min(1).max(200),
  status: z.enum(["active", "disabled"]),
  system_role: z.enum(["admin", "member"]),
  user_id: z.string().min(1).max(200),
});

export type BrowserIdentity = {
  accessToken: string;
  createdAt: string;
  displayName: string;
  email: string;
  expiresAt: string;
  refreshToken: string;
  sessionExpiresAt: string;
  sessionId: string;
  status: "active" | "disabled";
  systemRole: "admin" | "member";
  userId: string;
};

export type SessionAssertion = z.input<typeof sessionAssertionSchema>;

export function registerBrowserAuth(
  app: FastifyInstance,
  coreClient: CoreClient,
  config: BffConfig,
): void {
  app.register(cookie, {
    hook: "onRequest",
    secret: config.cookieSecret,
  });
  app.decorateRequest("browserIdentity");

  app.addHook("preHandler", async (request, reply) => {
    if (request.routeOptions.config.auth === "public") {
      return;
    }

    const signed = request.cookies?.[sessionCookieName];
    if (!signed) {
      throw unauthorized();
    }
    const result = request.unsignCookie(signed);
    if (!result.valid || !result.value) {
      throw unauthorized();
    }

    let assertion: unknown;
    try {
      assertion = JSON.parse(
        Buffer.from(result.value, "base64url").toString("utf8"),
      );
    } catch {
      throw unauthorized();
    }
    const parsed = sessionAssertionSchema.safeParse(assertion);
    if (!parsed.success) {
      throw unauthorized();
    }

    let session = parsed.data;
    if (Date.parse(session.expires_at) <= Date.now() + 60_000) {
      try {
        const refreshed = await coreClient.refreshSession(
          session.refresh_token,
          { requestId: request.id },
        );
        setBrowserSessionCookie(reply, refreshed, config);
        session = sessionAssertionSchema.parse({
          access_token: refreshed.access_token,
          created_at: refreshed.user.created_at,
          display_name: refreshed.user.display_name,
          email: refreshed.user.email,
          expires_at: refreshed.expires_at,
          refresh_token: refreshed.refresh_token,
          session_expires_at:
            refreshed.session_expires_at ?? refreshed.expires_at,
          session_id: refreshed.session_id,
          status: refreshed.user.status,
          system_role: refreshed.user.system_role,
          user_id: refreshed.user.id,
        });
      } catch {
        reply.clearCookie(sessionCookieName, { path: "/" });
        throw unauthorized();
      }
    }

    request.browserIdentity = {
      accessToken: session.access_token,
      createdAt: session.created_at,
      displayName: session.display_name,
      email: session.email,
      expiresAt: session.expires_at,
      refreshToken: session.refresh_token,
      sessionExpiresAt: session.session_expires_at ?? session.expires_at,
      sessionId: session.session_id,
      status: session.status,
      systemRole: session.system_role,
      userId: session.user_id,
    };
  });
}

export function setBrowserSessionCookie(
  reply: FastifyReply,
  result: Awaited<ReturnType<CoreClient["login"]>>,
  config: BffConfig,
): void {
  if (!result.refresh_token) {
    throw new Error("Core did not return a refreshable session");
  }
  const sessionExpiresAt = result.session_expires_at ?? result.expires_at;
  const assertion = encodeSessionAssertion({
    access_token: result.access_token,
    created_at: result.user.created_at,
    display_name: result.user.display_name,
    email: result.user.email,
    expires_at: result.expires_at,
    refresh_token: result.refresh_token,
    session_expires_at: sessionExpiresAt,
    session_id: result.session_id,
    status: result.user.status,
    system_role: result.user.system_role,
    user_id: result.user.id,
  });
  reply.setCookie(sessionCookieName, assertion, {
    expires: new Date(sessionExpiresAt),
    httpOnly: true,
    path: "/",
    sameSite: "lax",
    secure: config.nodeEnv === "production",
    signed: true,
  });
}

export function encodeSessionAssertion(assertion: SessionAssertion): string {
  const parsed = sessionAssertionSchema.parse(assertion);
  return Buffer.from(JSON.stringify(parsed), "utf8").toString("base64url");
}

function unauthorized(): BffError {
  return new BffError({
    code: "UNAUTHENTICATED",
    message: "A valid browser session is required",
    status: 401,
  });
}
