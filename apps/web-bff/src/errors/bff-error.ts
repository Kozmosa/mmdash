export class BffError extends Error {
  readonly code: string;
  readonly details?: unknown;
  readonly status: number;

  constructor(options: {
    code: string;
    details?: unknown;
    message: string;
    status: number;
  }) {
    super(options.message);
    this.name = "BffError";
    this.code = options.code;
    this.details = options.details;
    this.status = options.status;
  }
}
