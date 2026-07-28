import type { CliCommand, CommandRegistry } from "./registry.js";

export function createHelpCommand(
  registry: CommandRegistry,
  version: string,
): CliCommand {
  return {
    name: "help",
    summary: "Show command help",
    usage: "mmdash help [command]",
    run({ args, io }) {
      const targetName = args[0];
      if (targetName) {
        const target = registry.get(targetName);
        if (!target) {
          io.stderr(`Unknown command: ${targetName}`);
          return 2;
        }
        io.stdout([target.usage, "", target.summary].join("\n"));
        return 0;
      }

      const commandLines = registry
        .list()
        .map((command) => `  ${command.name.padEnd(12)} ${command.summary}`);
      io.stdout(
        [
          `mmdash ${version}`,
          "",
          "Usage:",
          "  mmdash <command> [options]",
          "",
          "Commands:",
          ...commandLines,
          "",
          "Global options:",
          "  -h, --help     Show help",
          "  -v, --version  Show version",
          "",
          'Run "mmdash help <command>" for command details.',
        ].join("\n"),
      );
      return 0;
    },
  };
}
