import type { AuthInfo } from "@modelcontextprotocol/server";
import { createHash, timingSafeEqual } from "node:crypto";

import type { McpGatewayConfig } from "../config.js";
import { GatewayError } from "../errors/gateway-error.js";

export type TokenKind = "agent" | "cli";

export type Principal = {
  id: string;
  kind: TokenKind;
  projects: readonly string[];
  tools: readonly string[];
};

type TokenRecord = {
  principal: Principal;
  token: string;
};

export class TokenAuthenticator {
  private readonly records: readonly TokenRecord[];

  constructor(records: readonly TokenRecord[]) {
    this.records = [...records];
  }

  authenticate(authorization: string | null): {
    authInfo: AuthInfo;
    principal: Principal;
  } {
    const token = readBearerToken(authorization);
    const record = this.records.find((candidate) =>
      secretEquals(candidate.token, token),
    );
    if (!record) {
      throw new GatewayError(
        "UNAUTHENTICATED",
        "A valid CLI or Agent token is required",
        401,
      );
    }
    return {
      authInfo: {
        clientId: record.principal.id,
        extra: { principal: record.principal },
        scopes: [
          "mcp:tools",
          `principal:${record.principal.kind}`,
        ],
        token: record.token,
      },
      principal: record.principal,
    };
  }

  static fromConfig(config: McpGatewayConfig): TokenAuthenticator {
    return new TokenAuthenticator([
      {
        principal: {
          id: tokenIdentity("cli", config.cliToken),
          kind: "cli",
          projects: config.cliProjects,
          tools: config.cliTools,
        },
        token: config.cliToken,
      },
      {
        principal: {
          id: tokenIdentity("agent", config.agentToken),
          kind: "agent",
          projects: config.agentProjects,
          tools: config.agentTools,
        },
        token: config.agentToken,
      },
    ]);
  }
}

export function principalFromAuthInfo(authInfo: AuthInfo | undefined): Principal {
  const principal = authInfo?.extra?.principal;
  if (
    typeof principal !== "object" ||
    principal === null ||
    !("id" in principal) ||
    !("kind" in principal) ||
    !("projects" in principal) ||
    !("tools" in principal)
  ) {
    throw new GatewayError(
      "UNAUTHENTICATED",
      "Authenticated principal context is unavailable",
      401,
    );
  }
  return principal as Principal;
}

function readBearerToken(authorization: string | null): string {
  const match = authorization?.match(/^Bearer ([^\s]+)$/i);
  if (!match) {
    throw new GatewayError(
      "UNAUTHENTICATED",
      "A Bearer token is required",
      401,
    );
  }
  return match[1]!;
}

function secretEquals(expected: string, actual: string): boolean {
  const expectedBuffer = Buffer.from(expected);
  const actualBuffer = Buffer.from(actual);
  return (
    expectedBuffer.length === actualBuffer.length &&
    timingSafeEqual(expectedBuffer, actualBuffer)
  );
}

function tokenIdentity(kind: TokenKind, token: string): string {
  return `${kind}:${createHash("sha256").update(token).digest("hex").slice(0, 16)}`;
}
