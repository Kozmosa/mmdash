import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance } from "fastify";
import { z } from "zod";

const project = z.object({ projectId: z.string().uuid() });
const question = project.extend({ questionId: z.string().uuid() });
const snapshot = question.extend({ snapshotId: z.string().uuid() });
const createQuestion = z.object({
  code: z.string().trim().regex(/^[A-Za-z][A-Za-z0-9_-]{0,31}$/),
  title: z.string().trim().min(1).max(255),
  notion_page_id: z.string().uuid(),
  position: z.number().int().min(0).optional(),
});
const updateQuestion = createQuestion.partial().refine((value) => Object.keys(value).length > 0);
const updateSnapshot = z.object({
  tags: z.array(z.string().trim().min(1).max(64)).max(20).optional(),
  version_note: z.string().max(4_000).optional(),
}).refine((value) => value.tags !== undefined || value.version_note !== undefined);
const diffQuery = z.object({
  from_snapshot_id: z.string().uuid(),
  to_snapshot_id: z.string().uuid(),
});
const startNotionOAuth = z.object({
  root_page_url: z.string().url().max(2_048),
  auto_sync_enabled: z.boolean(),
  auto_sync_interval_seconds: z.number().int().min(60).max(86_400),
});
const notionOAuthCallback = z.object({
  state: z.string().min(32).max(512),
  code: z.string().min(1).max(2_048).optional(),
  error: z.string().min(1).max(200).optional(),
}).refine((value) => Boolean(value.code) !== Boolean(value.error));

export function registerModelRoutes(app: FastifyInstance, coreClient: CoreClient): void {
  const options = { config: { auth: "required" as const, project: "required" as const } };
  app.get("/api/projects/:projectId/models", options, async (request) => coreClient.getModels(request.currentProjectId!, context(request)));
  app.get("/api/projects/:projectId/models/source", options, async (request) => coreClient.getModelSource(request.currentProjectId!, context(request)));
  app.post("/api/projects/:projectId/models/source/sync", options, async (request, reply) => reply.code(202).send(await coreClient.syncModels(request.currentProjectId!, context(request))));
  app.get("/api/projects/:projectId/models/notion/oauth", options, async (request) => coreClient.getNotionOAuthConnection(request.currentProjectId!, context(request)));
  app.post("/api/projects/:projectId/models/notion/oauth/authorizations", options, async (request, reply) => reply.code(201).send(await coreClient.startNotionOAuth(request.currentProjectId!, startNotionOAuth.parse(request.body), context(request))));
  app.delete("/api/projects/:projectId/models/notion/oauth/connection", options, async (request, reply) => { await coreClient.disconnectNotionOAuth(request.currentProjectId!, context(request)); return reply.code(204).send(); });
  app.get("/api/integrations/notion/oauth/callback", { config: { auth: "required", project: "none" } }, async (request, reply) => {
    const query = notionOAuthCallback.parse(request.query);
    const result = await coreClient.completeNotionOAuth(query, context(request));
    const params = new URLSearchParams({ notion_oauth: result.status });
    return reply.redirect(`/projects/${encodeURIComponent(result.project_id)}/settings?${params.toString()}#model-settings`);
  });
  app.get("/api/projects/:projectId/models/questions", options, async (request) => coreClient.listModelQuestions(request.currentProjectId!, context(request)));
  app.post("/api/projects/:projectId/models/questions", options, async (request, reply) => reply.code(201).send(await coreClient.createModelQuestion(request.currentProjectId!, createQuestion.parse(request.body), context(request))));
  app.get("/api/projects/:projectId/models/questions/:questionId", options, async (request) => { const params = question.parse(request.params); return coreClient.getModelQuestion(params.projectId, params.questionId, context(request)); });
  app.patch("/api/projects/:projectId/models/questions/:questionId", options, async (request) => { const params = question.parse(request.params); return coreClient.updateModelQuestion(params.projectId, params.questionId, updateQuestion.parse(request.body), context(request)); });
  app.delete("/api/projects/:projectId/models/questions/:questionId", options, async (request, reply) => { const params = question.parse(request.params); await coreClient.deleteModelQuestion(params.projectId, params.questionId, context(request)); return reply.code(204).send(); });
  app.post("/api/projects/:projectId/models/questions/:questionId/sync", options, async (request, reply) => { const params = question.parse(request.params); return reply.code(202).send(await coreClient.syncModelQuestion(params.projectId, params.questionId, context(request))); });
  app.get("/api/projects/:projectId/models/questions/:questionId/snapshots", options, async (request) => { const params = question.parse(request.params); return coreClient.listModelSnapshots(params.projectId, params.questionId, context(request)); });
  app.get("/api/projects/:projectId/models/questions/:questionId/snapshots/:snapshotId", options, async (request) => { const params = snapshot.parse(request.params); return coreClient.getModelSnapshot(params.projectId, params.questionId, params.snapshotId, context(request)); });
  app.patch("/api/projects/:projectId/models/questions/:questionId/snapshots/:snapshotId", options, async (request) => { const params = snapshot.parse(request.params); return coreClient.updateModelSnapshot(params.projectId, params.questionId, params.snapshotId, updateSnapshot.parse(request.body), context(request)); });
  app.get("/api/projects/:projectId/models/questions/:questionId/diff", options, async (request) => { const params = question.parse(request.params); const query = diffQuery.parse(request.query); return coreClient.diffModelSnapshots(params.projectId, params.questionId, query.from_snapshot_id, query.to_snapshot_id, context(request)); });
}

function context(request: { browserIdentity?: { accessToken: string; userId: string }; currentProjectId?: string; id: string }) {
  return { accessToken: request.browserIdentity!.accessToken, projectId: request.currentProjectId, requestId: request.id, userId: request.browserIdentity!.userId };
}
