import type { CoreClient } from "@mmdash/core-client";
import type { components } from "@mmdash/core-client/generated";
import type { FastifyInstance } from "fastify";
import { Readable } from "node:stream";
import { z } from "zod";

const project = z.object({ projectId: z.string().uuid() });
const experiment = project.extend({ experimentId: z.string().uuid() });
const createExperiment = z.object({
  name: z.string().trim().min(1).max(200),
  experiment_type: z.enum(["box", "self"]),
  source_commit: z.string().regex(/^[0-9a-f]{40}([0-9a-f]{24})?$/),
  entrypoint: z
    .string()
    .regex(/^(python3?|node|go|binary):[a-zA-Z0-9_./-]+$/)
    .refine(
      (value) =>
        !value
          .split(":", 2)[1]!
          .split("/")
          .some((part) => part === "." || part === ".."),
      "entrypoint path must be normalized",
    ),
  parameters: z.record(z.string(), z.unknown()),
  environment: z.record(z.string(), z.string()),
  inputs: z.record(z.string(), z.unknown()),
  runtime_policy: z.enum(["auto", "e2b", "local-docker"]).optional(),
  requested_box_id: z.string().uuid().optional(),
  limits_override: z
    .object({
      cpu_millis: z.number().int().positive(),
      memory_bytes: z.number().int().positive(),
      timeout_seconds: z.number().int().positive(),
      disk_bytes: z.number().int().positive(),
      pids: z.number().int().positive(),
      network: z.enum(["disabled", "restricted", "enabled"]),
    })
    .optional(),
  idempotency_key: z.string().trim().min(1).max(200),
});
const runExperiment = z.object({
  idempotency_key: z.string().trim().min(1).max(200),
});
const experimentSettings = z.object({
  timezone: z.string().trim().min(1).max(100),
  default_runtime_policy: z.enum(["auto", "e2b", "local-docker"]),
  default_limits: z.object({
    cpu_millis: z.number().int().positive(),
    memory_bytes: z.number().int().positive(),
    timeout_seconds: z.number().int().positive(),
    disk_bytes: z.number().int().positive(),
    pids: z.number().int().positive(),
    network: z.enum(["disabled", "restricted", "enabled"]),
  }),
  git_large_file_threshold_bytes: z.number().int().positive(),
});
const rerunExperiment = createExperiment
  .partial()
  .omit({ experiment_type: true })
  .extend({
    idempotency_key: z.string().trim().min(1).max(200),
  });
const box = project.extend({ boxId: z.string().uuid() });
const personalBox = z.object({ boxId: z.string().uuid() });

