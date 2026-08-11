import type { McpGatewayConfig } from "../src/config.js";

export const cliToken = "test-cli-token-that-is-at-least-32-characters";
export const agentToken = "test-agent-token-that-is-at-least-32-characters";

export const testConfig: McpGatewayConfig = {
  agentProjects: ["allowed-project"],
  agentToken,
  agentTools: ["system.echo"],
  allowedHosts: ["test.local", "127.0.0.1"],
  allowedOrigins: ["https://mmdash.moe"],
  cliProjects: ["*"],
  cliToken,
  cliTools: ["*"],
  coreBaseUrl: "http://core.test",
  host: "127.0.0.1",
  nodeEnv: "test",
  port: 3002,
  sessionTtlMs: 60_000,
  version: "0.1.0-test",
};
