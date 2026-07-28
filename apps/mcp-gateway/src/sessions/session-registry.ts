import { randomUUID } from "node:crypto";

import { GatewayError } from "../errors/gateway-error.js";

export const gatewaySessionHeader = "x-mmdash-session-id";

export type GatewaySession = {
  createdAt: number;
  expiresAt: number;
  id: string;
  principalId: string;
  requestCount: number;
};

export class SessionRegistry {
  private readonly sessions = new Map<string, GatewaySession>();

  constructor(
    private readonly ttlMs: number,
    private readonly now: () => number = Date.now,
    private readonly maxSessions = 10_000,
  ) {}

  resolve(sessionId: string | null, principalId: string): GatewaySession {
    this.prune();
    if (sessionId) {
      const current = this.sessions.get(sessionId);
      if (!current) {
        throw new GatewayError(
          "SESSION_NOT_FOUND",
          "The gateway session is unavailable",
          404,
        );
      }
      if (current.principalId !== principalId) {
        throw new GatewayError(
          "SESSION_PRINCIPAL_MISMATCH",
          "The gateway session belongs to another principal",
          403,
        );
      }
      current.expiresAt = this.now() + this.ttlMs;
      current.requestCount += 1;
      return current;
    }

    if (this.sessions.size >= this.maxSessions) {
      throw new GatewayError(
        "SESSION_CAPACITY_REACHED",
        "The gateway cannot create another session",
        503,
      );
    }
    const createdAt = this.now();
    const session: GatewaySession = {
      createdAt,
      expiresAt: createdAt + this.ttlMs,
      id: randomUUID(),
      principalId,
      requestCount: 1,
    };
    this.sessions.set(session.id, session);
    return session;
  }

  terminate(sessionId: string, principalId: string): boolean {
    const session = this.sessions.get(sessionId);
    if (!session) {
      return false;
    }
    if (session.principalId !== principalId) {
      throw new GatewayError(
        "SESSION_PRINCIPAL_MISMATCH",
        "The gateway session belongs to another principal",
        403,
      );
    }
    return this.sessions.delete(sessionId);
  }

  private prune(): void {
    const now = this.now();
    for (const [id, session] of this.sessions) {
      if (session.expiresAt <= now) {
        this.sessions.delete(id);
      }
    }
  }
}
