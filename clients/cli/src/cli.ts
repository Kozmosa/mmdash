import { createDoctorCommand } from "./commands/doctor.js";
import { createHelpCommand } from "./commands/help.js";
import { CommandRegistry, type CommandIo } from "./commands/registry.js";
import { createVersionCommand } from "./commands/version.js";
import { resolveCliPaths } from "./config/paths.js";
import { normalizeCliError } from "./errors.js";
import { CliLogger, parseLogLevel } from "./logging/logger.js";

export type RunCliOptions = {
  environment?: NodeJS.ProcessEnv;
  io?: CommandIo;
  nodeVersion?: string;
  platform?: NodeJS.Platform;
  registry?: CommandRegistry;
  version: string;
};

export async function runCli(
  args: readonly string[],
  options: RunCliOptions,
): Promise<number> {
  const environment = options.environment ?? process.env;
  const io = options.io ?? processIo;
  const logger = new CliLogger(
    parseLogLevel(environment.MMDASH_LOG_LEVEL),
    io.stderr,
  );
  const registry =
    options.registry ??
    createDefaultRegistry({
      endpoint: environment.MMDASH_URL ?? "https://mmdash.com",
      environment,
      nodeVersion: options.nodeVersion ?? process.version,
      platform: options.platform ?? process.platform,
      version: options.version,
    });
  const normalized = normalizeGlobalAliases(args);
  const commandName = normalized[0] ?? "help";
  const commandArgs = normalized.slice(1);

  logger.debug("cli.command.started", { command: commandName });
  try {
    const exitCode = await registry.execute(commandName, commandArgs, io);
    logger.debug("cli.command.completed", {
      command: commandName,
      exitCode,
    });
    return exitCode;
  } catch (error) {
    const safe = normalizeCliError(error);
    logger.error("cli.command.failed", {
      code: safe.code,
      command: commandName,
    });
    io.stderr(`[${safe.code}] ${safe.message}`);
    if (safe.hint) {
      io.stderr(`Hint: ${safe.hint}`);
    }
    return safe.exitCode;
  }
}

export function createDefaultRegistry(options: {
  endpoint: string;
  environment: NodeJS.ProcessEnv;
  nodeVersion: string;
  platform: NodeJS.Platform;
  version: string;
}): CommandRegistry {
  const registry = new CommandRegistry();
  registry.register(
    createDoctorCommand({
      endpoint: options.endpoint,
      nodeVersion: options.nodeVersion,
      paths: resolveCliPaths({
        environment: options.environment,
        platform: options.platform,
      }),
      platform: options.platform,
    }),
  );
  registry.register(createVersionCommand(options.version));
  registry.register(createHelpCommand(registry, options.version));
  return registry;
}

function normalizeGlobalAliases(args: readonly string[]): string[] {
  if (args.length === 0 || args[0] === "-h" || args[0] === "--help") {
    return ["help"];
  }
  if (args[0] === "-v" || args[0] === "--version") {
    return ["version"];
  }
  if (args.length === 2 && (args[1] === "-h" || args[1] === "--help")) {
    return ["help", args[0]!];
  }
  return [...args];
}

const processIo: CommandIo = {
  stderr(line) {
    process.stderr.write(`${line}\n`);
  },
  stdout(line) {
    process.stdout.write(`${line}\n`);
  },
};
