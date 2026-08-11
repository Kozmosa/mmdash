import { CoreClientError, type CoreRequestContext } from "@mmdash/core-client";
import type { CallToolResult } from "@modelcontextprotocol/server";
import { z } from "zod/v4";

import { GatewayError, safeError } from "../errors/gateway-error.js";
import type { ToolModule, ToolRegistrationContext } from "./registry.js";

const projectIdSchema = z.string().uuid();
const uploadIdSchema = z.string().uuid();
const agentSessionIdSchema = z.string().uuid();
const agentRunIdSchema = z.string().uuid();
const partNumbersSchema = z
  .array(z.number().int().min(1).max(10_000))
  .min(1)
  .max(100)
  .refine((values) => new Set(values).size === values.length, {
    message: "Part numbers must be unique",
  });

const inputSchema = z
  .object({
    action: z.enum(["begin", "parts", "complete", "abort"]),
    agent_session_id: agentSessionIdSchema.optional(),
    agent_run_id: agentRunIdSchema.optional(),
    description: z.string().max(4_000).optional(),
    filename: z.string().trim().min(1).max(255).optional(),
    idempotency_key: z.string().trim().min(1).max(200).optional(),
    mime_type: z.string().trim().min(1).max(255).optional(),
    name: z.string().trim().min(1).max(255).optional(),
    part_numbers: partNumbersSchema.optional(),
    parts: z
      .array(
        z.object({
          etag: z.string().trim().min(1).max(2_048),
          part_number: z.number().int().min(1).max(10_000),
        }),
      )
      .min(1)
      .max(10_000)
      .optional(),
    project_id: projectIdSchema,
    sha256: z
      .string()
      .regex(/^[0-9a-f]{64}$/)
      .optional(),
    size_bytes: z.number().int().min(0).max(Number.MAX_SAFE_INTEGER).optional(),
    tags: z.array(z.string().trim().min(1).max(64)).max(32).optional(),
    upload_id: uploadIdSchema.optional(),
  })
  .superRefine((value, issueContext) => {
    if (Boolean(value.agent_session_id) !== Boolean(value.agent_run_id)) {
      issueContext.addIssue({
        code: "custom",
        message: "agent_session_id and agent_run_id must be provided together",
        path: ["agent_session_id"],
      });
    }
    if (value.action === "begin") {
      for (const field of [
        "filename",
        "idempotency_key",
        "sha256",
        "size_bytes",
      ] as const) {
        if (value[field] === undefined) {
          issueContext.addIssue({
            code: "custom",
            message: `${field} is required for begin`,
            path: [field],
          });
        }
      }
      return;
    }
    if (!value.upload_id) {
      issueContext.addIssue({
        code: "custom",
        message: "upload_id is required for this action",
        path: ["upload_id"],
      });
    }
    if (value.action === "parts" && !value.part_numbers) {
      issueContext.addIssue({
        code: "custom",
        message: "part_numbers is required for parts",
        path: ["part_numbers"],
      });
    }
    if (value.action === "complete" && !value.parts) {
      issueContext.addIssue({
        code: "custom",
        message: "parts is required for complete",
        path: ["parts"],
      });
    }
  });

type ArtifactUploadInput = z.infer<typeof inputSchema>;

export const artifactUploadToolName = "artifact.upload";

export const artifactUploadTool: ToolModule = {
  name: artifactUploadToolName,
  register(server, context) {
    server.registerTool(
      artifactUploadToolName,
      {
        annotations: {
          destructiveHint: false,
          idempotentHint: false,
          readOnlyHint: false,
          title: "Upload an Agent Artifact",
        },
        description:
          "Upload a local image or file as an immutable mmdash Artifact with kind/source=agent and show it in the current mmdash chat. Proactively use this whenever a useful result is naturally a file or image; do not wait for the user to ask you to upload it. Call action=begin with the current agent_session_id and agent_run_id from the Run instructions, filename, exact byte size, lowercase SHA-256, and a stable idempotency_key. PUT each exact byte range directly to the returned short-lived URL using its exact headers, retain each response ETag, request later batches with action=parts when needed, then call action=complete with every part_number and ETag. Never put file bytes or base64 in this MCP call. Use action=abort to cancel an unfinished upload.",
        inputSchema,
        title: "Upload Agent Artifact",
      },
      async (input): Promise<CallToolResult> =>
        executeArtifactUpload(context, input),
    );
  },
};

