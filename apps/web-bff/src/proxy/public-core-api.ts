import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance, FastifyReply, FastifyRequest } from "fastify";
import { Readable } from "node:stream";

import { BffError } from "../errors/bff-error.js";

const publicOperations = new Set([
  "GET /v1/example",
  "POST /v1/auth/device/authorize",
  "POST /v1/auth/device/token",
  "POST /v1/auth/invitations/preview",
  "POST /v1/auth/invitations/reject",
  "POST /v1/auth/login",
  "POST /v1/auth/refresh",
  "POST /v1/auth/register",
  "POST /v1/boxes",
]);

const forwardedRequestHeaders = [
  "accept",
  "content-length",
  "content-range",
  "content-type",
  "idempotency-key",
  "if-match",
  "if-none-match",
  "last-event-id",
  "range",
  "x-github-delivery",
  "x-github-event",
  "x-hub-signature-256",
] as const;

const forwardedResponseHeaders = [
  "accept-ranges",
  "cache-control",
  "content-disposition",
  "content-length",
  "content-range",
  "content-type",
  "etag",
  "last-modified",
  "location",
  "retry-after",
] as const;

// registerPublicCoreApiProxy preserves the user/CLI API without publishing the
// Core process. It admits only public operations or first-class user/API
// identities; Agent and service credentials must use their dedicated private
// boundaries (MCP Gateway, Worker, or Box).
export function registerPublicCoreApiProxy(
  app: FastifyInstance,
  coreClient: CoreClient,
): void {
  app.route({
    config: { auth: "public", project: "none" },
    handler: async (request, reply) => {
      const target = targetPath(request);
      const path = new URL(target, "http://bff.local").pathname;
      const isPublic = isPublicOperation(request.method, path);
      const accessToken = isPublic
        ? undefined
        : isBoxControlOperation(request.method, path)
          ? requireBearer(request)
          : await requireUserOrApiToken(request, coreClient);
      const headers = new Headers();
      for (const name of forwardedRequestHeaders) {
        const value = readHeader(request, name);
        if (value) headers.set(name, value);
      }
      const hasBody = request.method !== "GET" && request.method !== "HEAD";
      const response = await coreClient.fetch(
        target,
        {
          body: hasBody ? (request.body as BodyInit) : undefined,
          duplex: hasBody ? "half" : undefined,
          headers,
          method: request.method,
        },
        { accessToken, requestId: request.id },
      );
      reply.code(response.status);
      for (const name of forwardedResponseHeaders) {
        const value = response.headers.get(name);
        if (value) reply.header(name, value);
      }
      return sendBody(reply, response);
    },
    method: ["DELETE", "GET", "HEAD", "PATCH", "POST", "PUT"],
    url: "/v1/*",
  });
}

async function requireUserOrApiToken(
  request: FastifyRequest,
  coreClient: CoreClient,
): Promise<string> {
  const authorization = readHeader(request, "authorization")?.trim() ?? "";
  const match = /^Bearer\s+(\S+)$/i.exec(authorization);
  if (!match) {
    throw new BffError({
      code: "UNAUTHENTICATED",
      message: "A user or API bearer token is required",
      status: 401,
    });
  }
  const accessToken = match[1]!;
  const identity = await coreClient.currentIdentity({
    accessToken,
    requestId: request.id,
  });
  if (identity.kind !== "session" && identity.kind !== "api") {
    throw new BffError({
      code: "PUBLIC_API_IDENTITY_FORBIDDEN",
      message: "This credential must use its dedicated service boundary",
      status: 403,
    });
  }
  return accessToken;
}

function isPublicOperation(method: string, path: string): boolean {
  if (publicOperations.has(`${method} ${path}`)) return true;
  if (/^\/v1\/artifact-transfers\/[^/]+$/.test(path)) return true;
  return (
    method === "POST" && /^\/v1\/repo\/webhooks\/github\/[^/]+$/.test(path)
  );
}

function isBoxControlOperation(method: string, path: string): boolean {
  if (method !== "POST") return false;
  return (
    /^\/v1\/boxes\/[^/]+\/heartbeat$/.test(path) ||
    /^\/v1\/boxes\/[^/]+\/tasks\/claim$/.test(path) ||
    /^\/v1\/boxes\/[^/]+\/tasks\/[^/]+\/(?:resume|logs|status|artifact|result)$/.test(
      path,
    )
  );
}

function requireBearer(request: FastifyRequest): string {
  const authorization = readHeader(request, "authorization")?.trim() ?? "";
  const match = /^Bearer\s+(\S+)$/i.exec(authorization);
  if (!match) {
    throw new BffError({
      code: "UNAUTHENTICATED",
      message: "A Box bearer token is required",
      status: 401,
    });
  }
  return match[1]!;
}

function targetPath(request: FastifyRequest): string {
  const url = new URL(request.raw.url ?? "", "http://bff.local");
  if (!url.pathname.startsWith("/v1/")) {
    throw new BffError({
      code: "INVALID_CORE_API_PATH",
      message: "The Core API path is invalid",
      status: 400,
    });
  }
  return `${url.pathname}${url.search}`;
}

function readHeader(request: FastifyRequest, name: string): string | undefined {
  const value = request.headers[name];
  return typeof value === "string" ? value : undefined;
}

function sendBody(reply: FastifyReply, response: Response) {
  if (!response.body || response.status === 204 || response.status === 304) {
    return reply.send();
  }
  return reply.send(
    Readable.from(response.body as unknown as AsyncIterable<Uint8Array>),
  );
}
