import type { CoreClient } from "@mmdash/core-client";
import type { components } from "@mmdash/core-client/generated";
import type { FastifyInstance } from "fastify";
import { z } from "zod";

const typeKeySchema = z
  .string()
  .min(1)
  .max(200)
  .regex(/^[a-z][a-z0-9_.-]*$/);

const updateSchema = z.object({
  values: z.record(z.string(), z.unknown()),
});

export function registerSettingsRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
): void {
  app.get(
    "/api/settings/types",
    { config: { auth: "required", project: "none" } },
    async (request) => {
      const query = z
        .object({ scope: z.literal("system") })
        .parse(request.query);
      return coreClient.listSettingTypes(query.scope, coreContext(request));
    },
  );

  app.get(
    "/api/settings/system/:typeKey",
    { config: { auth: "required", project: "none" } },
    async (request) =>
      coreClient.getSystemSetting(
        typeKey(request.params),
        coreContext(request),
      ),
  );

  app.patch(
    "/api/settings/system/:typeKey",
    { config: { auth: "required", project: "none" } },
    async (request) =>
      coreClient.updateSystemSetting(
        typeKey(request.params),
        updateSchema.parse(
          request.body,
        ) satisfies components["schemas"]["UpdateSettingRequest"],
        coreContext(request),
      ),
  );

  app.delete(
    "/api/settings/system/:typeKey",
    { config: { auth: "required", project: "none" } },
    async (request, reply) => {
      await coreClient.deleteSystemSetting(
        typeKey(request.params),
        coreContext(request),
      );
      return reply.code(204).send();
    },
  );

  app.post(
    "/api/settings/system/:typeKey/test",
    { config: { auth: "required", project: "none" } },
    async (request) =>
      coreClient.testSystemSetting(
        typeKey(request.params),
        coreContext(request),
      ),
  );

  app.get(
    "/api/projects/:projectId/settings/types",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.listSettingTypes(
        "project",
        coreContext(request, request.currentProjectId),
        request.currentProjectId,
      ),
  );

  app.get(
    "/api/projects/:projectId/settings/:typeKey",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.getProjectSetting(
        request.currentProjectId!,
        typeKey(request.params),
        coreContext(request, request.currentProjectId),
      ),
  );

  app.patch(
    "/api/projects/:projectId/settings/:typeKey",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.updateProjectSetting(
        request.currentProjectId!,
        typeKey(request.params),
        updateSchema.parse(
          request.body,
        ) satisfies components["schemas"]["UpdateSettingRequest"],
        coreContext(request, request.currentProjectId),
      ),
  );

  app.delete(
    "/api/projects/:projectId/settings/:typeKey",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      await coreClient.deleteProjectSetting(
        request.currentProjectId!,
        typeKey(request.params),
        coreContext(request, request.currentProjectId),
      );
      return reply.code(204).send();
    },
  );

  app.post(
    "/api/projects/:projectId/settings/:typeKey/test",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.testProjectSetting(
        request.currentProjectId!,
        typeKey(request.params),
        coreContext(request, request.currentProjectId),
      ),
  );
}

function typeKey(parameters: unknown): string {
  return typeKeySchema.parse(
    (parameters as Record<string, unknown> | null)?.typeKey,
  );
}

function coreContext(
  request: {
    browserIdentity?: {
      accessToken: string;
      userId: string;
    };
    id: string;
  },
  projectId?: string,
) {
  return {
    accessToken: request.browserIdentity!.accessToken,
    projectId,
    requestId: request.id,
    userId: request.browserIdentity!.userId,
  };
}
