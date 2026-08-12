import { CoreClientError, type CoreRequestContext } from "@mmdash/core-client";
import type { CallToolResult } from "@modelcontextprotocol/server";
import { z } from "zod/v4";

import { GatewayError, safeError } from "../errors/gateway-error.js";
import type { ToolModule, ToolRegistrationContext } from "./registry.js";

const projectId = z.string().uuid();
const experimentId = z.string().uuid();
const limits = z.object({
  cpu_millis: z.number().int().positive(),
  memory_bytes: z.number().int().positive(),
  timeout_seconds: z.number().int().positive(),
  disk_bytes: z.number().int().positive(),
  pids: z.number().int().positive(),
  network: z.enum(["disabled", "restricted", "enabled"]),
});

export const experimentCreateTool: ToolModule = {
  name: "experiment.create",
  register(server, context) {
    server.registerTool(
      this.name,
      {
        annotations: { destructiveHint: false, idempotentHint: true, readOnlyHint: false, title: "Create experiment" },
        description: "Create an experiment with a fixed commit, entrypoint, parameters, runtime, and resource limits.",
        inputSchema: z.object({
          project_id: projectId,
          name: z.string().trim().min(1).max(200),
          source_commit: z.string().regex(/^[0-9a-f]{40}([0-9a-f]{24})?$/),
          entrypoint: z.string().regex(/^(python3?|node|go|binary):[a-zA-Z0-9_./-]+$/).refine((value) => !value.split(":", 2)[1]!.split("/").some((part) => part === "." || part === ".."), "entrypoint path must be normalized"),
          parameters: z.record(z.string(), z.unknown()),
          environment: z.record(z.string(), z.string()),
          inputs: z.record(z.string(), z.unknown()),
          runtime: z.enum(["local-docker", "e2b"]),
          limits,
          idempotency_key: z.string().trim().min(1).max(200),
          max_attempts: z.number().int().min(1).max(5).default(1),
        }),
      },
      async (input) => execute(context, this.name, input.project_id, (requestContext) =>
        context.coreClient.createExperiment(input.project_id, {
          name: input.name, source_commit: input.source_commit, entrypoint: input.entrypoint,
          parameters: input.parameters, environment: input.environment, inputs: input.inputs,
          runtime: input.runtime, limits: input.limits, idempotency_key: input.idempotency_key, max_attempts: input.max_attempts,
        }, requestContext)),
    );
  },
};

export const experimentRunTool: ToolModule = {
  name: "experiment.run",
  register(server, context) {
    server.registerTool(
      this.name,
      {
        annotations: { destructiveHint: false, idempotentHint: true, readOnlyHint: false, title: "Run experiment" },
        description: "Queue one frozen experiment exactly once.",
        inputSchema: z.object({ project_id: projectId, experiment_id: experimentId }),
      },
      async ({ project_id, experiment_id }) => execute(context, this.name, project_id, (requestContext) =>
        context.coreClient.runExperiment(project_id, experiment_id, requestContext)),
    );
  },
};

export const experimentStatusTool: ToolModule = {
  name: "experiment.status",
  register(server, context) {
    server.registerTool(
      this.name,
      {
        annotations: { destructiveHint: false, idempotentHint: true, readOnlyHint: true, title: "Read experiment status" },
        description: "Read the authoritative experiment lifecycle state, resource usage, and result pointer.",
        inputSchema: z.object({ project_id: projectId, experiment_id: experimentId }),
      },
      async ({ project_id, experiment_id }) => execute(context, this.name, project_id, (requestContext) =>
        context.coreClient.getExperiment(project_id, experiment_id, requestContext)),
    );
  },
};

export const resultGetTool: ToolModule = {
  name: "result.get",
  register(server, context) {
    server.registerTool(
      this.name,
      {
        annotations: { destructiveHint: false, idempotentHint: true, readOnlyHint: true, title: "Read experiment result" },
        description: "Read the manifest summary and Artifact pointer for one completed experiment.",
        inputSchema: z.object({ project_id: projectId, experiment_id: experimentId }),
      },
      async ({ project_id, experiment_id }) => execute(context, this.name, project_id, (requestContext) =>
        context.coreClient.getExperimentResult(project_id, experiment_id, requestContext)),
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
    if (!context.coreAccessToken) throw new GatewayError("CORE_ACCESS_TOKEN_REQUIRED", "The MCP Core access token is not configured", 503);
    const result = await operation({ accessToken: context.coreAccessToken, projectId: projectIdValue, requestId: context.requestId });
    await recordAudit(context, toolName, projectIdValue, startedAt, "success");
    return { content: [{ text: JSON.stringify(result), type: "text" }], structuredContent: result as Record<string, unknown> };
  } catch (error) {
    const safe = mapError(error);
    await recordAudit(context, toolName, projectIdValue, startedAt, safe.status === 403 ? "denied" : "error", safe.code, false);
    return { content: [{ text: JSON.stringify({ code: safe.code, message: safe.message, request_id: context.requestId }), type: "text" }], isError: true };
  }
}

function mapError(error: unknown): GatewayError {
  if (!(error instanceof CoreClientError)) return safeError(error);
  if (error.status === 401) return new GatewayError("UNAUTHENTICATED", "The Agent credential is no longer valid", 401);
  if (error.status === 403) return new GatewayError("EXPERIMENT_ACCESS_DENIED", "The caller cannot access this experiment", 403);
  if (error.status === 404) return new GatewayError("EXPERIMENT_NOT_FOUND", "The experiment or result was not found", 404);
  if (error.status === 409) return new GatewayError("EXPERIMENT_CONFLICT", "The experiment state conflicts with this operation", 409);
  if (error.status >= 500) return new GatewayError("CORE_UNAVAILABLE", "Core is temporarily unavailable", 503);
  return new GatewayError("CORE_REQUEST_FAILED", "Core rejected the experiment request", error.status);
}

async function recordAudit(context: ToolRegistrationContext, toolName: string, projectIdValue: string, startedAt: number, outcome: "success" | "denied" | "error", errorCode?: string, required = true): Promise<void> {
  try { await context.audit.record({ actorId: context.principal.id, actorKind: context.principal.kind, durationMs: context.now() - startedAt, errorCode, occurredAt: new Date(context.now()).toISOString(), outcome, projectId: projectIdValue, requestId: context.requestId, sessionId: context.sessionId, toolName }); }
  catch { if (required) throw new GatewayError("AUDIT_UNAVAILABLE", "The tool audit service is unavailable", 503); }
}
