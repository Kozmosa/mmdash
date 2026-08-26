import type { CoreClient } from "@mmdash/core-client";
import type { components } from "@mmdash/core-client/generated";
import type { FastifyInstance, FastifyRequest } from "fastify";
import { z } from "zod";

const artifactKindSchema = z.enum([
  "problem",
  "attachment",
  "experiment_result",
  "model_file",
  "article_build",
  "agent",
  "other",
]);
const publicArtifactKindSchema = z.enum(["problem", "attachment", "other"]);
const artifactSourceSchema = z.enum([
  "user_upload",
  "experiment",
  "model",
  "article",
  "agent",
  "system",
]);
const artifactStatusSchema = z.enum(["pending_upload", "available", "trashed"]);
const idSchema = z.string().uuid();
const tagsSchema = z.array(z.string().max(64)).max(32);
const uploadInputSchema = z.object({
  description: z.string().max(20_000).optional(),
  filename: z.string().trim().min(1).max(255),
  idempotency_key: z.string().trim().min(1).max(200),
  kind: publicArtifactKindSchema,
  mime_type: z.string().trim().min(1).max(255).optional(),
  name: z.string().trim().min(1).max(255).optional(),
  sha256: z.string().regex(/^[0-9a-f]{64}$/),
  size_bytes: z.number().int().min(0),
  tags: tagsSchema.optional(),
});
const versionUploadInputSchema = uploadInputSchema.pick({
  filename: true,
  idempotency_key: true,
  mime_type: true,
  sha256: true,
  size_bytes: true,
});
const updateInputSchema = z
  .object({
    description: z.string().max(20_000).nullable().optional(),
    kind: publicArtifactKindSchema.optional(),
    name: z.string().trim().min(1).max(255).optional(),
    tags: tagsSchema.optional(),
  })
  .refine((value) => Object.keys(value).length > 0);
const listQuerySchema = z.object({
  cursor: z.string().max(4_096).optional(),
  kind: artifactKindSchema.optional(),
  limit: z.coerce.number().int().min(1).max(100).optional(),
  source: artifactSourceSchema.optional(),
  status: artifactStatusSchema.optional(),
  tag: z.string().trim().min(1).max(64).optional(),
});
const trashQuerySchema = listQuerySchema.pick({
  cursor: true,
  kind: true,
  limit: true,
  tag: true,
});
const projectArtifactParamsSchema = z.object({
  artifactId: idSchema,
  projectId: idSchema,
});
const uploadParamsSchema = z.object({
  projectId: idSchema,
  uploadId: idSchema,
});
const versionParamsSchema = projectArtifactParamsSchema.extend({
  versionId: idSchema,
});
const semanticDescriptionInputSchema = z.object({
  agent_instance_id: idSchema.optional(),
});
const folderCreateInputSchema = z.object({
  name: z.string().trim().min(1).max(255),
  parent_folder_id: idSchema.nullable().optional(),
});
const folderRenameInputSchema = z.object({
  name: z.string().trim().min(1).max(255),
});
const folderMoveInputSchema = z.object({
  parent_folder_id: idSchema.nullable(),
  position: z.number().int().min(0).optional(),
});
const folderDeleteQuerySchema = z.object({
  recursive: z.enum(["true", "false"]).optional().default("false"),
});
const artifactFolderInputSchema = z.object({
  folder_id: idSchema.nullable(),
});

