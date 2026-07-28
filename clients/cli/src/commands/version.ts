import type { CliCommand } from "./registry.js";

export function createVersionCommand(version: string): CliCommand {
  return {
    name: "version",
    summary: "Show CLI version",
    usage: "mmdash version",
    run({ io }) {
      io.stdout(version);
      return 0;
    },
  };
}
