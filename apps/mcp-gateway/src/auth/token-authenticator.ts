import type { AuthInfo } from "@modelcontextprotocol/server";
import { CoreClient, CoreClientError } from "@mmdash/core-client";
import { createHash, timingSafeEqual } from "node:crypto";

import type { McpGatewayConfig } from "../config.js";
import { GatewayError } from "../errors/gateway-error.js";

export type TokenKind = "agent" | "cli";

export type AgentCredentialStatus = "active" | "pending";

export type Principal = {
  agentInstanceId?: string;
  credentialStatus?: AgentCredentialStatus;
  delegated: boolean;
  id: string;
  kind: TokenKind;
  projects: readonly string[];
  sessionOwnerId: string;
  tokenId?: string;
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
    private readonly delegatedCliTools: readonly string[] = [],
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
      return delegatedPrincipal(identity, this.delegatedCliTools);
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
      const id = tokenIdentity("cli", config.cliToken);
      records.push({
        principal: {
          delegated: false,
          id,
          kind: "cli",
          projects: config.cliProjects,
          sessionOwnerId: id,
          tools: config.cliTools,
        },
        token: config.cliToken,
      });
    }
    if (config.agentToken) {
      const id = tokenIdentity("agent", config.agentToken);
      records.push({
        principal: {
          delegated: false,
          id,
          kind: "agent",
          projects: config.agentProjects,
          sessionOwnerId: id,
          tools: config.agentTools,
        },
        token: config.agentToken,
      });
    }
    return new TokenAuthenticator(records, coreClient, config.cliTools);
  }
}

function delegatedPrincipal(
  identity: unknown,
  delegatedCliTools: readonly string[],
): Principal {
  const record = asRecord(identity);
  const kind = readString(record.kind);
  if (kind === "session" || kind === "api") {
    const user = asRecord(record.user);
    const userId = readString(user.id);
    if (!userId) {
      throw unauthenticated();
    }
    const projectId = readString(record.project_id);
    const id = `cli:${userId}`;
    return {
      delegated: true,
      id,
      kind: "cli",
      projects: projectId ? [projectId] : ["*"],
      sessionOwnerId: id,
      tools: delegatedCliTools,
    };
  }
  if (kind !== "agent") {
    throw unauthenticated();
  }

  const credentialStatus =
    readString(record.credential_status) ??
    readString(record.token_status) ??
    readString(record.status);
  if (credentialStatus !== "active" && credentialStatus !== "pending") {
    throw new GatewayError(
      "AGENT_CREDENTIAL_INACTIVE",
      "The Agent credential is not active",
      401,
    );
  }
  const agentInstanceId = readUUID(record.agent_instance_id);
  const projectId = readUUID(record.project_id);
  const tokenId = readUUID(record.token_id);
  const tools = readExactTools(record.allowed_tools);
  if (!agentInstanceId || !projectId || !tokenId || !tools) {
    throw new GatewayError(
      "AGENT_IDENTITY_INVALID",
      "The Agent credential grant is invalid",
      401,
    );
  }
  const id = `agent:${agentInstanceId}`;
  return {
    agentInstanceId,
    credentialStatus,
    delegated: true,
    id,
    kind: "agent",
    projects: [projectId],
    sessionOwnerId: `${id}:credential:${tokenId}`,
    tokenId,
    tools,
  };
}

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null
    ? (value as Record<string, unknown>)
    : {};
}

function readString(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function readUUID(value: unknown): string | undefined {
  const candidate = readString(value);
  return candidate && uuidPattern.test(candidate) ? candidate : undefined;
}

function readExactTools(value: unknown): readonly string[] | undefined {
  if (!Array.isArray(value)) {
    return undefined;
  }
  const tools = value.map(readString);
  if (
    tools.length === 0 ||
    tools.some(
      (tool) => !tool || tool.includes("*") || !exactToolNamePattern.test(tool),
    )
  ) {
    return undefined;
  }
  return [...new Set(tools as string[])];
}

const uuidPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const exactToolNamePattern = /^[a-z][a-z0-9_.-]*$/;

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
    !("sessionOwnerId" in principal) ||
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