export function registerExperimentRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
): void {
  const options = {
    config: { auth: "required" as const, project: "required" as const },
  };
  app.get("/api/projects/:projectId/experiments", options, async (request) => {
    const params = project.parse(request.params);
    const query = z
      .object({
        cursor: z.string().optional(),
        limit: z.coerce.number().int().min(1).max(200).optional(),
        status: z.string().optional(),
      })
      .parse(request.query);
    return coreClient.listExperiments(
      params.projectId,
      context(request),
      query,
    );
  });
  app.post(
    "/api/projects/:projectId/experiments",
    options,
    async (request, reply) => {
      const params = project.parse(request.params);
      const value = await coreClient.createExperiment(
        params.projectId,
        createExperiment.parse(
          request.body,
        ) satisfies components["schemas"]["CreateExperimentRequest"],
        context(request),
      );
      return reply.code(201).send(value);
    },
  );
  app.get(
    "/api/projects/:projectId/experiments/settings",
    options,
    async (request) => {
      const params = project.parse(request.params);
      return coreClient.getExperimentSettings(
        params.projectId,
        context(request),
      );
    },
  );
  app.patch(
    "/api/projects/:projectId/experiments/settings",
    options,
    async (request) => {
      const params = project.parse(request.params);
      return coreClient.updateExperimentSettings(
        params.projectId,
        experimentSettings.parse(
          request.body,
        ) satisfies components["schemas"]["UpdateExperimentSettingsRequest"],
        context(request),
      );
    },
  );
  app.get(
    "/api/projects/:projectId/experiments/compare",
    options,
    async (request) => {
      const params = project.parse(request.params);
      const query = z
        .object({
          experiment_id: z.union([z.string(), z.array(z.string())]),
        })
        .parse(request.query);
      const ids = Array.isArray(query.experiment_id)
        ? query.experiment_id
        : query.experiment_id.split(",").filter(Boolean);
      return coreClient.compareExperiments(
        params.projectId,
        ids,
        context(request),
      );
    },
  );
  app.get(
    "/api/projects/:projectId/experiments/:experimentId",
    options,
    async (request) => {
      const params = experiment.parse(request.params);
      return coreClient.getExperiment(
        params.projectId,
        params.experimentId,
        context(request),
      );
    },
  );
  app.post(
    "/api/projects/:projectId/experiments/:experimentId/run",
    options,
    async (request, reply) => {
      const params = experiment.parse(request.params);
      return reply
        .code(202)
        .send(
          await coreClient.runExperiment(
            params.projectId,
            params.experimentId,
            runExperiment.parse(
              request.body,
            ) satisfies components["schemas"]["RunExperimentRequest"],
            context(request),
          ),
        );
    },
  );
  app.post(
    "/api/projects/:projectId/experiments/:experimentId/rerun",
    options,
    async (request, reply) => {
      const params = experiment.parse(request.params);
      return reply
        .code(201)
        .send(
          await coreClient.rerunExperiment(
            params.projectId,
            params.experimentId,
            rerunExperiment.parse(
              request.body,
            ) satisfies components["schemas"]["RerunExperimentRequest"],
            context(request),
          ),
        );
    },
  );
  app.post(
    "/api/projects/:projectId/experiments/:experimentId/result/bind",
    options,
    async (request, reply) => {
      const params = experiment.parse(request.params);
      const body = z
        .object({
          commit_sha: z.string().regex(/^[0-9a-f]{40}([0-9a-f]{24})?$/),
          idempotency_key: z.string().trim().min(1).max(200),
        })
        .parse(request.body);
      return reply
        .code(202)
        .send(
          await coreClient.bindExperimentResult(
            params.projectId,
            params.experimentId,
            body,
            context(request),
          ),
        );
    },
  );
  app.post(
    "/api/projects/:projectId/experiments/:experimentId/cancel",
    options,
    async (request, reply) => {
      const params = experiment.parse(request.params);
      return reply
        .code(202)
        .send(
          await coreClient.cancelExperiment(
            params.projectId,
            params.experimentId,
            context(request),
          ),
        );
    },
  );
  app.post(
    "/api/projects/:projectId/experiments/:experimentId/archive",
    options,
    async (request, reply) => {
      const params = experiment.parse(request.params);
      return reply
        .code(202)
        .send(
          await coreClient.archiveExperiment(
            params.projectId,
            params.experimentId,
            context(request),
          ),
        );
    },
  );
  app.get(
    "/api/projects/:projectId/experiments/:experimentId/logs",
    options,
    async (request) => {
      const params = experiment.parse(request.params);
      const query = z
        .object({
          cursor: z.string().regex(/^\d+$/).optional(),
          limit: z.coerce.number().int().min(1).max(500).optional(),
          tail: z.enum(["true", "false"]).optional(),
        })
        .parse(request.query);
      return coreClient.listExperimentLogs(
        params.projectId,
        params.experimentId,
        context(request),
        {
          cursor: query.cursor,
          limit: query.limit,
          tail: query.tail === undefined ? undefined : query.tail === "true",
        },
      );
    },
  );
  app.get(
    "/api/projects/:projectId/experiments/:experimentId/logs/stream",
    options,
    async (request, reply) => {
      const params = experiment.parse(request.params);
      const query = z
        .object({ after_sequence: z.coerce.number().int().min(0).default(0) })
        .parse(request.query);
      const abort = new AbortController();
      request.raw.once("aborted", () => abort.abort());
      reply.raw.once("close", () => abort.abort());
      reply.header("cache-control", "no-cache, no-transform");
      reply.header("content-type", "text/event-stream; charset=utf-8");
      reply.header("x-accel-buffering", "no");
      const stream = Readable.from(
        streamExperimentLogs(
          coreClient,
          params.projectId,
          params.experimentId,
          context(request),
          query.after_sequence,
          abort.signal,
        ),
      );
      return reply.send(stream);
    },
  );
  app.get(
    "/api/projects/:projectId/experiments/:experimentId/result",
    options,
    async (request) => {
      const params = experiment.parse(request.params);
      return coreClient.getExperimentResult(
        params.projectId,
        params.experimentId,
        context(request),
      );
    },
  );
  app.get("/api/projects/:projectId/boxes", options, async (request) => {
    const params = project.parse(request.params);
    return coreClient.listProjectBoxes(params.projectId, context(request));
  });
  app.put("/api/projects/:projectId/boxes/:boxId", options, async (request) => {
    const params = box.parse(request.params);
    return coreClient.assignProjectBox(
      params.projectId,
      params.boxId,
      context(request),
    );
  });
  app.delete(
    "/api/projects/:projectId/boxes/:boxId",
    options,
    async (request, reply) => {
      const params = box.parse(request.params);
      const query = z
        .object({ force: z.enum(["true", "false"]).optional() })
        .parse(request.query);
      await coreClient.removeProjectBox(
        params.projectId,
        params.boxId,
        query.force === "true",
        context(request),
      );
      return reply.code(204).send();
    },
  );

  const personalOptions = {
    config: { auth: "required" as const, project: "none" as const },
  };
  app.get("/api/users/me/boxes", personalOptions, async (request) =>
    coreClient.listPersonalBoxes(context(request)),
  );
  app.get("/api/users/me/boxes/:boxId", personalOptions, async (request) => {
    const params = personalBox.parse(request.params);
    return coreClient.getPersonalBox(params.boxId, context(request));
  });
  app.patch("/api/users/me/boxes/:boxId", personalOptions, async (request) => {
    const params = personalBox.parse(request.params);
    const body = z
      .object({ name: z.string().trim().min(1).max(200) })
      .parse(request.body);
    return coreClient.updatePersonalBox(params.boxId, body, context(request));
  });
  app.post(
    "/api/users/me/boxes/:boxId/revoke",
    personalOptions,
    async (request, reply) => {
      const params = personalBox.parse(request.params);
      const body = z
        .object({ mode: z.enum(["drain", "force"]) })
        .parse(request.body);
      return reply
        .code(202)
        .send(
          await coreClient.revokePersonalBox(
            params.boxId,
            body,
            context(request),
          ),
        );
    },
  );
}

