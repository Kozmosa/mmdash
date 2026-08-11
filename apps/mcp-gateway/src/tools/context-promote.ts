import { CoreClientError } from "@mmdash/core-client";
import type { CallToolResult } from "@modelcontextprotocol/server";
import { z } from "zod/v4";

import { GatewayError, safeError } from "../errors/gateway-error.js";
import type { ToolModule, ToolRegistrationContext } from "./registry.js";

const projectIdSchema = z.string().uuid();
const inputSchema = z
  .object({
    agent_run_id: z.string().uuid().optional(),
    agent_session_id: z.string().uuid().optional(),
    content: z.string().trim().min(1).max(20_000),
    context_type: z.string().trim().min(1).max(100),
    project_id: projectIdSchema,
    rationale: z.string().trim().max(2_000).optional(),
    source_object_ids: z.array(z.string().uuid()).max(100).optional(),
    title: z.string().trim().min(1).max(200),
  })
  .refine(
    (value) => Boolean(value.agent_session_id) === Boolean(value.agent_run_id),
    {
      message: "Agent Session and Run provenance must be provided together",
      path: ["agent_session_id"],
    },
  );

export const contextPromoteToolName = "context.promote";

export const contextPromoteTool: ToolModule = {
  name: contextPromoteToolName,
  register(server, context) {
    server.registerTool(
      contextPromoteToolName,
      {
        annotations: {
          destructiveHint: false,
          idempotentHint: false,
          readOnlyHint: false,
          title: "Propose project context",
        },
        description:
          "Submit an explicit conclusion as a pending Project Context proposal. A human collaborator must review it before it becomes formal context.",
        inputSchema,
        title: "Promote Context",
      },
      async (input): Promise<CallToolResult> => executePromote(context, input),
    );
  },
};

async function executePromote(
  context: ToolRegistrationContext,
  input: z.infer<typeof inputSchema>,
): Promise<CallToolResult> {
  const startedAt = context.now();
  try {
    context.authorizer.assertToolAccess(
      context.principal,
      contextPromoteToolName,
    );
    context.authorizer.assertProjectAccess(context.principal, input.project_id);
    if (!context.coreAccessToken) {
      throw new GatewayError(
        "CORE_CREDENTIAL_MISSING",
        "The delegated Core credential is unavailable",
        503,
      );
    }
    const proposalInput = {
      agent_run_id: input.agent_run_id,
      agent_session_id: input.agent_session_id,
      content: input.content,
      context_type: input.context_type,
      rationale: input.rationale,
      source_object_ids: input.source_object_ids,
      title: input.title,
    };
    const proposal = await context.coreClient.createContextProposal(
      input.project_id,
      proposalInput,
      {
        accessToken: context.coreAccessToken,
        projectId: input.project_id,
        requestId: context.requestId,
      },
    );
    await recordAudit(context, startedAt, input.project_id, "success");
    return {
      content: [{ text: JSON.stringify(proposal), type: "text" }],
      structuredContent: proposal as unknown as Record<string, unknown>,
    };
  } catch (error) {
    const safe = promoteError(error);
    await recordAudit(
      context,
      startedAt,
      input.project_id,
      safe.status === 403 ? "denied" : "error",
      safe.code,
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

function promoteError(error: unknown): GatewayError {
  if (!(error instanceof CoreClientError)) {
    return safeError(error);
  }
  if (error.status === 401) {
    return new GatewayError(
      "UNAUTHENTICATED",
      "The Agent credential is no longer valid",
      401,
    );
  }
  if (error.status === 403) {
    return new GatewayError(
      "CONTEXT_PROMOTE_DENIED",
      "The Agent cannot propose context for this project",
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
  if (error.status === 400 || error.status === 422) {
    return new GatewayError(
      "CONTEXT_PROPOSAL_INVALID",
      "The context proposal was rejected",
      error.status,
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
    "Core rejected the context proposal",
    error.status,
  );
}

async function recordAudit(
  context: ToolRegistrationContext,
  startedAt: number,
  projectId: string,
  outcome: "denied" | "error" | "success",
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
      projectId,
      requestId: context.requestId,
      sessionId: context.sessionId,
      toolName: contextPromoteToolName,
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
