import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance, FastifyReply, FastifyRequest } from "fastify";
import { Readable } from "node:stream";
import { z } from "zod";

import { BffError } from "../errors/bff-error.js";

const projectParamsSchema = z.object({
  projectId: z.string(),
});
const fileParamsSchema = z.object({
  "*": z.string().min(1),
  projectId: z.string(),
});

const forwardedResponseHeaders = [
  "accept-ranges",
  "cache-control",
  "content-disposition",
  "content-length",
  "content-range",
  "content-type",
  "etag",
  "last-modified",
] as const;

export function registerHttpStreamRoutes(
  app: FastifyInstance,
  coreClient: CoreClient,
): void {
  app.get(
    "/api/projects/:projectId/events",
    { config: { auth: "required", project: "required" } },
    async (request, reply) => {
      const params = projectParamsSchema.parse(request.params);
      const query = new URL(request.raw.url ?? "", "http://bff.local").search;
      const response = await coreClient.fetch(
        `/v1/projects/${encodeURIComponent(params.projectId)}/events${query}`,
        {
          headers: {
            accept: "text/event-stream",
            "last-event-id": readHeader(request, "last-event-id") ?? "",
          },
          method: "GET",
        },
        coreContext(request),
      );
      if (!response.ok) {
        throw await proxyError(response);
      }
      reply.header("cache-control", "no-cache");
      reply.header("content-type", "text/event-stream");
      reply.header("x-accel-buffering", "no");
      return sendBody(reply, response);
    },
  );

  app.route({
    config: { auth: "required", project: "required" },
    handler: async (request, reply) => {
      const params = fileParamsSchema.parse(request.params);
      const filePath = normalizeFilePath(params["*"]);
      const headers = new Headers();
      for (const name of [
        "content-length",
        "content-range",
        "content-type",
        "if-match",
        "if-none-match",
        "range",
      ]) {
        const value = readHeader(request, name);
        if (value) {
          headers.set(name, value);
        }
      }

      const response = await coreClient.fetch(
        `/v1/projects/${encodeURIComponent(params.projectId)}/files/${filePath}`,
        {
          body:
            request.method === "PUT"
              ? (request.body as BodyInit)
              : undefined,
          duplex: request.method === "PUT" ? "half" : undefined,
          headers,
          method: request.method,
        },
        coreContext(request),
      );
      if (!response.ok) {
        throw await proxyError(response);
      }
      reply.code(response.status);
      copyResponseHeaders(response, reply);
      return request.method === "HEAD" ? reply.send() : sendBody(reply, response);
    },
    method: ["GET", "HEAD", "PUT"],
    url: "/api/projects/:projectId/files/*",
  });
}

function coreContext(request: FastifyRequest) {
  return {
    projectId: request.currentProjectId,
    requestId: request.id,
    userId: request.browserIdentity?.userId,
  };
}

function normalizeFilePath(filePath: string): string {
  const segments = filePath.split("/");
  if (segments.some((segment) => segment === "" || segment === "..")) {
    throw new BffError({
      code: "INVALID_FILE_PATH",
      message: "The file path is invalid",
      status: 400,
    });
  }
  return segments.map((segment) => encodeURIComponent(segment)).join("/");
}

function readHeader(
  request: FastifyRequest,
  name: string,
): string | undefined {
  const value = request.headers[name];
  return typeof value === "string" ? value : undefined;
}

function copyResponseHeaders(response: Response, reply: FastifyReply): void {
  for (const name of forwardedResponseHeaders) {
    const value = response.headers.get(name);
    if (value) {
      reply.header(name, value);
    }
  }
}

function sendBody(reply: FastifyReply, response: Response) {
  if (!response.body) {
    return reply.send();
  }
  return reply.send(
    Readable.from(
      response.body as unknown as AsyncIterable<Uint8Array>,
    ),
  );
}

async function proxyError(response: Response): Promise<BffError> {
  let code = "CORE_PROXY_FAILED";
  let message = "Core proxy request failed";
  if (response.headers.get("content-type")?.includes("application/json")) {
    try {
      const body = (await response.json()) as {
        code?: string;
        message?: string;
      };
      code = body.code ?? code;
      message = body.message ?? message;
    } catch {
      // Keep the safe proxy error.
    }
  }
  return new BffError({ code, message, status: response.status });
}
