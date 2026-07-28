export class CliError extends Error {
  readonly code: string;
  readonly exitCode: number;
  readonly hint?: string;

  constructor(options: {
    code: string;
    exitCode?: number;
    hint?: string;
    message: string;
  }) {
    super(options.message);
    this.name = "CliError";
    this.code = options.code;
    this.exitCode = options.exitCode ?? 1;
    this.hint = options.hint;
  }
}

export function normalizeCliError(error: unknown): CliError {
  return error instanceof CliError
    ? error
    : new CliError({
        code: "INTERNAL_ERROR",
        message: "mmdash could not complete the command",
      });
}
