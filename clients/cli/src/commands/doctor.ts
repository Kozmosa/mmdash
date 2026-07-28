import type { CliPaths } from "../config/paths.js";
import type { CliCommand } from "./registry.js";

export type DoctorDependencies = {
  endpoint: string;
  nodeVersion: string;
  paths: CliPaths;
  platform: NodeJS.Platform;
};

type DoctorCheck = {
  detail: string;
  id: string;
  ok: boolean;
};

export function createDoctorCommand(
  dependencies: DoctorDependencies,
): CliCommand {
  return {
    name: "doctor",
    summary: "Diagnose the local CLI foundation",
    usage: "mmdash doctor [--json]",
    run({ args, io }) {
      const checks = runChecks(dependencies);
      const ok = checks.every((check) => check.ok);
      if (args.includes("--json")) {
        io.stdout(JSON.stringify({ checks, ok }, null, 2));
      } else {
        io.stdout(
          [
            "mmdash doctor",
            ...checks.map(
              (check) =>
                `${check.ok ? "OK" : "FAIL"}  ${check.id}: ${check.detail}`,
            ),
          ].join("\n"),
        );
      }
      return ok ? 0 : 1;
    },
  };
}

function runChecks(dependencies: DoctorDependencies): DoctorCheck[] {
  const endpoint = parseEndpoint(dependencies.endpoint);
  return [
    {
      detail: dependencies.nodeVersion,
      id: "node",
      ok: supportsNode(dependencies.nodeVersion),
    },
    {
      detail: dependencies.platform,
      id: "platform",
      ok: ["aix", "darwin", "linux", "win32"].includes(dependencies.platform),
    },
    {
      detail: dependencies.paths.configDirectory,
      id: "config-directory",
      ok: dependencies.paths.configDirectory.length > 0,
    },
    {
      detail: dependencies.endpoint,
      id: "server-url",
      ok:
        endpoint !== null &&
        (endpoint.protocol === "https:" ||
          ["127.0.0.1", "localhost"].includes(endpoint.hostname)),
    },
  ];
}

function parseEndpoint(value: string): URL | null {
  try {
    return new URL(value);
  } catch {
    return null;
  }
}

function supportsNode(version: string): boolean {
  const match = version.match(/^v?(\d+)\.(\d+)\.(\d+)/);
  if (!match) {
    return false;
  }
  const major = Number(match[1]);
  const minor = Number(match[2]);
  return major > 20 || (major === 20 && minor >= 9);
}
