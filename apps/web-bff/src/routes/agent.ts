import type { CoreClient } from "@mmdash/core-client";
import type { components } from "@mmdash/core-client/generated";
import type { FastifyInstance, FastifyRequest } from "fastify";
import { Readable } from "node:stream";
import { z } from "zod";

import { BffError } from "../errors/bff-error.js";

type AgentInstance = components["schemas"]["AgentInstance"];
type AgentInstanceList = components["schemas"]["AgentInstanceList"];
type AgentInstanceProvisioningResult =
  components["schemas"]["AgentInstanceProvisioningResult"];
type AgentChecksResult = components["schemas"]["AgentChecksResult"];
type AgentProjectAccessVerificationResult =
  components["schemas"]["AgentProjectAccessVerificationResult"];
type AgentTokenRotationResult =
  components["schemas"]["AgentTokenRotationResult"];
type AgentTokenVerificationResult =
  components["schemas"]["AgentTokenVerificationResult"];
type AgentTokenAbortResult = components["schemas"]["AgentTokenAbortResult"];
type AgentPrompt = components["schemas"]["AgentPrompt"];
type AgentSession = components["schemas"]["AgentSession"];
type AgentSessionList = components["schemas"]["AgentSessionList"];
type AgentMessageList = components["schemas"]["AgentMessageList"];
type AgentRun = components["schemas"]["AgentRun"];
type AgentRunLaunch = components["schemas"]["AgentRunLaunch"];

const idSchema = z.string().uuid();
const projectParamsSchema = z.object({ projectId: idSchema });
const instanceParamsSchema = projectParamsSchema.extend({
  agentInstanceId: idSchema,
});
const tokenParamsSchema = instanceParamsSchema.extend({ tokenId: idSchema });
const sessionParamsSchema = instanceParamsSchema.extend({
  sessionId: idSchema,
});
const runParamsSchema = sessionParamsSchema.extend({ runId: idSchema });
const approvalParamsSchema = runParamsSchema.extend({
  approvalId: z.string().trim().min(1).max(500),
});

const httpUrlSchema = z
  .string()
  .trim()
  .min(1)
  .max(2_048)
  .url()
  .refine((value) => {
    const protocol = new URL(value).protocol;
    return protocol === "http:" || protocol === "https:";
  }, "Only HTTP and HTTPS URLs are supported");
const displayNameSchema = z.string().trim().min(1).max(120);
const profileIdPattern = /^[a-z0-9][a-z0-9_-]{0,63}$/;
const reservedProfileNames = new Set(["hermes", "test", "tmp", "root", "sudo"]);
const profileSchema = z
  .string()
  .regex(profileIdPattern, "Invalid Hermes profile")
  .refine(
    (value) => !reservedProfileNames.has(value),
    "Reserved Hermes profile",
  );
const hermesApiKeySchema = z.string().min(16).max(4_096);
const dashboardSessionTokenSchema = z.string().min(16).max(4_096);
const cloudflareClientIdSchema = z.string().min(1).max(4_096);
const cloudflareClientSecretSchema = z.string().min(16).max(4_096);
const toolNameSchema = z.enum([
  "project.get",
  "data.list",
  "data.read",
  "context.promote",
  "progress.get",
  "progress.recalculate",
  "artifact.upload",
  "artifact.read",
  "experiment.create",
  "experiment.run",
  "experiment.status",
  "result.get",
]);
const allowedToolsSchema = z.array(toolNameSchema).min(1).max(12).superRefine(
  (tools, context) => {
    if (new Set(tools).size !== tools.length) {
      context.addIssue({
        code: "custom",
        message: "Allowed tools must be unique",
      });
    }
  });

