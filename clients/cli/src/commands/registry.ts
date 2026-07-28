import { CliError } from "../errors.js";

export type CommandIo = {
  stderr(line: string): void;
  stdout(line: string): void;
};

export type CommandContext = {
  args: readonly string[];
  io: CommandIo;
};

export type CliCommand = {
  name: string;
  run(context: CommandContext): Promise<number> | number;
  summary: string;
  usage: string;
};

export class CommandRegistry {
  private readonly commands = new Map<string, CliCommand>();

  register(command: CliCommand): void {
    if (this.commands.has(command.name)) {
      throw new Error(`CLI command "${command.name}" is already registered`);
    }
    this.commands.set(command.name, command);
  }

  get(name: string): CliCommand | undefined {
    return this.commands.get(name);
  }

  list(): readonly CliCommand[] {
    return [...this.commands.values()].sort((a, b) =>
      a.name.localeCompare(b.name),
    );
  }

  async execute(
    name: string,
    args: readonly string[],
    io: CommandIo,
  ): Promise<number> {
    const command = this.get(name);
    if (!command) {
      throw new CliError({
        code: "UNKNOWN_COMMAND",
        exitCode: 2,
        hint: 'Run "mmdash help" to list commands.',
        message: `Unknown command: ${name}`,
      });
    }
    return command.run({ args, io });
  }
}
