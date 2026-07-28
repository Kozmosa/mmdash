import type { FastifyInstance } from "fastify";

import {
  encodeSessionAssertion,
  sessionCookieName,
  type SessionAssertion,
} from "../src/auth/browser-auth.js";
import type { BffConfig } from "../src/config.js";

export const testConfig: BffConfig = {
  cookieSecret: "test-cookie-secret-that-is-at-least-32-characters",
  coreBaseUrl: "http://core.test",
  host: "127.0.0.1",
  nodeEnv: "test",
  port: 3001,
  version: "0.1.0-test",
};

export async function signedSessionCookie(
  app: FastifyInstance,
  overrides: Partial<SessionAssertion> = {},
): Promise<string> {
  await app.ready();
  const assertion = encodeSessionAssertion({
    access_token: "test-access-token",
    created_at: "2026-07-28T00:00:00.000Z",
    display_name: "Test User",
    email: "test@example.com",
    expires_at: new Date(Date.now() + 60_000).toISOString(),
    session_id: "session-1",
    status: "active",
    system_role: "admin",
    user_id: "user-1",
    ...overrides,
  });
  const signed = app.signCookie(assertion);
  return `${sessionCookieName}=${encodeURIComponent(signed)}`;
}
