import { spawnSync } from "node:child_process";

let resolved;

export function resolveComposeCommand() {
  if (resolved) return resolved;
  const configured = process.env.MMDASH_SMOKE_COMPOSE_COMMAND?.trim();
  const candidates = configured
    ? configured === "docker compose"
      ? [{ args: ["compose"], command: "docker" }]
      : configured === "docker-compose"
        ? [{ args: [], command: "docker-compose" }]
        : []
    : [
        { args: [], command: "docker-compose" },
        { args: ["compose"], command: "docker" },
      ];
  if (candidates.length === 0) {
    throw new Error(
      "MMDASH_SMOKE_COMPOSE_COMMAND must be 'docker-compose' or 'docker compose'.",
    );
  }
  for (const candidate of candidates) {
    const probe = spawnSync(candidate.command, [...candidate.args, "version"], {
      encoding: "utf8",
    });
    if (probe.status === 0) {
      resolved = candidate;
      return candidate;
    }
  }
  throw new Error(
    "Neither docker-compose nor the docker compose plugin is available.",
  );
}

export function runCompose(args, options = {}) {
  const compose = resolveComposeCommand();
  const result = spawnSync(
    compose.command,
    [...compose.args, "-f", "deploy/compose/compose.yaml", ...args],
    {
      encoding: "utf8",
      env: process.env,
      ...options,
    },
  );
  if (result.status !== 0) {
    throw new Error(
      `Compose command failed (${args.join(" ")}):\n${result.stdout ?? ""}\n${result.stderr ?? ""}`,
    );
  }
  return result;
}
