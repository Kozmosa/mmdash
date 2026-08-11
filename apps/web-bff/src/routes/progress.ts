import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance } from "fastify";
import { z } from "zod";

const projectId = z.object({ projectId: z.string().uuid() });
const milestoneId = projectId.extend({ milestoneId: z.string().uuid() });
const taskId = projectId.extend({ taskId: z.string().uuid() });
const dependencyId = projectId.extend({ dependencyId: z.string().uuid() });
const reminderId = projectId.extend({ reminderId: z.string().uuid() });
const proposalId = projectId.extend({ proposalId: z.string().uuid() });
const evaluationId = projectId.extend({ evaluationId: z.string().uuid() });
const date = z.string().datetime({ offset: true }).optional();
const createMilestone = z.object({ title: z.string().trim().min(1).max(255), description: z.string().max(10_000).optional(), critical: z.boolean().optional(), start_at: date, target_at: date, target_has_time: z.boolean().optional() });
const updateMilestone = z.object({ title: z.string().trim().min(1).max(255).optional(), description: z.string().max(10_000).optional(), status: z.enum(["planned", "in_progress", "completed"]).optional(), critical: z.boolean().optional(), start_at: date, target_at: date, target_has_time: z.boolean().optional() }).refine((value) => Object.keys(value).length > 0);
const createTask = z.object({ milestone_id: z.string().uuid().optional(), title: z.string().trim().min(1).max(255), description: z.string().max(10_000).optional(), status: z.enum(["todo", "in_progress", "blocked", "done"]).optional(), assignee_id: z.string().uuid().optional(), start_at: date, due_at: date, related_object_ids: z.array(z.string().uuid()).optional(), source_run_id: z.string().max(200).optional() });
const updateTask = createTask.partial().refine((value) => Object.keys(value).length > 0);
const createDependency = z.object({ task_id: z.string().uuid(), depends_on_task_id: z.string().uuid(), kind: z.enum(["blocks", "relates_to"]).optional() });
const createReminder = z.object({ task_id: z.string().uuid().optional(), milestone_id: z.string().uuid().optional(), remind_at: z.string().datetime({ offset: true }), note: z.string().max(2_000).optional() });
const createProposal = z.object({ proposal_type: z.enum(["milestone.create", "milestone.update", "milestone.complete", "task.create", "task.update", "task.complete"]), target_id: z.string().uuid().optional(), title: z.string().trim().min(1).max(255), rationale: z.string().max(10_000).optional(), changes: z.record(z.string(), z.unknown()), source_run_id: z.string().max(200).optional() });
const reviewProposal = z.object({ decision: z.enum(["accepted", "rejected"]), note: z.string().max(4_000).optional() });
const batchReviewProposals = z.object({ proposal_ids: z.array(z.string().uuid()).min(1).max(100), decision: z.enum(["accepted", "rejected"]), note: z.string().max(4_000).optional() });
const updateSettings = z.object({
  auto_task_changes: z.boolean(),
  auto_tracking_enabled: z.boolean(),
  event_triggers_enabled: z.boolean(),
  cron_enabled: z.boolean(),
  cron_schedule: z.string().trim().min(1).max(100),
  debounce_seconds: z.number().int().min(0).max(3_600),
  min_interval_seconds: z.number().int().min(0).max(86_400),
  reasoning_effort: z.enum(["none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"]),
  agent_instance_id: z.string().uuid().optional(),
});
const recalculate = z.object({ trigger_kind: z.enum(["manual", "cron"]), force: z.boolean() });
const evaluationQuery = z.object({ cursor: z.string().max(2_048).optional(), limit: z.coerce.number().int().min(1).max(100).optional() });
const stageOverride = z.object({ stage: z.string().trim().min(1).max(100), summary: z.string().max(2_000).optional(), note: z.string().max(2_000).optional() });

