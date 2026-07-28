import type { CoreClient } from "@mmdash/core-client";
import { McpServer } from "@modelcontextprotocol/server";

import type { Principal } from "../auth/token-authenticator.js";
import type { AuditSink } from "../audit/audit.js";
import type { GatewayAuthorizer } from "../authorization/authorizer.js";

export type ToolRegistrationContext = {
  audit: AuditSink;
  authorizer: GatewayAuthorizer;
  coreClient: CoreClient;
  now: () => number;
  principal: Principal;
  requestId: string;
  sessionId: string;
  version: string;
};

export type ToolModule = {
  name: string;
  register(server: McpServer, context: ToolRegistrationContext): void;
};

export class ToolRegistry {
  private readonly modules = new Map<string, ToolModule>();

  register(module: ToolModule): void {
    if (this.modules.has(module.name)) {
      throw new Error(`MCP tool "${module.name}" is already registered`);
    }
    this.modules.set(module.name, module);
  }

  createServer(context: ToolRegistrationContext): McpServer {
    const server = new McpServer(
      { name: "mmdash-mcp-gateway", version: context.version },
      {
        capabilities: { tools: {} },
        instructions:
          "Use project-scoped mmdash tools. Pass the target project_id explicitly and do not infer access.",
      },
    );
    for (const module of [...this.modules.values()].sort((a, b) =>
      a.name.localeCompare(b.name),
    )) {
      if (context.authorizer.canAccessTool(context.principal, module.name)) {
        module.register(server, context);
      }
    }
    return server;
  }

  list(): readonly string[] {
    return [...this.modules.keys()].sort();
  }
}