export function registerArtifactRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
): void {
  app.post(
    "/api/projects/:projectId/artifacts/uploads",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const input = uploadInputSchema.parse(request.body);
      const session = await coreClient.initializeArtifactUpload(
        request.currentProjectId!,
        input satisfies components["schemas"]["ArtifactInitializeUploadRequest"],
        coreContext(request),
      );
      return reply.code(201).send(session);
    },
  );

  app.get(
    "/api/projects/:projectId/artifacts/uploads/:uploadId",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { uploadId } = uploadParamsSchema.parse(request.params);
      return coreClient.getArtifactUpload(
        request.currentProjectId!,
        uploadId,
        coreContext(request),
      );
    },
  );

  app.delete(
    "/api/projects/:projectId/artifacts/uploads/:uploadId",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const { uploadId } = uploadParamsSchema.parse(request.params);
      await coreClient.abortArtifactUpload(
        request.currentProjectId!,
        uploadId,
        coreContext(request),
      );
      return reply.code(204).send();
    },
  );

  app.post(
    "/api/projects/:projectId/artifacts/uploads/:uploadId/parts/sign",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { uploadId } = uploadParamsSchema.parse(request.params);
      const input = z
        .object({
          part_numbers: z
            .array(z.number().int().min(1).max(10_000))
            .min(1)
            .max(100),
        })
        .parse(request.body);
      const grants = await coreClient.signArtifactUploadParts(
        request.currentProjectId!,
        uploadId,
        input,
        coreContext(request),
      );
      return rewriteTransferGrants(grants);
    },
  );

  app.post(
    "/api/projects/:projectId/artifacts/uploads/:uploadId/confirm",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { uploadId } = uploadParamsSchema.parse(request.params);
      const input = z
        .object({
          parts: z
            .array(
              z.object({
                etag: z.string().trim().min(1).max(1_024),
                part_number: z.number().int().min(1).max(10_000),
              }),
            )
            .max(10_000),
        })
        .parse(request.body);
      return coreClient.confirmArtifactUpload(
        request.currentProjectId!,
        uploadId,
        input,
        coreContext(request),
      );
    },
  );

  app.get(
    "/api/projects/:projectId/artifacts",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const query = listQuerySchema.parse(request.query);
      return coreClient.listArtifacts(
        request.currentProjectId!,
        coreContext(request),
        query,
      );
    },
  );

  app.get(
    "/api/projects/:projectId/artifacts/trash",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const query = trashQuerySchema.parse(request.query);
      return coreClient.listArtifactTrash(
        request.currentProjectId!,
        coreContext(request),
        query,
      );
    },
  );

  app.get(
    "/api/projects/:projectId/artifacts/folders",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.request<components["schemas"]["ArtifactFolderTree"]>(
        `/v1/projects/${encodeURIComponent(request.currentProjectId!)}/artifacts/folders`,
        { method: "GET" },
        coreContext(request),
      ),
  );

  app.post(
    "/api/projects/:projectId/artifacts/folders",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const folder = await coreClient.request<
        components["schemas"]["ArtifactFolder"]
      >(
        `/v1/projects/${encodeURIComponent(request.currentProjectId!)}/artifacts/folders`,
        {
          body: folderCreateInputSchema.parse(request.body),
          method: "POST",
        },
        coreContext(request),
      );
      return reply.code(201).send(folder);
    },
  );

  app.patch(
    "/api/projects/:projectId/artifacts/folders/:folderId",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { folderId } = z
        .object({ folderId: idSchema })
        .parse(request.params);
      return coreClient.request<components["schemas"]["ArtifactFolder"]>(
        `/v1/projects/${encodeURIComponent(request.currentProjectId!)}/artifacts/folders/${encodeURIComponent(folderId)}`,
        {
          body: folderRenameInputSchema.parse(request.body),
          method: "PATCH",
        },
        coreContext(request),
      );
    },
  );

  app.delete(
    "/api/projects/:projectId/artifacts/folders/:folderId",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const { folderId } = z
        .object({ folderId: idSchema })
        .parse(request.params);
      const { recursive } = folderDeleteQuerySchema.parse(request.query);
      await coreClient.request<void>(
        `/v1/projects/${encodeURIComponent(request.currentProjectId!)}/artifacts/folders/${encodeURIComponent(folderId)}?recursive=${recursive}`,
        { method: "DELETE" },
        coreContext(request),
      );
      return reply.code(204).send();
    },
  );

  app.post(
    "/api/projects/:projectId/artifacts/folders/:folderId/move",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { folderId } = z
        .object({ folderId: idSchema })
        .parse(request.params);
      return coreClient.request<components["schemas"]["ArtifactFolder"]>(
        `/v1/projects/${encodeURIComponent(request.currentProjectId!)}/artifacts/folders/${encodeURIComponent(folderId)}/move`,
        {
          body: folderMoveInputSchema.parse(request.body),
          method: "POST",
        },
        coreContext(request),
      );
    },
  );

  app.get(
    "/api/projects/:projectId/artifacts/:artifactId",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { artifactId } = projectArtifactParamsSchema.parse(request.params);
      return coreClient.getArtifact(
        request.currentProjectId!,
        artifactId,
        coreContext(request),
      );
    },
  );

  app.put(
    "/api/projects/:projectId/artifacts/:artifactId/folder",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { artifactId } = projectArtifactParamsSchema.parse(request.params);
      return coreClient.request<components["schemas"]["ArtifactDetail"]>(
        `/v1/projects/${encodeURIComponent(request.currentProjectId!)}/artifacts/${encodeURIComponent(artifactId)}/folder`,
        {
          body: artifactFolderInputSchema.parse(request.body),
          method: "PUT",
        },
        coreContext(request),
      );
    },
  );

  app.patch(
    "/api/projects/:projectId/artifacts/:artifactId",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { artifactId } = projectArtifactParamsSchema.parse(request.params);
      return coreClient.updateArtifact(
        request.currentProjectId!,
        artifactId,
        updateInputSchema.parse(request.body),
        coreContext(request),
      );
    },
  );

  app.post(
    "/api/projects/:projectId/artifacts/:artifactId/description",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const { artifactId } = projectArtifactParamsSchema.parse(request.params);
      const result = await coreClient.requestArtifactSemanticDescription(
        request.currentProjectId!,
        artifactId,
        semanticDescriptionInputSchema.parse(request.body ?? {}),
        coreContext(request),
      );
      return reply.code(202).send(result);
    },
  );

  app.delete(
    "/api/projects/:projectId/artifacts/:artifactId",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const { artifactId } = projectArtifactParamsSchema.parse(request.params);
      await coreClient.trashArtifact(
        request.currentProjectId!,
        artifactId,
        coreContext(request),
      );
      return reply.code(204).send();
    },
  );

  app.get(
    "/api/projects/:projectId/artifacts/:artifactId/versions",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { artifactId } = projectArtifactParamsSchema.parse(request.params);
      return coreClient.listArtifactVersions(
        request.currentProjectId!,
        artifactId,
        coreContext(request),
      );
    },
  );

  app.post(
    "/api/projects/:projectId/artifacts/:artifactId/versions/uploads",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const { artifactId } = projectArtifactParamsSchema.parse(request.params);
      const session = await coreClient.initializeArtifactVersionUpload(
        request.currentProjectId!,
        artifactId,
        versionUploadInputSchema.parse(request.body),
        coreContext(request),
      );
      return reply.code(201).send(session);
    },
  );

  app.post(
    "/api/projects/:projectId/artifacts/:artifactId/versions/:versionId/restore",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { artifactId, versionId } = versionParamsSchema.parse(
        request.params,
      );
      const input = z
        .object({ idempotency_key: z.string().trim().min(1).max(200) })
        .parse(request.body);
      return coreClient.restoreArtifactVersion(
        request.currentProjectId!,
        artifactId,
        versionId,
        input,
        coreContext(request),
      );
    },
  );

  app.post(
    "/api/projects/:projectId/artifacts/:artifactId/download",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { artifactId } = projectArtifactParamsSchema.parse(request.params);
      const grant = await coreClient.downloadArtifact(
        request.currentProjectId!,
        artifactId,
        coreContext(request),
      );
      return rewriteDownloadGrant(grant);
    },
  );

  app.post(
    "/api/projects/:projectId/artifacts/:artifactId/versions/:versionId/download",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { artifactId, versionId } = versionParamsSchema.parse(
        request.params,
      );
      const grant = await coreClient.downloadArtifact(
        request.currentProjectId!,
        artifactId,
        coreContext(request),
        versionId,
      );
      return rewriteDownloadGrant(grant);
    },
  );

  app.get(
    "/api/projects/:projectId/artifacts/:artifactId/versions/:versionId/previews",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const { artifactId, versionId } = versionParamsSchema.parse(
        request.params,
      );
      const previews = await coreClient.listArtifactPreviews(
        request.currentProjectId!,
        artifactId,
        versionId,
        coreContext(request),
      );
      reply.header("cache-control", "no-store");
      return rewritePreviewGrants(previews);
    },
  );

  app.post(
    "/api/projects/:projectId/artifacts/:artifactId/restore",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const { artifactId } = projectArtifactParamsSchema.parse(request.params);
      return coreClient.restoreArtifact(
        request.currentProjectId!,
        artifactId,
        coreContext(request),
      );
    },
  );

  app.delete(
    "/api/projects/:projectId/artifacts/:artifactId/purge",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const { artifactId } = projectArtifactParamsSchema.parse(request.params);
      await coreClient.purgeArtifact(
        request.currentProjectId!,
        artifactId,
        coreContext(request),
      );
      return reply.code(204).send();
    },
  );
}

