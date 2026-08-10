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
  type Principal,
} from "./auth/token-authenticator.js";
import type { GatewayAuthorizer } from "./authorization/authorizer.js";
import { PatternAuthorizer } from "./authorization/authorizer.js";
import type { McpGatewayConfig } from "./config.js";
import { loadConfig } from "./config.js";
import { GatewayError } from "./errors/gateway-error.js";
import {
  gatewaySessionHeader,
  mcpSessionHeader,
  SessionRegistry,
} from "./sessions/session-registry.js";
import { dataListTool, dataReadTool } from "./tools/data.js";
import { contextPromoteTool } from "./tools/context-promote.js";
import { ToolRegistry } from "./tools/registry.js";
import { systemEchoTool } from "./tools/system-echo.js";
import {
  projectMemberGetTool,
  projectMemberListTool,
} from "./tools/project-members.js";
import { projectGetTool, projectListTool } from "./tools/projects.js";
import {
  progressGetTool,
  progressRecalculateTool,
} from "./tools/progress.js";
import {
  experimentCreateTool,
  experimentRunTool,
  experimentStatusTool,
  resultGetTool,
} from "./tools/experiment.js";

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
  const configuredAudit = options.audit;
  const now = options.now ?? Date.now;
  const registry = options.registry ?? createDefaultToolRegistry();
  const sessions =
    options.sessions ?? new SessionRegistry(config.sessionTtlMs, now);
  const authenticator = TokenAuthenticator.fromConfig(config, coreClient);

  const handler = createMcpHandler(
    (requestContext) => {
      const principal = principalFromAuthInfo(requestContext.authInfo);
      return registry.createServer({
        audit: resolveAuditSink(configuredAudit, coreClient, config),
        authorizer,
        coreClient,
        coreAccessToken: resolveDelegatedCoreToken(
          principal,
          requestContext.authInfo?.token,
          config.coreAccessToken,
        ),
        coreGatewayAccessToken:
          principal.delegated && principal.kind === "agent"
            ? config.coreAccessToken
            : undefined,
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
      handleGatewayRequest(
        request,
        config,
        authenticator,
        sessions,
        handler,
        coreClient,
      ),
    tools: registry.list(),
  };
}

function resolveAuditSink(
  configured: AuditSink | undefined,
  coreClient: CoreClient,
  config: McpGatewayConfig,
): AuditSink {
  if (configured) {
    return configured;
  }
  const coreToken = config.coreAuditToken;
  return coreToken
    ? new CompositeAuditSink([
        new JsonAuditSink(),
        new CoreAuditSink(coreClient, coreToken),
      ])
    : new JsonAuditSink();
}

function createDefaultToolRegistry(): ToolRegistry {
  const registry = new ToolRegistry();
  registry.register(contextPromoteTool);
  registry.register(experimentCreateTool);
  registry.register(experimentRunTool);
  registry.register(experimentStatusTool);
  registry.register(dataListTool);
  registry.register(dataReadTool);
  registry.register(projectGetTool);
  registry.register(projectListTool);
  registry.register(progressGetTool);
  registry.register(progressRecalculateTool);
  registry.register(resultGetTool);
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
  coreClient: CoreClient,
): Promise<Response> {
  const url = new URL(request.url);
  if (url.pathname === "/health/live" || url.pathname === "/mcp/health/live") {
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
    const requestId = resolveRequestId(request.headers.get("x-request-id"));
    const { authInfo, principal } = await authenticator.authenticate(
      request.headers.get("authorization"),
      requestId,
    );
    const mcpRequest = await readMcpRequest(request);
    const suppliedSession = readGatewaySession(request.headers);
    if (request.method === "DELETE" && suppliedSession) {
      sessions.terminate(suppliedSession, principal.sessionOwnerId);
      return new Response(null, { status: 204 });
    }
    const session = sessions.resolve(suppliedSession, principal.sessionOwnerId);
    if (
      isPendingAgent(principal) &&
      mcpRequest &&
      !pendingAgentMethodAllowed(mcpRequest.method)
    ) {
      throw new GatewayError(
        "AGENT_CREDENTIAL_PENDING",
        "The pending Agent credential can only initialize and list tools",
        403,
      );
    }
    if (
      isPendingAgent(principal) &&
      mcpRequest?.method === "tools/list" &&
      (!suppliedSession || !session.initialized || mcpRequest.id === undefined)
    ) {
      throw new GatewayError(
        "MCP_SESSION_NOT_INITIALIZED",
        "Agent verification requires an initialized MCP session",
        409,
      );
    }
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
    if (
      mcpRequest?.method === "initialize" ||
      mcpRequest?.method === "server/discover"
    ) {
      const result =
        mcpRequest.id === undefined
          ? undefined
          : await readMcpResult(response, mcpRequest.id);
      if (isSessionNegotiationResult(mcpRequest.method, result)) {
        sessions.markInitialized(session.id, principal.sessionOwnerId);
      }
    }
    if (isPendingAgent(principal) && mcpRequest?.method === "tools/list") {
      const result =
        mcpRequest.id === undefined
          ? undefined
          : await readMcpResult(response, mcpRequest.id);
      const listedTools = readListedToolNames(result);
      if (!listedTools || !sameExactTools(listedTools, principal.tools)) {
        throw verificationUnavailable();
      }
      await recordAgentTokenVerification(
        coreClient,
        config,
        principal,
        session.id,
        requestId,
      );
    }
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

type McpRequest = {
  id?: number | string;
  method: string;
};

async function readMcpRequest(
  request: Request,
): Promise<McpRequest | undefined> {
  if (request.method !== "POST") {
    return undefined;
  }
  try {
    const body = await request.clone().json();
    if (Array.isArray(body)) {
      return { method: "$batch" };
    }
    if (typeof body !== "object" || body === null) {
      return undefined;
    }
    const record = body as Record<string, unknown>;
    const id = record.id;
    const method = record.method;
    if (typeof method !== "string") {
      return undefined;
    }
    return typeof id === "number" || typeof id === "string"
      ? { id, method }
      : { method };
  } catch {
    return undefined;
  }
}

async function readMcpResult(
  response: Response,
  requestId: number | string,
): Promise<Record<string, unknown> | undefined> {
  if (!response.ok) {
    return undefined;
  }
  try {
    const bodies = await readMcpResponseBodies(response);
    for (const body of bodies) {
      if (typeof body !== "object" || body === null || Array.isArray(body)) {
        continue;
      }
      const record = body as Record<string, unknown>;
      if (
        record.id !== requestId ||
        !("result" in record) ||
        "error" in record
      ) {
        continue;
      }
      const result = record.result;
      if (
        typeof result === "object" &&
        result !== null &&
        !Array.isArray(result)
      ) {
        return result as Record<string, unknown>;
      }
    }
    return undefined;
  } catch {
    return undefined;
  }
}

async function readMcpResponseBodies(response: Response): Promise<unknown[]> {
  const contentType = response.headers.get("content-type") ?? "";
  if (contentType.includes("application/json")) {
    return [await response.clone().json()];
  }
  if (!contentType.includes("text/event-stream")) {
    return [];
  }
  const messages: unknown[] = [];
  let dataLines: string[] = [];
  const flush = () => {
    if (dataLines.length === 0) {
      return;
    }
    try {
      messages.push(JSON.parse(dataLines.join("\n")) as unknown);
    } catch {
      // Ignore non-JSON SSE frames and continue looking for the terminal result.
    }
    dataLines = [];
  };
  for (const line of (await response.clone().text()).split(/\r?\n/)) {
    if (line.length === 0) {
      flush();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice(5).trimStart());
    }
  }
  flush();
  return messages;
}

function isSessionNegotiationResult(
  method: "initialize" | "server/discover",
  result: Record<string, unknown> | undefined,
): boolean {
  if (
    !result ||
    typeof result.capabilities !== "object" ||
    result.capabilities === null
  ) {
    return false;
  }
  if (method === "initialize") {
    return typeof result.protocolVersion === "string";
  }
  return (
    result.resultType === "complete" &&
    Array.isArray(result.supportedVersions) &&
    result.supportedVersions.length > 0 &&
    result.supportedVersions.every((version) => typeof version === "string")
  );
}

function readListedToolNames(
  result: Record<string, unknown> | undefined,
): readonly string[] | undefined {
  if (!result || !Array.isArray(result.tools)) {
    return undefined;
  }
  const names = result.tools.map((tool) => {
    if (typeof tool !== "object" || tool === null || Array.isArray(tool)) {
      return undefined;
    }
    const name = (tool as Record<string, unknown>).name;
    return typeof name === "string" ? name : undefined;
  });
  return names.some((name) => !name) ? undefined : (names as readonly string[]);
}

function sameExactTools(
  listedTools: readonly string[],
  grantedTools: readonly string[],
): boolean {
  const listed = [...listedTools].sort();
  const granted = [...grantedTools].sort();
  return (
    listed.length === granted.length &&
    listed.every((tool, index) => tool === granted[index])
  );
}

async function recordAgentTokenVerification(
  coreClient: CoreClient,
  config: McpGatewayConfig,
  principal: Principal,
  sessionId: string,
  requestId: string,
): Promise<void> {
  const agentInstanceId = principal.agentInstanceId;
  const projectId = principal.projects[0];
  const tokenId = principal.tokenId;
  if (!config.coreAccessToken || !agentInstanceId || !projectId || !tokenId) {
    throw verificationUnavailable();
  }
  try {
    const evidence = await coreClient.recordAgentTokenVerification(
      tokenId,
      {
        agent_instance_id: agentInstanceId,
        mcp_method: "tools/list",
        mcp_session_id: sessionId,
        project_id: projectId,
        request_id: requestId,
      },
      {
        accessToken: config.coreAccessToken,
        projectId,
        requestId,
      },
    );
    if (
      evidence.token_id !== tokenId ||
      evidence.agent_instance_id !== agentInstanceId ||
      evidence.project_id !== projectId ||
      evidence.mcp_method !== "tools/list" ||
      !evidence.mcp_session_id ||
      !evidence.request_id
    ) {
      throw new Error("Agent verification evidence did not match the request");
    }
  } catch {
    throw verificationUnavailable();
  }
}

function isPendingAgent(principal: Principal): boolean {
  return (
    principal.delegated &&
    principal.kind === "agent" &&
    principal.credentialStatus === "pending"
  );
}

function pendingAgentMethodAllowed(method: string): boolean {
  return (
    method === "initialize" ||
    method === "server/discover" ||
    method === "ping" ||
    method === "tools/list" ||
    method.startsWith("notifications/")
  );
}

function resolveDelegatedCoreToken(
  principal: Principal,
  delegatedToken: string | undefined,
  serviceToken: string | undefined,
): string | undefined {
  if (!principal.delegated) {
    return serviceToken;
  }
  return isPendingAgent(principal) ? undefined : delegatedToken;
}

function verificationUnavailable(): GatewayError {
  return new GatewayError(
    "AGENT_VERIFICATION_UNAVAILABLE",
    "Agent credential verification is temporarily unavailable",
    503,
  );
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
  // Hermes v2026.8.3 uses the 2025 Streamable HTTP session header. Keep the
  // application-owned header for current-protocol clients and emit both with
  // the same principal-bound logical session ID.
  headers.set(mcpSessionHeader, sessionId);
  headers.set("x-request-id", requestId);
  return new Response(response.body, {
    headers,
    status: response.status,
    statusText: response.statusText,
  });
}

function readGatewaySession(headers: Headers): string | null {
  const standard = headers.get(mcpSessionHeader);
  const application = headers.get(gatewaySessionHeader);
  if (standard && application && standard !== application) {
    throw new GatewayError(
      "MCP_SESSION_HEADER_CONFLICT",
      "MCP session headers do not match",
      400,
    );
  }
  return standard ?? application;
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
