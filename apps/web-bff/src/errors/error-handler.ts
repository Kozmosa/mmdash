import { CoreClientError } from "@mmdash/core-client";
import type { FastifyInstance } from "fastify";
import { ZodError } from "zod";

import { BffError } from "./bff-error.js";

export function registerErrorHandler(app: FastifyInstance): void {
  app.setErrorHandler((error, request, reply) => {
    const mapped = mapError(error);
    if (mapped.status >= 500) {
      request.log.error(
        {
          code: mapped.code,
          error_name: errorName(error),
          request_id: request.id,
          status: mapped.status,
          ...coreFailureContext(error),
        },
        "request failed",
      );
    } else {
      request.log.info(
        { code: mapped.code, status: mapped.status },
        "request rejected",
      );
    }

    return reply.code(mapped.status).send({
      code: mapped.code,
      message: mapped.message,
      request_id: request.id,
      ...(mapped.details === undefined ? {} : { details: mapped.details }),
    });
  });
}

function coreFailureContext(error: unknown): Record<string, unknown> {
  if (!(error instanceof CoreClientError)) return {};
  return {
    core_code: error.body.code ?? "CORE_REQUEST_FAILED",
    core_request_id: error.body.request_id,
    core_status: error.status,
  };
}

function mapError(error: unknown): {
  code: string;
  details?: unknown;
  message: string;
  status: number;
} {
  if (error instanceof BffError) {
    return error;
  }
  if (error instanceof CoreClientError) {
    const code = error.body.code ?? "CORE_REQUEST_FAILED";
    if (safeRepoTransientCodes.has(code)) {
      return {
        code,
        details: error.body.details,
        message: safeCoreMessage(error.body.message),
        status: error.status,
      };
    }
    if (error.status >= 500) {
      return {
        code: "CORE_UNAVAILABLE",
        message: "Core service is temporarily unavailable",
        status: 502,
      };
    }
    return {
      code,
      details: error.body.details,
      message: safeCoreMessage(error.body.message),
      status: error.status,
    };
  }
  if (error instanceof ZodError) {
    return {
      code: "VALIDATION_ERROR",
      details: error.issues.map((issue) => ({
        message: issue.message,
        path: issue.path.join("."),
      })),
      message: "Request validation failed",
      status: 400,
    };
  }
  if (typeof error === "object" && error !== null && "validation" in error) {
    return {
      code: "VALIDATION_ERROR",
      message: "Request validation failed",
      status: 400,
    };
  }
  if (
    typeof error === "object" &&
    error !== null &&
    "statusCode" in error &&
    error.statusCode === 413
  ) {
    return {
      code: "PAYLOAD_TOO_LARGE",
      message: "Request payload is too large",
      status: 413,
    };
  }
  return {
    code: "INTERNAL_ERROR",
    message: "An unexpected error occurred",
    status: 500,
  };
}

const safeRepoTransientCodes = new Set([
  "REPO_GIT_TIMEOUT",
  "REPO_NETWORK_UNAVAILABLE",
  "REPO_PROVIDER_TEMPORARILY_UNAVAILABLE",
  "REPO_PROVIDER_RESPONSE_INVALID",
]);

const sensitiveMessagePattern =
  /(?:api[_ -]?key|authorization|bearer|cloudflare|credential|dashboard[_ -]?(?:session[_ -]?)?token|hermes[_ -]?api[_ -]?key|refresh[_ -]?token|secret|token[_ -]?hash)/i;

function safeCoreMessage(message: string | undefined): string {
  if (!message || sensitiveMessagePattern.test(message)) {
    return "Core rejected the request";
  }
  return message.slice(0, 1_000);
}

function errorName(error: unknown): string {
  return error instanceof Error ? error.name : "UnknownError";
}
