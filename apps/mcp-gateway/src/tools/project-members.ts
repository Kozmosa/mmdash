import type { CallToolResult } from "@modelcontextprotocol/server";
import { z } from "zod/v4";

import type { ToolModule, ToolRegistrationContext } from "./registry.js";

const projectInput = z.object({ project_id: z.string().uuid() });
const memberInput = projectInput.extend({ user_id: z.string().uuid() });

export const projectMemberListTool: ToolModule = {
  name: "project.member.list",
  register(server, context) {
    server.registerTool(
      this.name,
      {
        annotations: {
          destructiveHint: false,
          idempotentHint: true,
          readOnlyHint: true,
          title: "List project members",
        },
        description: "List members and roles for an authorized project.",
        inputSchema: projectInput,
      },
      async ({ project_id }) => executeList(context, project_id),
    );
  },
};

export const projectMemberGetTool: ToolModule = {
  name: "project.member.get",
  register(server, context) {
    server.registerTool(
      this.name,
      {
        annotations: {
          destructiveHint: false,
          idempotentHint: true,
          readOnlyHint: true,
          title: "Get project member",
        },
        description: "Read one project member and role.",
        inputSchema: memberInput,
      },
      async ({ project_id, user_id }) => {
        const result = await list(context, project_id);
        const member = result.items.find((item) => item.user_id === user_id);
        if (!member)
          return {
            content: [
              {
                type: "text",
                text: JSON.stringify({ code: "MEMBER_NOT_FOUND" }),
              },
            ],
            isError: true,
          };
        return {
          content: [{ type: "text", text: JSON.stringify(member) }],
          structuredContent: member,
        } as CallToolResult;
      },
    );
  },
};

async function executeList(
  context: ToolRegistrationContext,
  projectId: string,
): Promise<CallToolResult> {
  const result = await list(context, projectId);
  return {
    content: [{ type: "text", text: JSON.stringify(result) }],
    structuredContent: result,
  } as CallToolResult;
}

async function list(context: ToolRegistrationContext, projectId: string) {
  context.authorizer.assertProjectAccess(context.principal, projectId);
  if (!context.coreAccessToken)
    throw new Error("MCP Core access token is not configured");
  return context.coreClient.listProjectMembers(projectId, {
    accessToken: context.coreAccessToken,
    gatewayAccessToken: context.coreGatewayAccessToken,
    projectId,
    requestId: context.requestId,
  });
}
