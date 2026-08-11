import { CoreClientError, type CoreRequestContext } from "@mmdash/core-client";
import type { CallToolResult } from "@modelcontextprotocol/server";
import { z } from "zod/v4";

import { GatewayError, safeError } from "../errors/gateway-error.js";
import type { ToolModule, ToolRegistrationContext } from "./registry.js";

const projectId = z.string().regex(/^[A-Za-z0-9][A-Za-z0-9_-]{1,127}$/);

export const progressGetTool: ToolModule = {
  name: "progress.get",
  register(server, context) {
    server.registerTool(
      this.name,
      {
        annotations: {
          destructiveHint: false,
          idempotentHint: true,
          readOnlyHint: true,
          title: "Read project progress",
        },
        description:
          "Read the authoritative Progress aggregate, including detected stage, effective human override, latest evaluation, tasks, milestones, proposals, and tracking settings.",
        inputSchema: z.object({ project_id: projectId }),
      },
      async ({ project_id }) =>
        execute(context, this.name, project_id, (requestContext) =>
          context.coreClient.getProgress(project_id, requestContext),
        ),
    );
  },
};

export const progressRecalculateTool: ToolModule = {
  name: "progress.recalculate",
  register(server, context) {
    server.registerTool(
      this.name,
      {
        annotations: {
          destructiveHint: false,
          idempotentHint: true,
          readOnlyHint: false,
          title: "Recalculate project progress",
        },
        description:
          "Schedule a debounced, idempotent Progress evaluation. Product Agents must use trigger_kind=cron; human credentials may request manual evaluation and use force to bypass the minimum interval.",
        inputSchema: z.object({
          project_id: projectId,
          trigger_kind: z.enum(["manual", "cron"]).default("manual"),
          force: z.boolean().default(false),
        }),
      },
      async ({ force, project_id, trigger_kind }) =>
        execute(context, this.name, project_id, (requestContext) =>
          context.coreClient.recalculateProgress(
            project_id,
            { force, trigger_kind },
            requestContext,
          ),
        ),
    );
  },
};

async function execute(
  context: ToolRegistrationContext,
  toolName: string,
  projectIdValue: string,
  operation: (requestContext: CoreRequestContext) => Promise<unknown>,
): Promise<CallToolResult> {
  const startedAt = context.now();
  try {
    context.authorizer.assertToolAccess(context.principal, toolName);
    context.authorizer.assertProjectAccess(context.principal, projectIdValue);
    if (!context.coreAccessToken) {
      throw new GatewayError(
        "CORE_ACCESS_TOKEN_REQUIRED",
        "The MCP Core access token is not configured",
        503,
      );
    }
    const result = await operation({
      accessToken: context.coreAccessToken,
      projectId: projectIdValue,
      requestId: context.requestId,
    });
    await audit(context, toolName, projectIdValue, startedAt, "success");
    return {
      content: [{ text: JSON.stringify(result), type: "text" }],
      structuredContent: result as Record<string, unknown>,
    };
  } catch (error) {
    const mapped = mapError(error);
    await audit(
      context,
      toolName,
      projectIdValue,
      startedAt,
      mapped.status === 403 ? "denied" : "error",
      mapped.code,
      false,
    );
    return {
      content: [
        {
          text: JSON.stringify({
            code: mapped.code,
            message: mapped.message,
            request_id: context.requestId,
          }),
          type: "text",
        },
      ],
      isError: true,
    };
  }
}

function mapError(error: unknown): GatewayError {
  if (!(error instanceof CoreClientError)) return safeError(error);
  if (error.status === 403) {
    return new GatewayError(
      "PROGRESS_ACCESS_DENIED",
      "The caller cannot access or evaluate Progress for this project",
      403,
    );
  }
  if (error.status === 409) {
    return new GatewayError(
      "PROGRESS_CONFLICT",
      "The Progress evaluation conflicts with current state",
      409,
    );
  }
  if (error.status >= 500) {
    return new GatewayError("CORE_UNAVAILABLE", "Core is temporarily unavailable", 503);
  }
  return new GatewayError(
    "CORE_REQUEST_FAILED",
    "Core rejected the Progress request",
    error.status,
  );
}

async function audit(
  context: ToolRegistrationContext,
  toolName: string,
  projectIdValue: string,
  startedAt: number,
  outcome: "success" | "denied" | "error",
  errorCode?: string,
  required = true,
): Promise<void> {
  try {
    await context.audit.record({
      actorId: context.principal.id,
      actorKind: context.principal.kind,
      durationMs: context.now() - startedAt,
      errorCode,
      occurredAt: new Date(context.now()).toISOString(),
      outcome,
      projectId: projectIdValue,
      requestId: context.requestId,
      sessionId: context.sessionId,
      toolName,
    });
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