function coreContext(request: FastifyRequest) {
  return {
    accessToken: request.browserIdentity!.accessToken,
    projectId: request.currentProjectId!,
    requestId: request.id,
    userId: request.browserIdentity!.userId,
  };
}

function rewriteTransferUrl(url: string): string {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return url;
  }
  const prefix = "/v1/artifact-transfers/";
  if (!parsed.pathname.startsWith(prefix)) {
    return url;
  }
  const token = parsed.pathname.slice(prefix.length);
  if (!/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(token)) {
    return url;
  }
  return `/api/artifact-transfers/${token}`;
}

export function rewriteTransferGrant(
  transfer: components["schemas"]["ArtifactTransferGrant"],
): components["schemas"]["ArtifactTransferGrant"] {
  return { ...transfer, url: rewriteTransferUrl(transfer.url) };
}

function rewriteTransferGrants(
  grants: components["schemas"]["ArtifactPartGrantList"],
): components["schemas"]["ArtifactPartGrantList"] {
  return {
    items: grants.items.map((grant) => ({
      ...grant,
      transfer: rewriteTransferGrant(grant.transfer),
    })),
  };
}

function rewriteDownloadGrant(
  grant: components["schemas"]["ArtifactDownloadGrant"],
): components["schemas"]["ArtifactDownloadGrant"] {
  return { ...grant, transfer: rewriteTransferGrant(grant.transfer) };
}

function rewritePreviewGrants(
  previews: components["schemas"]["ArtifactPreviewList"],
): components["schemas"]["ArtifactPreviewList"] {
  return {
    items: previews.items.map((preview) => ({
      ...preview,
      transfer: preview.transfer
        ? rewriteTransferGrant(preview.transfer)
        : null,
    })),
  };
}