async function* streamExperimentLogs(
  coreClient: CoreClient,
  projectId: string,
  experimentId: string,
  requestContext: ReturnType<typeof context>,
  afterSequence: number,
  signal: AbortSignal,
): AsyncGenerator<string> {
  let cursor = afterSequence;
  while (!signal.aborted) {
    const page = await coreClient.listExperimentLogs(
      projectId,
      experimentId,
      requestContext,
      {
        cursor: String(cursor),
        limit: 500,
      },
    );
    for (const entry of page.items) {
      cursor = Math.max(cursor, entry.sequence);
      yield `id: ${entry.sequence}\nevent: log\ndata: ${JSON.stringify(entry)}\n\n`;
    }
    if (page.has_more && page.items.length > 0) {
      continue;
    }
    yield ": keepalive\n\n";
    await waitForLogPoll(signal, 1_000);
  }
}

async function waitForLogPoll(
  signal: AbortSignal,
  delay: number,
): Promise<void> {
  await new Promise<void>((resolve) => {
    if (signal.aborted) {
      resolve();
      return;
    }
    const timeout = setTimeout(done, delay);
    signal.addEventListener("abort", done, { once: true });
    function done() {
      clearTimeout(timeout);
      signal.removeEventListener("abort", done);
      resolve();
    }
  });
}

function context(request: {
  browserIdentity?: { accessToken: string; userId: string };
  currentProjectId?: string;
  id: string;
}) {
  return {
    accessToken: request.browserIdentity!.accessToken,
    projectId: request.currentProjectId,
    requestId: request.id,
    userId: request.browserIdentity!.userId,
  };
}
