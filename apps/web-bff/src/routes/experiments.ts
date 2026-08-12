import type { CoreClient } from "@mmdash/core-client";
import type { components } from "@mmdash/core-client/generated";
import type { FastifyInstance } from "fastify";
import { z } from "zod";

const project = z.object({ projectId: z.string().uuid() });
const experiment = project.extend({ experimentId: z.string().uuid() });
const createExperiment = z.object({
  name: z.string().trim().min(1).max(200),
  source_commit: z.string().regex(/^[0-9a-f]{40}([0-9a-f]{24})?$/),
  entrypoint: z.string().regex(/^(python3?|node|go|binary):[a-zA-Z0-9_./-]+$/).refine((value) => !value.split(":", 2)[1]!.split("/").some((part) => part === "." || part === ".."), "entrypoint path must be normalized"),
  parameters: z.record(z.string(), z.unknown()),
  environment: z.record(z.string(), z.string()),
  inputs: z.record(z.string(), z.unknown()),
  runtime: z.enum(["local-docker", "e2b"]),
  limits: z.object({
    cpu_millis: z.number().int().positive(), memory_bytes: z.number().int().positive(),
    timeout_seconds: z.number().int().positive(), disk_bytes: z.number().int().positive(),
    pids: z.number().int().positive(), network: z.enum(["disabled", "restricted", "enabled"]),
  }),
  idempotency_key: z.string().trim().min(1).max(200),
  max_attempts: z.number().int().min(1).max(5).default(1),
});

export function registerExperimentRoutes(app: FastifyInstance, coreClient: CoreClient): void {
  const options = { config: { auth: "required" as const, project: "required" as const } };
  app.get("/api/projects/:projectId/experiments", options, async (request) => {
    const params = project.parse(request.params);
    const query = z.object({ cursor: z.string().optional(), limit: z.coerce.number().int().min(1).max(200).optional(), status: z.string().optional() }).parse(request.query);
    return coreClient.listExperiments(params.projectId, context(request), query);
  });
  app.post("/api/projects/:projectId/experiments", options, async (request, reply) => {
    const params = project.parse(request.params);
    const value = await coreClient.createExperiment(params.projectId, createExperiment.parse(request.body) satisfies components["schemas"]["CreateExperimentRequest"], context(request));
    return reply.code(201).send(value);
  });
  app.get("/api/projects/:projectId/experiments/compare", options, async (request) => {
    const params = project.parse(request.params);
    const query = z.object({
      experiment_id: z.union([z.string(), z.array(z.string())]),
    }).parse(request.query);
    const ids = Array.isArray(query.experiment_id)
      ? query.experiment_id
      : query.experiment_id.split(",").filter(Boolean);
    return coreClient.compareExperiments(params.projectId, ids, context(request));
  });
  app.get("/api/projects/:projectId/experiments/:experimentId", options, async (request) => {
    const params = experiment.parse(request.params);
    return coreClient.getExperiment(params.projectId, params.experimentId, context(request));
  });
  app.post("/api/projects/:projectId/experiments/:experimentId/run", options, async (request, reply) => {
    const params = experiment.parse(request.params);
    return reply.code(202).send(await coreClient.runExperiment(params.projectId, params.experimentId, context(request)));
  });
  app.post("/api/projects/:projectId/experiments/:experimentId/cancel", options, async (request, reply) => {
    const params = experiment.parse(request.params);
    return reply.code(202).send(await coreClient.cancelExperiment(params.projectId, params.experimentId, context(request)));
  });
  app.post("/api/projects/:projectId/experiments/:experimentId/archive", options, async (request, reply) => {
    const params = experiment.parse(request.params);
    return reply.code(202).send(await coreClient.archiveExperiment(params.projectId, params.experimentId, context(request)));
  });
  app.get("/api/projects/:projectId/experiments/:experimentId/logs", options, async (request) => {
    const params = experiment.parse(request.params);
    const query = z.object({ offset: z.coerce.number().int().min(0).optional(), limit: z.coerce.number().int().min(1).max(500).optional() }).parse(request.query);
    return coreClient.listExperimentLogs(params.projectId, params.experimentId, context(request), query);
  });
  app.get("/api/projects/:projectId/experiments/:experimentId/result", options, async (request) => {
    const params = experiment.parse(request.params);
    return coreClient.getExperimentResult(params.projectId, params.experimentId, context(request));
  });
  app.get("/api/projects/:projectId/boxes", options, async (request) => {
    const params = project.parse(request.params);
    return coreClient.listBoxes(params.projectId, context(request));
  });
  app.put("/api/projects/:projectId/box", options, async (request) => {
    const params = project.parse(request.params);
    const body = z.object({ box_id: z.string().uuid() }).parse(request.body);
    return coreClient.bindBox(params.projectId, body.box_id, context(request));
  });
  app.delete("/api/projects/:projectId/box", options, async (request, reply) => {
    const params = project.parse(request.params);
    await coreClient.unbindBox(params.projectId, context(request));
    return reply.code(204).send();
  });
}

function context(request: { browserIdentity?: { accessToken: string; userId: string }; currentProjectId?: string; id: string }) {
  return { accessToken: request.browserIdentity!.accessToken, projectId: request.currentProjectId, requestId: request.id, userId: request.browserIdentity!.userId };
}
