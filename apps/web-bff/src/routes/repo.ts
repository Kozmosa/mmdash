import type { CoreClient } from "@mmdash/core-client";
import type { components } from "@mmdash/core-client/generated";
import type { FastifyInstance } from "fastify";
import { z } from "zod";

const workspaceSchema = z.enum(["code", "article", "result"]);
const fullShaSchema = z.string().regex(/^[0-9a-f]{40}([0-9a-f]{24})?$/);
const branchSchema = z.string().trim().min(1).max(255);
const connectSchema = z.object({
  replace_disconnected: z.boolean().optional(),
  settings_version: z.number().int().min(1),
});
const mappingsSchema = z.object({
  article_branch: branchSchema,
  code_branch: branchSchema,
  result_branch: branchSchema,
});
const commitQuerySchema = z.object({
  cursor: z.string().max(4_096).optional(),
  limit: z.coerce.number().int().min(1).max(100).optional(),
  workspace: workspaceSchema,
});
const treeQuerySchema = z.object({
  cursor: z.string().max(4_096).optional(),
  limit: z.coerce.number().int().min(1).max(200).optional(),
  path: z.string().max(4_096).optional(),
  revision: fullShaSchema,
  workspace: workspaceSchema,
});
const contentQuerySchema = z.object({
  path: z.string().min(1).max(4_096),
  revision: fullShaSchema,
  workspace: workspaceSchema,
});

export function registerRepoRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
): void {
  app.get(
    "/api/projects/:projectId/repository",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.getRepository(request.currentProjectId!, coreContext(request)),
  );

  app.get(
    "/api/projects/:projectId/repository/capabilities",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.getRepositoryCapabilities(
        request.currentProjectId!,
        coreContext(request),
      ),
  );

  app.put(
    "/api/projects/:projectId/repository",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const repository = await coreClient.connectRepository(
        request.currentProjectId!,
        connectSchema.parse(
          request.body,
        ) satisfies components["schemas"]["RepoConnectRequest"],
        coreContext(request),
      );
      return reply.code(202).send(repository);
    },
  );

  app.delete(
    "/api/projects/:projectId/repository",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      await coreClient.disconnectRepository(
        request.currentProjectId!,
        coreContext(request),
      );
      return reply.code(204).send();
    },
  );

  app.post(
    "/api/projects/:projectId/repository/test",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.testRepositoryConnection(
        request.currentProjectId!,
        coreContext(request),
      ),
  );

  app.post(
    "/api/projects/:projectId/repository/sync",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const repository = await coreClient.requestRepositorySync(
        request.currentProjectId!,
        coreContext(request),
      );
      return reply.code(202).send(repository);
    },
  );

  app.post(
    "/api/projects/:projectId/repository/webhook-secret",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.rotateRepositoryWebhookSecret(
        request.currentProjectId!,
        coreContext(request),
      ),
  );

  app.patch(
    "/api/projects/:projectId/repository/workspaces",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const repository = await coreClient.updateRepositoryWorkspaces(
        request.currentProjectId!,
        mappingsSchema.parse(
          request.body,
        ) satisfies components["schemas"]["RepoUpdateWorkspacesRequest"],
        coreContext(request),
      );
      return reply.code(202).send(repository);
    },
  );

  app.get(
    "/api/projects/:projectId/repository/branches",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.listRepositoryBranches(
        request.currentProjectId!,
        coreContext(request),
      ),
  );

  app.get(
    "/api/projects/:projectId/repository/commits",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const query = commitQuerySchema.parse(request.query);
      return coreClient.listRepositoryCommits(
        request.currentProjectId!,
        query.workspace,
        coreContext(request),
        { cursor: query.cursor, limit: query.limit },
      );
    },
  );

  app.get(
    "/api/projects/:projectId/repository/commits/:commitSha",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { commitSha } = z
        .object({ commitSha: fullShaSchema })
        .parse(request.params);
      return coreClient.getRepositoryCommit(
        request.currentProjectId!,
        commitSha,
        coreContext(request),
      );
    },
  );

  app.get(
    "/api/projects/:projectId/repository/tree",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const query = treeQuerySchema.parse(request.query);
      return coreClient.listRepositoryTree(
        request.currentProjectId!,
        query,
        coreContext(request),
      );
    },
  );

  app.get(
    "/api/projects/:projectId/repository/content",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const query = contentQuerySchema.parse(request.query);
      return coreClient.getRepositoryContent(
        request.currentProjectId!,
        query,
        coreContext(request),
      );
    },
  );

  app.get(
    "/api/projects/:projectId/repository/raw",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const query = contentQuerySchema.parse(request.query);
      const params = new URLSearchParams({
        path: query.path,
        revision: query.revision,
        workspace: query.workspace,
      });
      const response = await coreClient.fetch(
        `/v1/projects/${encodeURIComponent(request.currentProjectId!)}/repository/raw?${params.toString()}`,
        { headers: { accept: "*/*" }, method: "GET" },
        coreContext(request),
      );
      const body = Buffer.from(await response.arrayBuffer());
      const contentType = response.headers.get("content-type");
      if (contentType) reply.header("content-type", contentType);
      const contentLength = response.headers.get("content-length");
      if (contentLength) reply.header("content-length", contentLength);
      const cacheControl = response.headers.get("cache-control");
      if (cacheControl) reply.header("cache-control", cacheControl);
      const contentSecurityPolicy = response.headers.get(
        "content-security-policy",
      );
      if (contentSecurityPolicy) {
        reply.header("content-security-policy", contentSecurityPolicy);
      }
      const contentDisposition = response.headers.get("content-disposition");
      if (contentDisposition) {
        reply.header("content-disposition", contentDisposition);
      }
      const contentTypeOptions = response.headers.get("x-content-type-options");
      if (contentTypeOptions) {
        reply.header("x-content-type-options", contentTypeOptions);
      }
      return reply.code(response.status).send(body);
    },
  );
}

function coreContext(request: {
  browserIdentity?: {
    accessToken: string;
    userId: string;
  };
  currentProjectId?: string;
  id: string;
}) {
  return {
    accessToken: request.browserIdentity!.accessToken,
    projectId: request.currentProjectId!,
    requestId: request.id,
    userId: request.browserIdentity!.userId,
  };
}
