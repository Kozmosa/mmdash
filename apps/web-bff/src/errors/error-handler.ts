import { CoreClientError } from "@mmdash/core-client";
import type { FastifyInstance } from "fastify";
import { ZodError } from "zod";

import { BffError } from "./bff-error.js";

export function registerErrorHandler(app: FastifyInstance): void {
  app.setErrorHandler((error, request, reply) => {
    const mapped = mapError(error);
    if (mapped.status >= 500) {
      request.log.error({ err: error }, "request failed");
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
    if (error.status >= 500) {
      return {
        code: "CORE_UNAVAILABLE",
        message: "Core service is temporarily unavailable",
        status: 502,
      };
    }
    return {
      code: error.body.code ?? "CORE_REQUEST_FAILED",
      message: error.body.message ?? "Core request failed",
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
  if (
    typeof error === "object" &&
    error !== null &&
    "validation" in error
  ) {
    return {
      code: "VALIDATION_ERROR",
      message: "Request validation failed",
      status: 400,
    };
  }
  return {
    code: "INTERNAL_ERROR",
    message: "An unexpected error occurred",
    status: 500,
  };
}
