import { describe, expect, it, vi } from "vitest";

import { CoreAuditSink } from "../src/audit/audit.js";

describe("CoreAuditSink", () => {
  it("maps MCP principals to a queryable Core audit event", async () => {
    const recordAuditEvent = vi.fn().mockResolvedValue({});
    const sink = new CoreAuditSink(
      { recordAuditEvent } as never,
      "audit-token-with-at-least-thirty-two-characters",
    );

    await sink.record({
      actorId: "agent:0123456789abcdef",
      actorKind: "agent",
      durationMs: 12,
      occurredAt: "2026-07-28T12:00:00Z",
      outcome: "success",
      projectId: "project-1",
      requestId: "request-1",
      sessionId: "session-1",
      toolName: "system.echo",
    });

    expect(recordAuditEvent).toHaveBeenCalledWith(
      expect.objectContaining({
        action: "mcp.tool.called",
        category: "mcp",
        metadata: expect.objectContaining({
          delegated_actor_id: "agent:0123456789abcdef",
          delegated_actor_kind: "agent",
        }),
        project_id: "project-1",
        resource_id: "system.echo",
        source: "mcp-gateway",
      }),
      {
        accessToken: "audit-token-with-at-least-thirty-two-characters",
        projectId: "project-1",
        requestId: "request-1",
      },
    );
  });
});
