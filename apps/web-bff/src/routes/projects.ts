import type { CoreClient } from "@mmdash/core-client";
import type { components } from "@mmdash/core-client/generated";
import type { FastifyInstance } from "fastify";
import { z } from "zod";

const createProjectSchema = z.object({
  name: z.string().trim().min(1).max(200),
  problem_summary: z.string().max(20_000).default(""),
  problem_title: z.string().max(500).default(""),
  project_constraints: z.array(z.string().max(2_000)).default([]),
  source_artifact_ids: z.array(z.string().max(200)).default([]),
});

const updateProjectSchema = createProjectSchema.partial().extend({
  archived: z.boolean().optional(),
});

const memberRoleSchema = z.object({
  role: z.enum(["owner", "maintainer", "editor", "viewer", "agent", "box"]),
});

export function registerProjectRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
): void {
  app.get(
    "/api/projects",
    { config: { auth: "required", project: "none" } },
    async (request) => {
      const query = z
        .object({ include_archived: z.coerce.boolean().default(false) })
        .parse(request.query);
      return coreClient.listProjects(
        coreContext(request),
        query.include_archived,
      );
    },
  );

  app.put(
    "/api/projects/:projectId/members/:userId",
    { config: { auth: "required", project: "required" } },
    async (request) => {
      const parameters = z
        .object({ userId: z.string().uuid() })
        .parse(request.params);
      const { role } = memberRoleSchema.parse(request.body);
      return coreClient.updateProjectMember(
        request.currentProjectId!,
        parameters.userId,
        role,
        coreContext(request, request.currentProjectId),
      );
    },
  );

  app.get(
    "/api/projects/:projectId/invitations",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.listProjectInvitations(
        request.currentProjectId!,
        coreContext(request, request.currentProjectId),
      ),
  );

  app.post(
    "/api/projects/:projectId/invitations",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const input = z
        .object({
          email: z.string().email(),
          role: z.enum([
            "owner",
            "maintainer",
            "editor",
            "viewer",
            "agent",
            "box",
          ]),
        })
        .parse(request.body);
      const issued = await coreClient.createProjectInvitation(
        request.currentProjectId!,
        input,
        coreContext(request, request.currentProjectId),
      );
      return reply.code(201).send(issued);
    },
  );

  app.delete(
    "/api/projects/:projectId/invitations/:invitationId",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const { invitationId } = z
        .object({ invitationId: z.string().uuid() })
        .parse(request.params);
      await coreClient.revokeProjectInvitation(
        request.currentProjectId!,
        invitationId,
        coreContext(request, request.currentProjectId),
      );
      return reply.code(204).send();
    },
  );

  app.delete(
    "/api/projects/:projectId/members/:userId",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const parameters = z
        .object({ userId: z.string().uuid() })
        .parse(request.params);
      await coreClient.removeProjectMember(
        request.currentProjectId!,
        parameters.userId,
        coreContext(request, request.currentProjectId),
      );
      return reply.code(204).send();
    },
  );

  app.post(
    "/api/projects",
    { config: { auth: "required", project: "none" } },
    async (request, reply) => {
      const input = createProjectSchema.parse(request.body);
      const project = await coreClient.createProject(
        input satisfies components["schemas"]["CreateProjectRequest"],
        coreContext(request),
      );
      return reply.code(201).send(project);
    },
  );

  app.get(
    "/api/projects/:projectId",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.getProject(
        request.currentProjectId!,
        coreContext(request, request.currentProjectId),
      ),
  );

  app.patch(
    "/api/projects/:projectId",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.updateProject(
        request.currentProjectId!,
        updateProjectSchema.parse(request.body),
        coreContext(request, request.currentProjectId),
      ),
  );

  app.get(
    "/api/projects/:projectId/members",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.listProjectMembers(
        request.currentProjectId!,
        coreContext(request, request.currentProjectId),
      ),
  );

  app.get(
    "/api/projects/:projectId/permissions",
    { config: { auth: "required", project: "required" } },
    async (request) =>
      coreClient.getProjectPermissions(
        request.currentProjectId!,
        coreContext(request, request.currentProjectId),
      ),
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