export function registerProgressRoutes(app: FastifyInstance, coreClient: CoreClient): void {
  app.get("/api/projects/:projectId/progress", { config: { auth: "required", project: "required" } }, async (request) => coreClient.getProgress(request.currentProjectId!, context(request)));
  app.get("/api/projects/:projectId/progress/milestones", { config: { auth: "required", project: "required" } }, async (request) => coreClient.listProgressMilestones(request.currentProjectId!, context(request)));
  app.post("/api/projects/:projectId/progress/milestones", { config: { auth: "required", project: "required" } }, async (request, reply) => reply.code(201).send(await coreClient.createProgressMilestone(request.currentProjectId!, createMilestone.parse(request.body), context(request))));
  app.patch("/api/projects/:projectId/progress/milestones/:milestoneId", { config: { auth: "required", project: "required" } }, async (request) => { const params = milestoneId.parse(request.params); return coreClient.updateProgressMilestone(params.projectId, params.milestoneId, updateMilestone.parse(request.body), context(request)); });
  app.delete("/api/projects/:projectId/progress/milestones/:milestoneId", { config: { auth: "required", project: "required" } }, async (request, reply) => { const params = milestoneId.parse(request.params); await coreClient.deleteProgressMilestone(params.projectId, params.milestoneId, context(request)); return reply.code(204).send(); });

  app.get("/api/projects/:projectId/progress/tasks", { config: { auth: "required", project: "required" } }, async (request) => coreClient.listProgressTasks(request.currentProjectId!, context(request)));
  app.post("/api/projects/:projectId/progress/tasks", { config: { auth: "required", project: "required" } }, async (request, reply) => reply.code(201).send(await coreClient.createProgressTask(request.currentProjectId!, createTask.parse(request.body), context(request))));
  app.patch("/api/projects/:projectId/progress/tasks/:taskId", { config: { auth: "required", project: "required" } }, async (request) => { const params = taskId.parse(request.params); return coreClient.updateProgressTask(params.projectId, params.taskId, updateTask.parse(request.body), context(request)); });
  app.delete("/api/projects/:projectId/progress/tasks/:taskId", { config: { auth: "required", project: "required" } }, async (request, reply) => { const params = taskId.parse(request.params); await coreClient.deleteProgressTask(params.projectId, params.taskId, context(request)); return reply.code(204).send(); });

  app.get("/api/projects/:projectId/progress/dependencies", { config: { auth: "required", project: "required" } }, async (request) => coreClient.listProgressDependencies(request.currentProjectId!, context(request)));
  app.post("/api/projects/:projectId/progress/dependencies", { config: { auth: "required", project: "required" } }, async (request, reply) => reply.code(201).send(await coreClient.createProgressDependency(request.currentProjectId!, createDependency.parse(request.body), context(request))));
  app.delete("/api/projects/:projectId/progress/dependencies/:dependencyId", { config: { auth: "required", project: "required" } }, async (request, reply) => { const params = dependencyId.parse(request.params); await coreClient.deleteProgressDependency(params.projectId, params.dependencyId, context(request)); return reply.code(204).send(); });

  app.get("/api/projects/:projectId/progress/reminders", { config: { auth: "required", project: "required" } }, async (request) => coreClient.listProgressReminders(request.currentProjectId!, context(request)));
  app.post("/api/projects/:projectId/progress/reminders", { config: { auth: "required", project: "required" } }, async (request, reply) => reply.code(201).send(await coreClient.createProgressReminder(request.currentProjectId!, createReminder.parse(request.body), context(request))));
  app.post("/api/projects/:projectId/progress/reminders/:reminderId/trigger", { config: { auth: "required", project: "required" } }, async (request) => { const params = reminderId.parse(request.params); return coreClient.triggerProgressReminder(params.projectId, params.reminderId, context(request)); });

  app.get("/api/projects/:projectId/progress/proposals", { config: { auth: "required", project: "required" } }, async (request) => coreClient.listProgressProposals(request.currentProjectId!, context(request)));
  app.post("/api/projects/:projectId/progress/proposals", { config: { auth: "required", project: "required" } }, async (request, reply) => reply.code(201).send(await coreClient.createProgressProposal(request.currentProjectId!, createProposal.parse(request.body), context(request))));
  app.post("/api/projects/:projectId/progress/proposals/batch-review", { config: { auth: "required", project: "required" } }, async (request) => coreClient.batchReviewProgressProposals(request.currentProjectId!, batchReviewProposals.parse(request.body), context(request)));
  app.post("/api/projects/:projectId/progress/proposals/:proposalId/review", { config: { auth: "required", project: "required" } }, async (request) => { const params = proposalId.parse(request.params); return coreClient.reviewProgressProposal(params.projectId, params.proposalId, reviewProposal.parse(request.body), context(request)); });

  app.get("/api/projects/:projectId/progress/settings", { config: { auth: "required", project: "required" } }, async (request) => coreClient.getProgressSettings(request.currentProjectId!, context(request)));
  app.patch("/api/projects/:projectId/progress/settings", { config: { auth: "required", project: "required" } }, async (request) => coreClient.updateProgressSettings(request.currentProjectId!, updateSettings.parse(request.body), context(request)));
  app.post("/api/projects/:projectId/progress/recalculate", { config: { auth: "required", project: "required" } }, async (request, reply) => reply.code(202).send(await coreClient.recalculateProgress(request.currentProjectId!, recalculate.parse(request.body), context(request))));
  app.get("/api/projects/:projectId/progress/evaluations", { config: { auth: "required", project: "required" } }, async (request) => coreClient.listProgressEvaluations(request.currentProjectId!, evaluationQuery.parse(request.query), context(request)));
  app.get("/api/projects/:projectId/progress/evaluations/:evaluationId", { config: { auth: "required", project: "required" } }, async (request) => { const params = evaluationId.parse(request.params); return coreClient.getProgressEvaluation(params.projectId, params.evaluationId, context(request)); });
  app.post("/api/projects/:projectId/progress/evaluations/:evaluationId/retry", { config: { auth: "required", project: "required" } }, async (request, reply) => { const params = evaluationId.parse(request.params); return reply.code(202).send(await coreClient.retryProgressEvaluation(params.projectId, params.evaluationId, context(request))); });
  app.post("/api/projects/:projectId/progress/stage-override", { config: { auth: "required", project: "required" } }, async (request) => coreClient.setProgressStageOverride(request.currentProjectId!, stageOverride.parse(request.body), context(request)));
  app.delete("/api/projects/:projectId/progress/stage-override", { config: { auth: "required", project: "required" } }, async (request) => coreClient.clearProgressStageOverride(request.currentProjectId!, context(request)));
}

function context(request: { browserIdentity?: { accessToken: string; userId: string }; currentProjectId?: string; id: string }) {
  return { accessToken: request.browserIdentity!.accessToken, projectId: request.currentProjectId, requestId: request.id, userId: request.browserIdentity!.userId };
}
