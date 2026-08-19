import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance, FastifyReply } from "fastify";
import { z } from "zod";

import type { ArticleCollaboration } from "../article/collaboration.js";
import { roomContext } from "../article/collaboration.js";

const identifier = z.string().uuid();
const project = z.object({ projectId: identifier });
const resource = project.extend({ resourceId: identifier });
const objectBody = z.record(z.string(), z.unknown());
const options = {
  config: { auth: "required" as const, project: "required" as const },
};

export function registerArticleRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
  collaboration: ArticleCollaboration,
): void {
  const collection = (
    name: string,
    methods: ("GET" | "POST" | "PUT" | "DELETE")[],
  ) => {
    for (const method of methods) {
      app.route({
        ...options,
        method,
        url: `/api/projects/:projectId/article/${name}`,
        handler: async (request, reply) => {
          const { projectId } = project.parse(request.params);
          if (name === "draft/flush" && method === "POST") {
            return collaboration.flush(await roomContext(coreClient, request));
          }
          let body: Record<string, unknown> | undefined;
          if (
            method === "POST" &&
            ["commits", "preview-builds", "publications"].includes(name)
          ) {
            const draft = await collaboration.flush(
              await roomContext(coreClient, request),
            );
            body = {
              ...objectBody.parse(request.body ?? {}),
              draft_revision: draft.draft_revision,
            };
          }
          return proxy(
            coreClient,
            request,
            reply,
            projectId,
            `/${name}`,
            method,
            body,
          );
        },
      });
    }
  };
  app.get(
    "/api/projects/:projectId/article",
    options,
    async (request, reply) => {
      const { projectId } = project.parse(request.params);
      return proxy(coreClient, request, reply, projectId, "", "GET");
    },
  );
  collection("draft", ["GET"]);
  collection("draft/flush", ["POST"]);
  collection("patches", ["GET", "POST"]);
  collection("references", ["GET", "POST"]);
  collection("commits", ["GET", "POST"]);
  collection("builds", ["GET", "POST"]);
  collection("preview-builds", ["POST"]);
  collection("publications", ["POST"]);
  collection("releases", ["GET", "POST"]);
  collection("templates", ["GET", "POST"]);
  collection("zotero", ["GET", "PUT", "DELETE"]);
  collection("zotero/search", ["GET"]);
  for (const [name, suffix, methods] of [
    ["blocks", "review", ["POST"]],
    ["patches", "review", ["POST"]],
    ["commits", "", ["GET"]],
    ["commits", "restore", ["POST"]],
    ["builds", "", ["GET"]],
    ["builds", "retry", ["POST"]],
    ["publications", "retry", ["POST"]],
    ["releases", "", ["GET"]],
    ["references", "", ["DELETE"]],
  ] as const) {
    for (const method of methods) {
      app.route({
        ...options,
        method,
        url: `/api/projects/:projectId/article/${name}/:resourceId${suffix ? `/${suffix}` : ""}`,
        handler: async (request, reply) => {
          const parsed = resource.parse(request.params);
          return proxy(
            coreClient,
            request,
            reply,
            parsed.projectId,
            `/${name}/${encodeURIComponent(parsed.resourceId)}${suffix ? `/${suffix}` : ""}`,
            method,
          );
        },
      });
    }
  }
}

async function proxy(
  coreClient: CoreClient,
  request: {
    body?: unknown;
    browserIdentity?: { accessToken: string; userId: string };
    id: string;
    query?: unknown;
  },
  reply: FastifyReply,
  projectId: string,
  suffix: string,
  method: "GET" | "POST" | "PUT" | "DELETE",
  bodyOverride?: Record<string, unknown>,
): Promise<unknown> {
  const query = new URLSearchParams();
  if (typeof request.query === "object" && request.query !== null) {
    for (const [key, value] of Object.entries(request.query)) {
      if (typeof value === "string") query.set(key, value);
    }
  }
  const value = await coreClient.request(
    `/v1/projects/${encodeURIComponent(projectId)}/article${suffix}${query.size ? `?${query}` : ""}`,
    {
      body:
        method === "GET" || method === "DELETE"
          ? undefined
          : (bodyOverride ?? objectBody.parse(request.body ?? {})),
      method: method === "POST" && suffix === "/draft/flush" ? "PUT" : method,
    },
    {
      accessToken: request.browserIdentity!.accessToken,
      projectId,
      requestId: request.id,
      userId: request.browserIdentity!.userId,
    },
  );
  if (method === "DELETE") return reply.code(204).send();
  if (
    method === "POST" &&
    ["/builds", "/preview-builds", "/publications", "/templates"].includes(
      suffix,
    )
  ) {
    return reply.code(202).send(value);
  }
  if (
    method === "POST" &&
    ["/commits", "/references", "/patches", "/releases"].includes(suffix)
  ) {
    return reply.code(201).send(value);
  }
  return value;
}