const createInstanceSchema = z
  .object({
    adapter_type: z.literal("hermes").optional(),
    allowed_tools: allowedToolsSchema,
    cloudflare_access_client_id: cloudflareClientIdSchema.optional(),
    cloudflare_access_client_secret: cloudflareClientSecretSchema.optional(),
    dashboard_session_token: dashboardSessionTokenSchema.optional(),
    display_name: displayNameSchema,
    hermes_api_key: hermesApiKeySchema,
    management_mode: z.enum(["manual", "auto"]),
    management_url: httpUrlSchema.optional(),
    profile: profileSchema.optional(),
    request_timeout_seconds: z.number().int().min(1).max(300).optional(),
    runtime_url: httpUrlSchema,
  })
  .superRefine((value, context) => {
    if (value.management_mode === "auto") {
      if (!value.management_url) {
        context.addIssue({
          code: "custom",
          message: "Auto management requires a management URL",
          path: ["management_url"],
        });
      }
      if (!value.dashboard_session_token) {
        context.addIssue({
          code: "custom",
          message: "Auto management requires a Dashboard session token",
          path: ["dashboard_session_token"],
        });
      }
    } else if (
      value.dashboard_session_token ||
      value.cloudflare_access_client_id ||
      value.cloudflare_access_client_secret
    ) {
      context.addIssue({
        code: "custom",
        message: "Manual management must not submit Dashboard credentials",
        path: ["management_mode"],
      });
    }
    const hasCloudflareId = Boolean(value.cloudflare_access_client_id);
    const hasCloudflareSecret = Boolean(value.cloudflare_access_client_secret);
    if (hasCloudflareId !== hasCloudflareSecret) {
      context.addIssue({
        code: "custom",
        message:
          "Cloudflare Access client ID and secret must be provided together",
        path: ["cloudflare_access_client_id"],
      });
    }
  });

const updateInstanceSchema = z
  .object({
    allowed_tools: allowedToolsSchema.optional(),
    cloudflare_access_client_id: cloudflareClientIdSchema.optional(),
    cloudflare_access_client_secret: cloudflareClientSecretSchema.optional(),
    dashboard_session_token: dashboardSessionTokenSchema.optional(),
    display_name: displayNameSchema.optional(),
    hermes_api_key: hermesApiKeySchema.optional(),
    management_mode: z.enum(["manual", "auto"]).optional(),
    management_url: httpUrlSchema.optional(),
    profile: profileSchema.optional(),
    request_timeout_seconds: z.number().int().min(1).max(300).optional(),
    runtime_url: httpUrlSchema.optional(),
  })
  .refine((value) => Object.keys(value).length > 0, {
    message: "At least one Agent setting must be provided",
  })
  .superRefine((value, context) => {
    const hasCloudflareId = Boolean(value.cloudflare_access_client_id);
    const hasCloudflareSecret = Boolean(value.cloudflare_access_client_secret);
    if (hasCloudflareId !== hasCloudflareSecret) {
      context.addIssue({
        code: "custom",
        message:
          "Cloudflare Access client ID and secret must be provided together",
        path: ["cloudflare_access_client_id"],
      });
    }
    if (
      value.management_mode === "manual" &&
      (value.dashboard_session_token ||
        value.cloudflare_access_client_id ||
        value.cloudflare_access_client_secret)
    ) {
      context.addIssue({
        code: "custom",
        message: "Manual management must not submit Dashboard credentials",
        path: ["management_mode"],
      });
    }
  });

const checkSchema = z.object({
  scope: z.enum(["runtime", "management", "project_access", "all"]),
});
const rotateSchema = z.object({
  expires_at: z.string().datetime({ offset: true }).optional(),
  name: z.string().trim().min(1).max(120).optional(),
});
const updatePromptSchema = z.object({ content: z.string().min(1).max(50_000) });
const createSessionSchema = z.object({
  default: z.boolean().optional(),
  session_type: z.literal("main"),
  title: z.string().trim().min(1).max(255),
});
const updateSessionSchema = z.object({
  title: z.string().trim().min(1).max(255),
});
const endSessionSchema = z.object({
  reason: z.string().trim().max(500).optional(),
});
const forkSessionSchema = z.object({
  default: z.boolean().optional(),
  title: z.string().trim().min(1).max(255).optional(),
});
const startRunSchema = z.object({
  artifact_ids: z.array(z.string().uuid()).max(10).optional(),
  message: z.string().trim().min(1).max(100_000),
  reasoning_effort: z
    .enum(["none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"])
    .optional(),
});
const replayRunSchema = z.object({
  message_id: z.string().trim().min(1).max(500).optional(),
});
const approvalSchema = z.object({
  choice: z.enum(["once", "session", "always", "deny"]),
});
const lastEventIdSchema = z
  .string()
  .min(1)
  .max(512)
  .refine(
    (value) =>
      Array.from(value).every((character) => {
        const codePoint = character.codePointAt(0) ?? 0;
        return codePoint >= 32 && codePoint !== 127;
      }),
    "Event IDs must not contain control characters",
  );

