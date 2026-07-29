import cookie from "@fastify/cookie";
import type { FastifyInstance } from "fastify";
import { z } from "zod";

import { BffError } from "../errors/bff-error.js";

export const sessionCookieName = "mmdash_session";

const sessionAssertionSchema = z.object({
  access_token: z.string().min(1),
  created_at: z.string().datetime({ offset: true }),
  display_name: z.string().min(1).max(200),
  email: z.string().email(),
  expires_at: z.string().datetime({ offset: true }),
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
  sessionId: string;
  status: "active" | "disabled";
  systemRole: "admin" | "member";
  userId: string;
};

export type SessionAssertion = z.input<typeof sessionAssertionSchema>;

export function registerBrowserAuth(
  app: FastifyInstance,
  cookieSecret: string,
): void {
  app.register(cookie, {
    hook: "onRequest",
    secret: cookieSecret,
  });
  app.decorateRequest("browserIdentity");

  app.addHook("preHandler", async (request) => {
    if (request.routeOptions.config.auth === "public") {
      return;
    }

    const signed = request.cookies[sessionCookieName];
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
    if (!parsed.success || Date.parse(parsed.data.expires_at) <= Date.now()) {
      throw unauthorized();
    }

    request.browserIdentity = {
      accessToken: parsed.data.access_token,
      createdAt: parsed.data.created_at,
      displayName: parsed.data.display_name,
      email: parsed.data.email,
      sessionId: parsed.data.session_id,
      status: parsed.data.status,
      systemRole: parsed.data.system_role,
      userId: parsed.data.user_id,
    };
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