async function executeArtifactUpload(
  context: ToolRegistrationContext,
  input: ArtifactUploadInput,
): Promise<CallToolResult> {
  const startedAt = context.now();
  try {
    context.authorizer.assertToolAccess(
      context.principal,
      artifactUploadToolName,
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
    const result = await performAction(context, input, requestContext);
    await recordAudit(context, startedAt, input.project_id, "success");
    return {
      content: [{ text: JSON.stringify(result), type: "text" }],
      structuredContent: result,
    };
  } catch (error) {
    const safe = artifactUploadError(error);
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

async function performAction(
  context: ToolRegistrationContext,
  input: ArtifactUploadInput,
  requestContext: CoreRequestContext,
): Promise<Record<string, unknown>> {
  switch (input.action) {
    case "begin": {
      const upload = await context.coreClient.initializeAgentArtifactUpload(
        input.project_id,
        {
          agent_session_id: input.agent_session_id,
          agent_run_id: input.agent_run_id,
          description: input.description,
          filename: input.filename!,
          idempotency_key: input.idempotency_key!,
          mime_type: input.mime_type,
          name: input.name,
          sha256: input.sha256!,
          size_bytes: input.size_bytes!,
          tags: input.tags,
        },
        requestContext,
      );
      if (upload.upload_mode === "deduplicated") {
        return {
          instructions:
            "The verified blob already existed; no transfer is required.",
          part_grants: [],
          upload,
        };
      }
      if (upload.transfer_mode !== "direct") {
        await bestEffortAbort(
          context,
          input.project_id,
          upload.upload_id,
          requestContext,
        );
        throw directTransferRequired();
      }
      const partNumbers =
        input.part_numbers ??
        Array.from(
          { length: Math.min(upload.part_count, 100) },
          (_, index) => index + 1,
        );
      const grants = await context.coreClient.signArtifactUploadParts(
        input.project_id,
        upload.upload_id,
        { part_numbers: partNumbers },
        requestContext,
      );
      assertDirectGrants(grants.items);
      return {
        instructions:
          "PUT each exact part to its transfer URL with the exact headers, retain each response ETag, request additional batches when part_count exceeds the returned grants, then call complete with every part and ETag.",
        part_grants: grants.items,
        upload,
      };
    }
    case "parts": {
      const upload = await context.coreClient.getArtifactUpload(
        input.project_id,
        input.upload_id!,
        requestContext,
      );
      if (upload.transfer_mode !== "direct") {
        throw directTransferRequired();
      }
      const grants = await context.coreClient.signArtifactUploadParts(
        input.project_id,
        input.upload_id!,
        { part_numbers: input.part_numbers! },
        requestContext,
      );
      assertDirectGrants(grants.items);
      return {
        instructions:
          "PUT each exact part with the returned method and headers; retain each response ETag.",
        part_grants: grants.items,
        upload_id: input.upload_id,
      };
    }
    case "complete": {
      const artifact = await context.coreClient.confirmArtifactUpload(
        input.project_id,
        input.upload_id!,
        { parts: input.parts! },
        requestContext,
      );
      return { artifact, upload_id: input.upload_id };
    }
    case "abort":
      await context.coreClient.abortArtifactUpload(
        input.project_id,
        input.upload_id!,
        requestContext,
      );
      return { status: "aborted", upload_id: input.upload_id };
  }
}

function assertDirectGrants(
  grants: Array<{ transfer: { method: string; url: string } }>,
): void {
  if (
    grants.some(
      ({ transfer }) =>
        transfer.method !== "PUT" || !/^https?:\/\//u.test(transfer.url),
    )
  ) {
    throw directTransferRequired();
  }
}

function directTransferRequired(): GatewayError {
  return new GatewayError(
    "ARTIFACT_DIRECT_TRANSFER_REQUIRED",
    "This deployment cannot issue a direct object-storage upload grant reachable by Hermes",
    503,
  );
}

async function bestEffortAbort(
  context: ToolRegistrationContext,
  projectId: string,
  uploadId: string,
  requestContext: CoreRequestContext,
): Promise<void> {
  try {
    await context.coreClient.abortArtifactUpload(
      projectId,
      uploadId,
      requestContext,
    );
  } catch {
    // Core expiry cleanup remains authoritative if cancellation is unavailable.
  }
}

function artifactUploadError(error: unknown): GatewayError {
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
      "ARTIFACT_UPLOAD_DENIED",
      "The Agent cannot upload an Artifact for this project",
      403,
    );
  }
  if (error.status === 404) {
    return new GatewayError(
      "ARTIFACT_UPLOAD_NOT_FOUND",
      "The Agent Artifact upload was not found",
      404,
    );
  }
  if (error.status === 400 || error.status === 409 || error.status === 422) {
    return new GatewayError(
      "ARTIFACT_UPLOAD_INVALID",
      "Core rejected the Agent Artifact upload state",
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
    "Core rejected the Agent Artifact request",
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
      toolName: artifactUploadToolName,
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
