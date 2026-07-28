export class GatewayError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, message: string, status: number) {
    super(message);
    this.name = "GatewayError";
    this.code = code;
    this.status = status;
  }
}

export function safeError(error: unknown): GatewayError {
  return error instanceof GatewayError
    ? error
    : new GatewayError(
        "INTERNAL_ERROR",
        "The tool could not complete the request",
        500,
      );
}
