import type { CallToolResult } from "@modelcontextprotocol/server";
import { z } from "zod/v4";

import { GatewayError, safeError } from "../errors/gateway-error.js";
import type { ToolModule, ToolRegistrationContext } from "./registry.js";

export const systemEchoToolName = "system.echo";

const inputSchema = z.object({
  message: z.string().min(1).max(4_096),
  project_id: z.string().regex(/^[A-Za-z0-9][A-Za-z0-9_-]{1,127}$/),
});
const outputSchema = z.object({
  message: z.string(),
  principal_kind: z.enum(["agent", "cli"]),
  project_id: z.string(),
  request_id: z.string(),
  session_id: z.string(),
});

export const systemEchoTool: ToolModule = {
  name: systemEchoToolName,
  register(server, context) {
    server.registerTool(
      systemEchoToolName,
      {
        annotations: {
          destructiveHint: false,
          idempotentHint: true,
          readOnlyHint: true,
          title: "Echo gateway context",
        },
        description:
          "Test MCP authentication, project/tool authorization, session correlation, validation, and audit.",
        inputSchema,
        outputSchema,
        title: "System Echo",
      },
      async ({ message, project_id }): Promise<CallToolResult> =>
        executeEcho(context, message, project_id),
    );
  },
};

async function executeEcho(
  context: ToolRegistrationContext,
  message: string,
  projectId: string,
): Promise<CallToolResult> {
  const startedAt = context.now();
  try {
    context.authorizer.assertToolAccess(context.principal, systemEchoToolName);
    context.authorizer.assertProjectAccess(context.principal, projectId);
    const output = {
      message,
      principal_kind: context.principal.kind,
      project_id: projectId,
      request_id: context.requestId,
      session_id: context.sessionId,
    };
    await recordAudit(context, {
      actorId: context.principal.id,
      actorKind: context.principal.kind,
      durationMs: context.now() - startedAt,
      occurredAt: new Date(context.now()).toISOString(),
      outcome: "success",
      projectId,
      requestId: context.requestId,
      sessionId: context.sessionId,
      toolName: systemEchoToolName,
    });
    return {
      content: [{ text: JSON.stringify(output), type: "text" }],
      structuredContent: output,
    };
  } catch (error) {
    const safe = safeError(error);
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
        toolName: systemEchoToolName,
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
