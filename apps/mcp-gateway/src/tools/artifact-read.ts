import { CoreClientError, type CoreRequestContext } from "@mmdash/core-client";
import type { CallToolResult } from "@modelcontextprotocol/server";
import { z } from "zod/v4";

import { GatewayError, safeError } from "../errors/gateway-error.js";
import type { ToolModule, ToolRegistrationContext } from "./registry.js";

const inputSchema = z.object({
  artifact_id: z.string().uuid(),
  project_id: z.string().uuid(),
  version_id: z.string().uuid().optional(),
});

export const artifactReadToolName = "artifact.read";

export const artifactReadTool: ToolModule = {
  name: artifactReadToolName,
  register(server, context) {
    server.registerTool(
      artifactReadToolName,
      {
        annotations: {
          destructiveHint: false,
          idempotentHint: true,
          readOnlyHint: true,
          title: "Read an attached Artifact",
        },
        description:
          "Get a short-lived authorized download grant for an mmdash Artifact, including a file attached by the user to the current Run. Download the bytes using the exact returned method and headers. Never repeat the signed URL to the user.",
        inputSchema,
        title: "Read Artifact",
      },
      async (input): Promise<CallToolResult> => execute(context, input),
    );
  },
};

async function execute(
  context: ToolRegistrationContext,
  input: z.infer<typeof inputSchema>,
): Promise<CallToolResult> {
  const startedAt = context.now();
  try {
    context.authorizer.assertToolAccess(
      context.principal,
      artifactReadToolName,
    );
    context.authorizer.assertProjectAccess(context.principal, input.project_id);
    if (!context.coreAccessToken) {
      throw new GatewayError(
        "CORE_CREDENTIAL_MISSING",
        "The delegated Core credential is unavailable",
        503,
      );
    }
    const requestContext: CoreRequestContext = {
      accessToken: context.coreAccessToken,
      projectId: input.project_id,
      requestId: context.requestId,
    };
    const grant = await context.coreClient.downloadArtifact(
      input.project_id,
      input.artifact_id,
      requestContext,
      input.version_id,
    );
    await recordAudit(context, startedAt, input.project_id, "success");
    return {
      content: [{ text: JSON.stringify(grant), type: "text" }],
      structuredContent: grant,
    };
  } catch (error) {
    const normalized = normalizeError(error);
    await recordAudit(
      context,
      startedAt,
      input.project_id,
      normalized.status === 403 ? "denied" : "error",
      normalized.code,
      false,
    );
    return {
      content: [
        {
          text: JSON.stringify({
            code: normalized.code,
            message: normalized.message,
            request_id: context.requestId,
          }),
          type: "text",
        },
      ],
      isError: true,
    };
  }
}

function normalizeError(error: unknown): GatewayError {
  if (!(error instanceof CoreClientError)) return safeError(error);
  if (error.status === 401) {
    return new GatewayError(
      "UNAUTHENTICATED",
      "The Agent credential is no longer valid",
      401,
    );
  }
  if (error.status === 403) {
    return new GatewayError(
      "ARTIFACT_READ_DENIED",
      "The Agent cannot read this Artifact",
      403,
    );
  }
  if (error.status === 404) {
    return new GatewayError(
      "ARTIFACT_NOT_FOUND",
      "The Artifact was not found",
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
    "Core rejected the Artifact request",
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
      toolName: artifactReadToolName,
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
