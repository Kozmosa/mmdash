import cookie from "@fastify/cookie";
import type { FastifyInstance } from "fastify";
import { z } from "zod";

import { BffError } from "../errors/bff-error.js";

export const sessionCookieName = "mmdash_session";

const sessionAssertionSchema = z.object({
  display_name: z.string().min(1).max(200),
  email: z.string().email(),
  expires_at: z.string().datetime(),
  session_id: z.string().min(1).max(200),
  user_id: z.string().min(1).max(200),
});

export type BrowserIdentity = {
  displayName: string;
  email: string;
  sessionId: string;
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
    if (
      !parsed.success ||
      Date.parse(parsed.data.expires_at) <= Date.now()
    ) {
      throw unauthorized();
    }

    request.browserIdentity = {
      displayName: parsed.data.display_name,
      email: parsed.data.email,
      sessionId: parsed.data.session_id,
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
