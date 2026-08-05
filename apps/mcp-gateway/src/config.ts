import { z } from "zod";

const developmentCliToken = "development-cli-token-change-before-production";
const developmentAgentToken =
  "development-agent-token-change-before-production";

const configSchema = z.object({
  agentProjects: z.array(z.string().min(1)),
  agentToken: z.string().min(32).optional(),
  agentTools: z.array(z.string().min(1)),
  allowedHosts: z.array(z.string().min(1)),
  allowedOrigins: z.array(z.string().url()),
  cliProjects: z.array(z.string().min(1)),
  cliToken: z.string().min(32).optional(),
  cliTools: z.array(z.string().min(1)),
  coreBaseUrl: z.string().url(),
  coreAuditToken: z.string().min(32).optional(),
  coreAccessToken: z.string().min(32).optional(),
  host: z.string().min(1),
  nodeEnv: z.enum(["development", "test", "production"]),
  port: z.number().int().min(1).max(65_535),
  sessionTtlMs: z.number().int().min(60_000),
  version: z.string().min(1).max(100),
});

export type McpGatewayConfig = z.infer<typeof configSchema>;

export function loadConfig(
  environment: NodeJS.ProcessEnv = process.env,
): McpGatewayConfig {
  const nodeEnv = environment.NODE_ENV ?? "development";
  if (nodeEnv === "production" && environment.MCP_AGENT_TOKEN) {
    throw new Error(
      "MCP_AGENT_TOKEN is available only for development and tests",
    );
  }
  const config = configSchema.parse({
    agentProjects: parseList(environment.MCP_AGENT_PROJECTS, ["*"]),
    agentToken:
      nodeEnv === "production"
        ? undefined
        : environment.MCP_AGENT_TOKEN || developmentAgentToken,
    agentTools: parseList(environment.MCP_AGENT_TOOLS, ["*"]),
    allowedHosts: parseList(environment.MCP_ALLOWED_HOSTS, [
      "127.0.0.1",
      "localhost",
      "mmdash.moe",
    ]),
    allowedOrigins: parseList(environment.MCP_ALLOWED_ORIGINS, [
      "https://mmdash.moe",
      "http://localhost:3002",
    ]),
    cliProjects: parseList(environment.MCP_CLI_PROJECTS, ["*"]),
    cliToken:
      environment.MCP_CLI_TOKEN ||
      (nodeEnv === "production" ? undefined : developmentCliToken),
    cliTools: parseList(environment.MCP_CLI_TOOLS, ["*"]),
    coreBaseUrl: environment.CORE_BASE_URL ?? "http://localhost:8080",
    coreAuditToken: environment.MCP_CORE_AUDIT_TOKEN || undefined,
    coreAccessToken:
      environment.MCP_CORE_ACCESS_TOKEN ||
      environment.MCP_CORE_AUDIT_TOKEN ||
      undefined,
    host: environment.MCP_GATEWAY_HOST ?? "127.0.0.1",
    nodeEnv,
    port: Number(environment.MCP_GATEWAY_PORT ?? 3002),
    sessionTtlMs: Number(environment.MCP_SESSION_TTL_MS ?? 30 * 60_000),
    version: environment.MMDASH_VERSION ?? "0.1.0",
  });

  if (
    config.nodeEnv === "production" &&
    (config.cliToken === developmentCliToken ||
      config.agentToken === developmentAgentToken)
  ) {
    throw new Error("Production MCP tokens must be explicitly configured");
  }
  return config;
}

function parseList(value: string | undefined, fallback: string[]): string[] {
  return value
    ? value
        .split(",")
        .map((item) => item.trim())
        .filter(Boolean)
    : fallback;
}
