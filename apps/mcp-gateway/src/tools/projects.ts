import { CoreClientError } from "@mmdash/core-client";
import type { CallToolResult } from "@modelcontextprotocol/server";
import { z } from "zod/v4";

import { GatewayError, safeError } from "../errors/gateway-error.js";
import type { ToolModule, ToolRegistrationContext } from "./registry.js";

const projectIdSchema = z.string().regex(/^[A-Za-z0-9][A-Za-z0-9_-]{1,127}$/);

export const projectListTool: ToolModule = {
  name: "project.list",
  register(server, context) {
    server.registerTool(
      this.name,
      {
        annotations: {
          destructiveHint: false,
          idempotentHint: true,
          readOnlyHint: true,
          title: "List accessible projects",
        },
        description:
          "List active projects visible to the delegated mmdash identity.",
        inputSchema: z.object({}),
      },
      async () => executeProjectList(context),
    );
  },
};

export const projectGetTool: ToolModule = {
  name: "project.get",
  register(server, context) {
    server.registerTool(
      this.name,
      {
        annotations: {
          destructiveHint: false,
          idempotentHint: true,
          readOnlyHint: true,
          title: "Read project metadata",
        },
        description:
          "Read one authorized project's metadata, problem statement, and source references.",
        inputSchema: z.object({ project_id: projectIdSchema }),
      },
      async ({ project_id }) =>
        executeProjectRead(context, this.name, project_id, () =>
          context.coreClient.getProject(
            project_id,
            coreContext(context, project_id),
          ),
        ),
    );
  },
};

async function executeProjectList(
  context: ToolRegistrationContext,
): Promise<CallToolResult> {
  const startedAt = context.now();
  try {
    context.authorizer.assertToolAccess(
      context.principal,
      projectListTool.name,
    );
    const result = await context.coreClient.listProjects(coreContext(context));
    const filtered = {
      ...result,
      items: result.items.filter((project) =>
        context.authorizer.canAccessProject(context.principal, project.id),
      ),
    };
    await audit(context, projectListTool.name, startedAt, "success");
    return resultContent(filtered);
  } catch (error) {
    return toolFailure(
      context,
      projectListTool.name,
      startedAt,
      undefined,
      error,
    );
  }
}

async function executeProjectRead(
  context: ToolRegistrationContext,
  toolName: string,
  projectId: string,
  read: () => Promise<unknown>,
): Promise<CallToolResult> {
  const startedAt = context.now();
  try {
    context.authorizer.assertToolAccess(context.principal, toolName);
    context.authorizer.assertProjectAccess(context.principal, projectId);
    const result = await read();
    await audit(context, toolName, startedAt, "success", projectId);
    return resultContent(result);
  } catch (error) {
    return toolFailure(context, toolName, startedAt, projectId, error);
  }
}

function coreContext(context: ToolRegistrationContext, projectId?: string) {
  if (!context.coreAccessToken) {
    throw new GatewayError(
      "CORE_CREDENTIAL_MISSING",
      "The delegated Core credential is unavailable",
      503,
    );
  }
  return {
    accessToken: context.coreAccessToken,
    gatewayAccessToken: context.coreGatewayAccessToken,
    projectId,
    requestId: context.requestId,
  };
}

function projectError(error: unknown): GatewayError {
  if (!(error instanceof CoreClientError)) {
    return safeError(error);
  }
  if (error.status === 401) {
    return new GatewayError("UNAUTHENTICATED", "The user session expired", 401);
  }
  if (error.status === 403) {
    return new GatewayError(
      "PROJECT_ACCESS_DENIED",
      "Project access was denied",
      403,
    );
  }
  if (error.status === 404) {
    return new GatewayError(
      "PROJECT_NOT_FOUND",
      "The project was not found",
      404,
    );
  }
  if (error.status >= 500) {
    return new GatewayError(
      "CORE_UNAVAILABLE",
      "Core is temporarily unavailable",
      503,
    );
  }
  return new GatewayError(
    "CORE_REQUEST_FAILED",
    "Core rejected the project request",
    error.status,
  );
}

async function toolFailure(
  context: ToolRegistrationContext,
  toolName: string,
  startedAt: number,
  projectId: string | undefined,
  error: unknown,
): Promise<CallToolResult> {
  const safe = projectError(error);
  try {
    await audit(
      context,
      toolName,
      startedAt,
      safe.status === 403 ? "denied" : "error",
      projectId,
      safe.code,
    );
  } catch {
    // The original safe failure remains the user-facing result.
  }
  return {
    content: [
      {
        text: JSON.stringify({
          code: safe.code,
          message: safe.message,
          request_id: context.requestId,
        }),
        type: "text",
      },
    ],
    isError: true,
  };
}

async function audit(
  context: ToolRegistrationContext,
  toolName: string,
  startedAt: number,
  outcome: "denied" | "error" | "success",
  projectId?: string,
  errorCode?: string,
) {
  await context.audit.record({
    actorId: context.principal.id,
    actorKind: context.principal.kind,
    durationMs: context.now() - startedAt,
    errorCode,
    occurredAt: new Date(context.now()).toISOString(),
    outcome,
    projectId,
    requestId: context.requestId,
    sessionId: context.sessionId,
    toolName,
  });
}

function resultContent(result: unknown): CallToolResult {
  return {
    content: [{ text: JSON.stringify(result), type: "text" }],
    structuredContent: result as Record<string, unknown>,
  };
}
