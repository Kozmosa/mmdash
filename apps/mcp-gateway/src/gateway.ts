import { CoreClient } from "@mmdash/core-client";
import {
  createMcpHandler,
  type McpHandlerRequestOptions,
  type McpHttpHandler,
} from "@modelcontextprotocol/server";
import { randomUUID } from "node:crypto";

import type { AuditSink } from "./audit/audit.js";
import {
  CompositeAuditSink,
  CoreAuditSink,
  JsonAuditSink,
} from "./audit/audit.js";
import {
  principalFromAuthInfo,
  TokenAuthenticator,
} from "./auth/token-authenticator.js";
import type { GatewayAuthorizer } from "./authorization/authorizer.js";
import { PatternAuthorizer } from "./authorization/authorizer.js";
import type { McpGatewayConfig } from "./config.js";
import { loadConfig } from "./config.js";
import { GatewayError } from "./errors/gateway-error.js";
import {
  gatewaySessionHeader,
  SessionRegistry,
} from "./sessions/session-registry.js";
import { ToolRegistry } from "./tools/registry.js";
import { systemEchoTool } from "./tools/system-echo.js";
import {
  projectMemberGetTool,
  projectMemberListTool,
} from "./tools/project-members.js";

export type GatewayFetchHandler = {
  close(): Promise<void>;
  fetch(request: Request): Promise<Response>;
  tools: readonly string[];
};

export type BuildGatewayOptions = {
  audit?: AuditSink;
  authorizer?: GatewayAuthorizer;
  config?: McpGatewayConfig;
  coreClient?: CoreClient;
  now?: () => number;
  registry?: ToolRegistry;
  sessions?: SessionRegistry;
};

export function buildGateway(
  options: BuildGatewayOptions = {},
): GatewayFetchHandler {
  const config = options.config ?? loadConfig();
  const authorizer = options.authorizer ?? new PatternAuthorizer();
  const coreClient = options.coreClient ?? new CoreClient(config.coreBaseUrl);
  const audit =
    options.audit ??
    (config.coreAuditToken
      ? new CompositeAuditSink([
          new JsonAuditSink(),
          new CoreAuditSink(coreClient, config.coreAuditToken),
        ])
      : new JsonAuditSink());
  const now = options.now ?? Date.now;
  const registry = options.registry ?? createDefaultToolRegistry();
  const sessions =
    options.sessions ?? new SessionRegistry(config.sessionTtlMs, now);
  const authenticator = TokenAuthenticator.fromConfig(config);

  const handler = createMcpHandler(
    (requestContext) => {
      const principal = principalFromAuthInfo(requestContext.authInfo);
      return registry.createServer({
        audit,
        authorizer,
        coreClient,
        coreAccessToken: config.coreAccessToken,
        now,
        principal,
        requestId: readExtra(requestContext.authInfo?.extra, "requestId"),
        sessionId: readExtra(requestContext.authInfo?.extra, "sessionId"),
        version: config.version,
      });
    },
    {
      legacy: "stateless",
      onerror(error) {
        process.stderr.write(
          `${JSON.stringify({
            error_name: error.name,
            event: "mcp.handler.error",
            message: "MCP handler failed",
          })}\n`,
        );
      },
      responseMode: "auto",
    },
  );

  return {
    close: () => handler.close(),
    fetch: (request) =>
      handleGatewayRequest(request, config, authenticator, sessions, handler),
    tools: registry.list(),
  };
}

function createDefaultToolRegistry(): ToolRegistry {
  const registry = new ToolRegistry();
  registry.register(systemEchoTool);
  registry.register(projectMemberListTool);
  registry.register(projectMemberGetTool);
  return registry;
}

async function handleGatewayRequest(
  request: Request,
  config: McpGatewayConfig,
  authenticator: TokenAuthenticator,
  sessions: SessionRegistry,
  handler: McpHttpHandler,
): Promise<Response> {
  const url = new URL(request.url);
  if (url.pathname === "/health/live") {
    return jsonResponse(200, {
      service: "mcp-gateway",
      status: "ok",
      version: config.version,
    });
  }
  if (url.pathname !== "/mcp" && !url.pathname.startsWith("/mcp/")) {
    return jsonResponse(404, {
      code: "NOT_FOUND",
      message: "Route not found",
    });
  }

  try {
    validateRequestOrigin(request, config);
    const { authInfo, principal } = authenticator.authenticate(
      request.headers.get("authorization"),
    );
    const suppliedSession = request.headers.get(gatewaySessionHeader);
    if (request.method === "DELETE" && suppliedSession) {
      sessions.terminate(suppliedSession, principal.id);
      return new Response(null, { status: 204 });
    }
    const session = sessions.resolve(suppliedSession, principal.id);
    const requestId = resolveRequestId(request.headers.get("x-request-id"));
    const options: McpHandlerRequestOptions = {
      authInfo: {
        ...authInfo,
        extra: {
          ...authInfo.extra,
          requestId,
          sessionId: session.id,
        },
      },
    };
    const response = await handler.fetch(request, options);
    return withGatewayHeaders(response, requestId, session.id);
  } catch (error) {
    const safe =
      error instanceof GatewayError
        ? error
        : new GatewayError(
            "INTERNAL_ERROR",
            "The MCP request could not be processed",
            500,
          );
    const headers = new Headers({
      "content-type": "application/json",
      "x-request-id": resolveRequestId(request.headers.get("x-request-id")),
    });
    if (safe.status === 401) {
      headers.set("www-authenticate", 'Bearer realm="mmdash-mcp"');
    }
    return new Response(
      JSON.stringify({ code: safe.code, message: safe.message }),
      { headers, status: safe.status },
    );
  }
}

function validateRequestOrigin(
  request: Request,
  config: McpGatewayConfig,
): void {
  const url = new URL(request.url);
  if (!config.allowedHosts.includes(url.hostname)) {
    throw new GatewayError(
      "HOST_NOT_ALLOWED",
      "The request host is not allowed",
      421,
    );
  }
  const origin = request.headers.get("origin");
  if (origin && !config.allowedOrigins.includes(origin)) {
    throw new GatewayError(
      "ORIGIN_NOT_ALLOWED",
      "The request origin is not allowed",
      403,
    );
  }
}

function withGatewayHeaders(
  response: Response,
  requestId: string,
  sessionId: string,
): Response {
  const headers = new Headers(response.headers);
  headers.set(gatewaySessionHeader, sessionId);
  headers.set("x-request-id", requestId);
  return new Response(response.body, {
    headers,
    status: response.status,
    statusText: response.statusText,
  });
}

function resolveRequestId(value: string | null): string {
  return value && /^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$/.test(value)
    ? value
    : randomUUID();
}

function readExtra(
  extra: Record<string, unknown> | undefined,
  key: string,
): string {
  const value = extra?.[key];
  if (typeof value !== "string") {
    throw new GatewayError(
      "REQUEST_CONTEXT_MISSING",
      "The MCP request context is incomplete",
      500,
    );
  }
  return value;
}

function jsonResponse(status: number, body: unknown): Response {
  return Response.json(body, { status });
}
