import type { FastifyInstance, FastifyRequest } from "fastify";

import { BffError } from "../errors/bff-error.js";

const projectIdPattern = /^[A-Za-z0-9][A-Za-z0-9_-]{1,127}$/;

export function registerProjectContext(app: FastifyInstance): void {
  app.decorateRequest("currentProjectId");
  app.addHook("preHandler", async (request) => {
    const requirement = request.routeOptions.config.project ?? "none";
    if (requirement === "none") {
      return;
    }

    const candidates = collectCandidates(request);
    const unique = [...new Set(candidates)];
    if (unique.length > 1) {
      throw new BffError({
        code: "PROJECT_CONTEXT_CONFLICT",
        message: "Conflicting project identifiers were provided",
        status: 400,
      });
    }

    const projectId = unique[0];
    if (!projectId && requirement === "required") {
      throw new BffError({
        code: "PROJECT_CONTEXT_REQUIRED",
        message: "A project identifier is required",
        status: 400,
      });
    }
    if (projectId && !projectIdPattern.test(projectId)) {
      throw new BffError({
        code: "INVALID_PROJECT_ID",
        message: "The project identifier is invalid",
        status: 400,
      });
    }
    request.currentProjectId = projectId;
  });
}

function collectCandidates(request: FastifyRequest): string[] {
  const params = asRecord(request.params);
  const query = asRecord(request.query);
  const header = request.headers["x-mmdash-project-id"];
  return [
    readString(params.projectId),
    readString(query.project_id),
    typeof header === "string" ? header : undefined,
  ].filter((value): value is string => Boolean(value));
}

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null
    ? (value as Record<string, unknown>)
    : {};
}

function readString(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}
