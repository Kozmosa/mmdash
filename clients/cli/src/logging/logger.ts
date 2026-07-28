export type LogLevel = "debug" | "error" | "info" | "silent" | "warn";

const levelPriority: Record<Exclude<LogLevel, "silent">, number> = {
  debug: 10,
  info: 20,
  warn: 30,
  error: 40,
};

export type LogWriter = (line: string) => void;

export class CliLogger {
  constructor(
    private readonly level: LogLevel,
    private readonly write: LogWriter,
    private readonly now: () => Date = () => new Date(),
  ) {}

  debug(event: string, fields: Record<string, unknown> = {}): void {
    this.log("debug", event, fields);
  }

  error(event: string, fields: Record<string, unknown> = {}): void {
    this.log("error", event, fields);
  }

  info(event: string, fields: Record<string, unknown> = {}): void {
    this.log("info", event, fields);
  }

  private log(
    level: Exclude<LogLevel, "silent">,
    event: string,
    fields: Record<string, unknown>,
  ): void {
    if (
      this.level === "silent" ||
      levelPriority[level] < levelPriority[this.level]
    ) {
      return;
    }
    this.write(
      JSON.stringify({
        event,
        level,
        timestamp: this.now().toISOString(),
        ...fields,
      }),
    );
  }
}

export function parseLogLevel(value: string | undefined): LogLevel {
  return value === "debug" ||
    value === "info" ||
    value === "warn" ||
    value === "error" ||
    value === "silent"
    ? value
    : "warn";
}
