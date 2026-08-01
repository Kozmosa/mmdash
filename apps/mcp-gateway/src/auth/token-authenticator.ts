import type { AuthInfo } from "@modelcontextprotocol/server";
import { CoreClient, CoreClientError } from "@mmdash/core-client";
import { createHash, timingSafeEqual } from "node:crypto";

import type { McpGatewayConfig } from "../config.js";
import { GatewayError } from "../errors/gateway-error.js";

export type TokenKind = "agent" | "cli";

export type Principal = {
  delegated: boolean;
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

  constructor(
    records: readonly TokenRecord[],
    private readonly coreClient?: CoreClient,
    private readonly delegatedTools: readonly string[] = [],
  ) {
    this.records = [...records];
  }

  async authenticate(
    authorization: string | null,
    requestId: string,
  ): Promise<{
    authInfo: AuthInfo;
    principal: Principal;
  }> {
    const token = readBearerToken(authorization);
    const record = this.records.find((candidate) =>
      secretEquals(candidate.token, token),
    );
    const principal =
      record?.principal ?? (await this.authenticateDelegated(token, requestId));
    return {
      authInfo: {
        clientId: principal.id,
        extra: { principal },
        scopes: ["mcp:tools", `principal:${principal.kind}`],
        token,
      },
      principal,
    };
  }

  private async authenticateDelegated(
    token: string,
    requestId: string,
  ): Promise<Principal> {
    if (!this.coreClient) {
      throw unauthenticated();
    }
    try {
      const identity = await this.coreClient.currentIdentity({
        accessToken: token,
        requestId,
      });
      if (identity.kind !== "session" && identity.kind !== "api") {
        throw unauthenticated();
      }
      return {
        delegated: true,
        id: `cli:${identity.user.id}`,
        kind: "cli",
        projects: identity.project_id ? [identity.project_id] : ["*"],
        tools: this.delegatedTools,
      };
    } catch (error) {
      if (error instanceof GatewayError) {
        throw error;
      }
      if (error instanceof CoreClientError && error.status < 500) {
        throw unauthenticated();
      }
      if (error instanceof CoreClientError || error instanceof TypeError) {
        throw new GatewayError(
          "AUTH_SERVICE_UNAVAILABLE",
          "Authentication is temporarily unavailable",
          503,
        );
      }
      throw new GatewayError(
        "AUTH_SERVICE_UNAVAILABLE",
        "Authentication is temporarily unavailable",
        503,
      );
    }
  }

  static fromConfig(
    config: McpGatewayConfig,
    coreClient?: CoreClient,
  ): TokenAuthenticator {
    const records: TokenRecord[] = [];
    if (config.cliToken) {
      records.push({
        principal: {
          delegated: false,
          id: tokenIdentity("cli", config.cliToken),
          kind: "cli",
          projects: config.cliProjects,
          tools: config.cliTools,
        },
        token: config.cliToken,
      });
    }
    if (config.agentToken) {
      records.push({
        principal: {
          delegated: false,
          id: tokenIdentity("agent", config.agentToken),
          kind: "agent",
          projects: config.agentProjects,
          tools: config.agentTools,
        },
        token: config.agentToken,
      });
    }
    return new TokenAuthenticator(records, coreClient, config.cliTools);
  }
}

function unauthenticated(): GatewayError {
  return new GatewayError(
    "UNAUTHENTICATED",
    "A valid user or Agent token is required",
    401,
  );
}

export function principalFromAuthInfo(
  authInfo: AuthInfo | undefined,
): Principal {
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