export function registerAgentRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
): void {
  app.get(
    "/api/projects/:projectId/agent-instances",
    agentRoute,
    async (request) => {
      const { projectId } = projectParamsSchema.parse(request.params);
      return agentRequest<AgentInstanceList>(
        coreClient,
        `/v1/projects/${encodeURIComponent(projectId)}/agent-instances`,
        { method: "GET" },
        request,
      );
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances",
    agentRoute,
    async (request, reply) => {
      const { projectId } = projectParamsSchema.parse(request.params);
      const input = createInstanceSchema.parse(
        request.body,
      ) satisfies components["schemas"]["CreateAgentInstanceRequest"];
      const result = await agentRequest<AgentInstanceProvisioningResult>(
        coreClient,
        `/v1/projects/${encodeURIComponent(projectId)}/agent-instances`,
        { body: input, method: "POST" },
        request,
      );
      if (input.management_mode === "auto") {
        delete result.one_time_credential;
      }
      return reply.code(201).send(result);
    },
  );

  app.get(
    "/api/projects/:projectId/agent-instances/:agentInstanceId",
    agentRoute,
    async (request) => {
      const params = instanceParamsSchema.parse(request.params);
      return agentRequest<AgentInstance>(
        coreClient,
        instancePath(params),
        { method: "GET" },
        request,
      );
    },
  );

  app.patch(
    "/api/projects/:projectId/agent-instances/:agentInstanceId",
    agentRoute,
    async (request) => {
      const params = instanceParamsSchema.parse(request.params);
      const input = updateInstanceSchema.parse(
        request.body,
      ) satisfies components["schemas"]["UpdateAgentInstanceRequest"];
      const result = await agentRequest<AgentInstanceProvisioningResult>(
        coreClient,
        instancePath(params),
        { body: input, method: "PATCH" },
        request,
      );
      if (result.instance.management_mode === "auto") {
        delete result.one_time_credential;
      }
      return result;
    },
  );

  app.delete(
    "/api/projects/:projectId/agent-instances/:agentInstanceId",
    agentRoute,
    async (request, reply) => {
      const params = instanceParamsSchema.parse(request.params);
      await agentRequest<void>(
        coreClient,
        instancePath(params),
        { method: "DELETE" },
        request,
      );
      return reply.code(204).send();
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/checks",
    agentRoute,
    async (request) => {
      const params = instanceParamsSchema.parse(request.params);
      const input = checkSchema.parse(
        request.body,
      ) satisfies components["schemas"]["RunAgentChecksRequest"];
      return agentRequest<AgentChecksResult>(
        coreClient,
        `${instancePath(params)}/checks`,
        { body: input, method: "POST" },
        request,
      );
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/project-access/verify",
    agentRoute,
    async (request) => {
      const params = instanceParamsSchema.parse(request.params);
      return agentRequest<AgentProjectAccessVerificationResult>(
        coreClient,
        `${instancePath(params)}/project-access/verify`,
        { method: "POST" },
        request,
      );
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/tokens/rotate",
    agentRoute,
    async (request, reply) => {
      const params = instanceParamsSchema.parse(request.params);
      const input = rotateSchema.parse(request.body ?? {});
      const result = await agentRequest<AgentTokenRotationResult>(
        coreClient,
        `${instancePath(params)}/tokens/rotate`,
        { body: input, method: "POST" },
        request,
      );
      return reply.code(201).send(result);
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/tokens/:tokenId/verify",
    agentRoute,
    async (request) => {
      const params = tokenParamsSchema.parse(request.params);
      return agentRequest<AgentTokenVerificationResult>(
        coreClient,
        `${instancePath(params)}/tokens/${encodeURIComponent(params.tokenId)}/verify`,
        { method: "POST" },
        request,
      );
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/tokens/:tokenId/abort",
    agentRoute,
    async (request) => {
      const params = tokenParamsSchema.parse(request.params);
      return agentRequest<AgentTokenAbortResult>(
        coreClient,
        `${instancePath(params)}/tokens/${encodeURIComponent(params.tokenId)}/abort`,
        { method: "POST" },
        request,
      );
    },
  );

  app.delete(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/tokens/:tokenId",
    agentRoute,
    async (request, reply) => {
      const params = tokenParamsSchema.parse(request.params);
      await agentRequest<void>(
        coreClient,
        `${instancePath(params)}/tokens/${encodeURIComponent(params.tokenId)}`,
        { method: "DELETE" },
        request,
      );
      return reply.code(204).send();
    },
  );

  app.get(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/prompt",
    agentRoute,
    async (request) => {
      const params = instanceParamsSchema.parse(request.params);
      return agentRequest<AgentPrompt>(
        coreClient,
        `${instancePath(params)}/prompt`,
        { method: "GET" },
        request,
      );
    },
  );

  app.patch(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/prompt",
    agentRoute,
    async (request) => {
      const params = instanceParamsSchema.parse(request.params);
      const input = updatePromptSchema.parse(
        request.body,
      ) satisfies components["schemas"]["UpdateAgentPromptRequest"];
      return agentRequest<AgentPrompt>(
        coreClient,
        `${instancePath(params)}/prompt`,
        { body: input, method: "PATCH" },
        request,
      );
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/prompt/reset",
    agentRoute,
    async (request) => {
      const params = instanceParamsSchema.parse(request.params);
      return agentRequest<AgentPrompt>(
        coreClient,
        `${instancePath(params)}/prompt/reset`,
        { method: "POST" },
        request,
      );
    },
  );

  app.get(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/sessions",
    agentRoute,
    async (request) => {
      const params = instanceParamsSchema.parse(request.params);
      return agentRequest<AgentSessionList>(
        coreClient,
        `${instancePath(params)}/sessions`,
        { method: "GET" },
        request,
      );
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/sessions",
    agentRoute,
    async (request, reply) => {
      const params = instanceParamsSchema.parse(request.params);
      const input = createSessionSchema.parse(
        request.body,
      ) satisfies components["schemas"]["CreateAgentSessionRequest"];
      const result = await agentRequest<AgentSession>(
        coreClient,
        `${instancePath(params)}/sessions`,
        { body: input, method: "POST" },
        request,
      );
      return reply.code(201).send(result);
    },
  );

  app.get(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/sessions/:sessionId",
    agentRoute,
    async (request) => {
      const params = sessionParamsSchema.parse(request.params);
      return agentRequest<AgentSession>(
        coreClient,
        sessionPath(params),
        { method: "GET" },
        request,
      );
    },
  );

  app.patch(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/sessions/:sessionId",
    agentRoute,
    async (request) => {
      const params = sessionParamsSchema.parse(request.params);
      const input = updateSessionSchema.parse(
        request.body,
      ) satisfies components["schemas"]["UpdateAgentSessionRequest"];
      return agentRequest<AgentSession>(
        coreClient,
        sessionPath(params),
        { body: input, method: "PATCH" },
        request,
      );
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/sessions/:sessionId/end",
    agentRoute,
    async (request) => {
      const params = sessionParamsSchema.parse(request.params);
      const input = endSessionSchema.parse(request.body ?? {});
      return agentRequest<AgentSession>(
        coreClient,
        `${sessionPath(params)}/end`,
        { body: input, method: "POST" },
        request,
      );
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/sessions/:sessionId/continue",
    agentRoute,
    async (request) => {
      const params = sessionParamsSchema.parse(request.params);
      return agentRequest<AgentSession>(
        coreClient,
        `${sessionPath(params)}/continue`,
        { method: "POST" },
        request,
      );
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/sessions/:sessionId/fork",
    agentRoute,
    async (request, reply) => {
      const params = sessionParamsSchema.parse(request.params);
      const input = forkSessionSchema.parse(request.body ?? {});
      const result = await agentRequest<AgentSession>(
        coreClient,
        `${sessionPath(params)}/fork`,
        { body: input, method: "POST" },
        request,
      );
      return reply.code(201).send(result);
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/sessions/:sessionId/default",
    agentRoute,
    async (request) => {
      const params = sessionParamsSchema.parse(request.params);
      return agentRequest<AgentSession>(
        coreClient,
        `${sessionPath(params)}/default`,
        { method: "POST" },
        request,
      );
    },
  );

  app.get(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/sessions/:sessionId/messages",
    agentRoute,
    async (request) => {
      const params = sessionParamsSchema.parse(request.params);
      return agentRequest<AgentMessageList>(
        coreClient,
        `${sessionPath(params)}/messages`,
        { method: "GET" },
        request,
      );
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/sessions/:sessionId/runs",
    agentRoute,
    async (request, reply) => {
      const params = sessionParamsSchema.parse(request.params);
      const input = startRunSchema.parse(
        request.body,
      ) satisfies components["schemas"]["StartAgentRunRequest"];
      const result = await agentRequest<AgentRunLaunch>(
        coreClient,
        `${sessionPath(params)}/runs`,
        { body: input, method: "POST" },
        request,
      );
      return reply.code(202).send(result);
    },
  );

  app.get(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/sessions/:sessionId/runs/:runId",
    agentRoute,
    async (request) => {
      const params = runParamsSchema.parse(request.params);
      return agentRequest<AgentRun>(
        coreClient,
        runPath(params),
        { method: "GET" },
        request,
      );
    },
  );

  app.get(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/sessions/:sessionId/runs/:runId/events",
    agentRoute,
    async (request, reply) => {
      const params = runParamsSchema.parse(request.params);
      const lastEventId = readHeader(request, "last-event-id");
      if (lastEventId) {
        lastEventIdSchema.parse(lastEventId);
      }

      const abortController = new AbortController();
      const abort = () => abortController.abort();
      request.raw.once("aborted", abort);
      reply.raw.once("close", abort);

      let response: Response;
      try {
        const headers = new Headers({ accept: "text/event-stream" });
        if (lastEventId) {
          headers.set("last-event-id", lastEventId);
        }
        response = await coreClient.fetch(
          `${runPath(params)}/events`,
          {
            headers,
            method: "GET",
            signal: abortController.signal,
          },
          coreContext(request),
        );
      } catch (error) {
        request.raw.off("aborted", abort);
        reply.raw.off("close", abort);
        if (abortController.signal.aborted) {
          return reply.code(499).send();
        }
        throw error;
      }

      if (!response.ok) {
        request.raw.off("aborted", abort);
        reply.raw.off("close", abort);
        throw new BffError({
          code: "AGENT_STREAM_UNAVAILABLE",
          message: "The Agent event stream is unavailable",
          status: response.status >= 500 ? 502 : response.status,
        });
      }

      reply.header("cache-control", "no-cache, no-transform");
      reply.header("content-type", "text/event-stream; charset=utf-8");
      reply.header("x-accel-buffering", "no");
      if (!response.body) {
        request.raw.off("aborted", abort);
        reply.raw.off("close", abort);
        return reply.send();
      }

      const stream = Readable.from(
        response.body as unknown as AsyncIterable<Uint8Array>,
      );
      stream.once("close", () => {
        request.raw.off("aborted", abort);
        reply.raw.off("close", abort);
      });
      return reply.send(stream);
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/sessions/:sessionId/runs/:runId/approvals/:approvalId",
    agentRoute,
    async (request) => {
      const params = approvalParamsSchema.parse(request.params);
      const input = approvalSchema.parse(
        request.body,
      ) satisfies components["schemas"]["RespondAgentRunApprovalRequest"];
      return agentRequest<AgentRun>(
        coreClient,
        `${runPath(params)}/approvals/${encodeURIComponent(params.approvalId)}`,
        { body: input, method: "POST" },
        request,
      );
    },
  );

  app.post(
    "/api/projects/:projectId/agent-instances/:agentInstanceId/sessions/:sessionId/runs/:runId/stop",
    agentRoute,
    async (request) => {
      const params = runParamsSchema.parse(request.params);
      return agentRequest<AgentRun>(
        coreClient,
        `${runPath(params)}/stop`,
        { method: "POST" },
        request,
      );
    },
  );

  for (const action of ["regenerate", "rerun"] as const) {
    app.post(
      `/api/projects/:projectId/agent-instances/:agentInstanceId/sessions/:sessionId/runs/:runId/${action}`,
      agentRoute,
      async (request, reply) => {
        const params = runParamsSchema.parse(request.params);
        const input = replayRunSchema.parse(request.body ?? {});
        const result = await agentRequest<AgentRunLaunch>(
          coreClient,
          `${runPath(params)}/${action}`,
          { body: input, method: "POST" },
          request,
        );
        return reply.code(202).send(result);
      },
    );
  }
}

const agentRoute = {
  config: { auth: "required", project: "required" },
} as const;

function instancePath(params: z.output<typeof instanceParamsSchema>): string {
  return `/v1/projects/${encodeURIComponent(params.projectId)}/agent-instances/${encodeURIComponent(params.agentInstanceId)}`;
}

function sessionPath(params: z.output<typeof sessionParamsSchema>): string {
  return `${instancePath(params)}/sessions/${encodeURIComponent(params.sessionId)}`;
}

function runPath(params: z.output<typeof runParamsSchema>): string {
  return `${sessionPath(params)}/runs/${encodeURIComponent(params.runId)}`;
}

async function agentRequest<T>(
  coreClient: CoreClient,
  path: string,
  options: Parameters<CoreClient["request"]>[1],
  request: FastifyRequest,
): Promise<T> {
  const result = await coreClient.request<T>(
    path,
    options,
    coreContext(request),
  );
  return redactUnexpectedSecrets(result);
}

function coreContext(request: FastifyRequest) {
  return {
    accessToken: request.browserIdentity!.accessToken,
    projectId: request.currentProjectId!,
    requestId: request.id,
    userId: request.browserIdentity!.userId,
  };
}

function readHeader(request: FastifyRequest, name: string): string | undefined {
  const value = request.headers[name];
  return typeof value === "string" ? value : undefined;
}

const forbiddenResponseKeys = new Set([
  "access_token",
  "api_key",
  "authorization",
  "cloudflare_access_client_id",
  "cloudflare_access_client_secret",
  "client_secret",
  "dashboard_session_token",
  "dashboard_token",
  "hermes_api_key",
  "refresh_token",
  "token",
  "token_hash",
]);

function redactUnexpectedSecrets<T>(value: T, parentKey?: string): T {
  if (Array.isArray(value)) {
    return value.map((item) => redactUnexpectedSecrets(item, parentKey)) as T;
  }
  if (typeof value !== "object" || value === null) {
    return value;
  }

  const output: Record<string, unknown> = {};
  for (const [key, item] of Object.entries(value)) {
    const oneTimeAgentToken =
      parentKey === "one_time_credential" && key === "token";
    if (!oneTimeAgentToken && forbiddenResponseKeys.has(key.toLowerCase())) {
      continue;
    }
    output[key] = redactUnexpectedSecrets(item, key);
  }
  return output as T;
}
