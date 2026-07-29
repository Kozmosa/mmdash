import { CliError } from "../errors.js";
import type { CliCommand } from "./registry.js";

export function createMembersCommand(options: {
  endpoint: string;
  token?: string;
  fetchImplementation?: typeof fetch;
}): CliCommand {
  return {
    name: "members",
    summary: "List project members and roles",
    usage: "mmdash members <project-id> [--json]",
    async run({ args, io }) {
      const projectId = args.find((arg) => !arg.startsWith("-"));
      if (!projectId)
        throw new CliError({
          code: "PROJECT_REQUIRED",
          exitCode: 2,
          message: "A project ID is required",
        });
      if (!options.token)
        throw new CliError({
          code: "AUTH_REQUIRED",
          exitCode: 2,
          message: "MMDASH_TOKEN is required",
        });
      const response = await (options.fetchImplementation ?? fetch)(
        `${options.endpoint.replace(/\/$/, "")}/box/v1/projects/${encodeURIComponent(projectId)}/members`,
        {
          headers: {
            accept: "application/json",
            authorization: `Bearer ${options.token}`,
          },
        },
      );
      if (!response.ok)
        throw new CliError({
          code: "REQUEST_FAILED",
          exitCode: 1,
          message: `Member request failed with HTTP ${response.status}`,
        });
      const result = (await response.json()) as {
        items: Array<{ display_name: string; email: string; role: string }>;
      };
      if (args.includes("--json")) io.stdout(JSON.stringify(result, null, 2));
      else
        io.stdout(
          result.items
            .map(
              (item) =>
                `${item.role.padEnd(12)} ${item.display_name} <${item.email}>`,
            )
            .join("\n"),
        );
      return 0;
    },
  };
}
