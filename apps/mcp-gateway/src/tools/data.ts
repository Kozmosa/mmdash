import { CoreClientError, type CoreRequestContext } from "@mmdash/core-client";
import type { CallToolResult } from "@modelcontextprotocol/server";
import { z } from "zod/v4";

import { GatewayError, safeError } from "../errors/gateway-error.js";
import type { ToolModule, ToolRegistrationContext } from "./registry.js";

const projectIdSchema = z.string().regex(/^[A-Za-z0-9][A-Za-z0-9_-]{1,127}$/);
const dataListInput = z.object({
  cursor: z
    .string()
    .max(4_096)
    .optional()
    .describe("Opaque cursor from the preceding page; paginate only to answer a specific evidence question."),
  limit: z
    .number()
    .int()
    .min(1)
    .max(200)
    .optional()
    .describe("Bounded discovery size; prefer a small page such as 20 for evidence assessment."),
  project_id: projectIdSchema.describe(
    "Exact Project ID supplied by the evaluation or user request.",
  ),
  type: z
    .string()
    .regex(/^[a-z][a-z0-9_-]{0,99}$/)
    .optional()
    .describe(
      "One evidence type to inspect, for example repo_commit, model_snapshot, experiment_run, or article_draft.",
    ),
});
const dataReadInput = z.object({
  object_id: z
    .string()
    .uuid()
    .describe("Object ID selected from data.list because its full content can answer a material question."),
  project_id: projectIdSchema.describe(
    "Exact Project ID used in the preceding data.list call.",
  ),
});

export const dataListTool: ToolModule = {
  name: "data.list",
  register(server, context) {
    server.registerTool(
      this.name,
      {
        annotations: {
          destructiveHint: false,
          idempotentHint: true,
          readOnlyHint: true,
          title: "List project data",
        },
        description:
          "Evidence discovery index for one Project. Filter by one domain type and keep the page small. Titles, summaries, counts, and status are navigation hints, not proof of object content; follow relevant results with data.read.",
        inputSchema: dataListInput,
      },
      async ({ cursor, limit, project_id, type }) =>
        executeDataTool(context, this.name, project_id, (requestContext) =>
          context.coreClient.listDataObjects(project_id, requestContext, {
            cursor,
            limit,
            type,
          }),
        ),
    );
  },
};

export const dataReadTool: ToolModule = {
  name: "data.read",
  register(server, context) {
    server.registerTool(
      this.name,
      {
        annotations: {
          destructiveHint: false,
          idempotentHint: true,
          readOnlyHint: true,
          title: "Read project data",
        },
        description:
          "Authoritative evidence read for one object selected through data.list. Use the returned domain content to support material claims; read only decision-relevant objects rather than the whole Project. Artifact transfers remain controlled and Repo files revision-pinned.",
        inputSchema: dataReadInput,
      },
      async ({ object_id, project_id }) =>
        executeDataTool(context, this.name, project_id, (requestContext) =>
          context.coreClient.readDataObject(
            project_id,
            object_id,
            requestContext,
          ),
        ),
    );
  },
};

async function executeDataTool(
  context: ToolRegistrationContext,
  toolName: string,
  projectId: string,
  operation: (requestContext: CoreRequestContext) => Promise<unknown>,
): Promise<CallToolResult> {
  const startedAt = context.now();
  try {
    context.authorizer.assertToolAccess(context.principal, toolName);
    context.authorizer.assertProjectAccess(context.principal, projectId);
    if (!context.coreAccessToken) {
      throw new GatewayError(
        "CORE_ACCESS_TOKEN_REQUIRED",
        "The MCP Core access token is not configured",
        503,
      );
    }
    const result = await operation({
      accessToken: context.coreAccessToken,
      projectId,
      requestId: context.requestId,
    });
    await recordAudit(context, {
      actorId: context.principal.id,
      actorKind: context.principal.kind,
      durationMs: context.now() - startedAt,
      occurredAt: new Date(context.now()).toISOString(),
      outcome: "success",
      projectId,
      requestId: context.requestId,
      sessionId: context.sessionId,
      toolName,
    });
    return {
      content: [{ text: JSON.stringify(result), type: "text" }],
      structuredContent: result as Record<string, unknown>,
    };
  } catch (error) {
    const safe = dataToolError(error);
    await recordAudit(
      context,
      {
        actorId: context.principal.id,
        actorKind: context.principal.kind,
        durationMs: context.now() - startedAt,
        errorCode: safe.code,
        occurredAt: new Date(context.now()).toISOString(),
        outcome: safe.status === 403 ? "denied" : "error",
        projectId,
        requestId: context.requestId,
        sessionId: context.sessionId,
        toolName,
      },
      false,
    );
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
}

function dataToolError(error: unknown): GatewayError {
  if (!(error instanceof CoreClientError)) {
    return safeError(error);
  }
  if (error.status === 403) {
    return new GatewayError(
      "DATA_ACCESS_DENIED",
      "The Core token cannot access this project data",
      403,
    );
  }
  if (error.status === 404) {
    return new GatewayError(
      "DATA_OBJECT_NOT_FOUND",
      "The requested project data object was not found",
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
    "Core rejected the project data request",
    error.status,
  );
}

async function recordAudit(
  context: ToolRegistrationContext,
  event: Parameters<ToolRegistrationContext["audit"]["record"]>[0],
  required = true,
): Promise<void> {
  try {
    await context.audit.record(event);
  } catch {
    if (required) {
      throw new GatewayError(
        "AUDIT_UNAVAILABLE",
        "The tool audit service is unavailable",
        503,
      );
    }
  }
}
