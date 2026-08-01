import type { TokenKind } from "../auth/token-authenticator.js";

export type AuditOutcome = "denied" | "error" | "success";

export type ToolAuditEvent = {
  actorId: string;
  actorKind: TokenKind;
  durationMs: number;
  errorCode?: string;
  occurredAt: string;
  outcome: AuditOutcome;
  projectId?: string;
  requestId: string;
  sessionId: string;
  toolName: string;
};

export interface AuditSink {
  record(event: ToolAuditEvent): Promise<void>;
}

export class JsonAuditSink implements AuditSink {
  async record(event: ToolAuditEvent): Promise<void> {
    process.stderr.write(
      `${JSON.stringify({ event: "mcp.tool.called", ...event })}\n`,
    );
  }
}

export class MemoryAuditSink implements AuditSink {
  readonly events: ToolAuditEvent[] = [];

  async record(event: ToolAuditEvent): Promise<void> {
    this.events.push(event);
  }
}

export class CoreAuditSink implements AuditSink {
  constructor(
    private readonly client: CoreClient,
    private readonly accessToken: string,
  ) {}

  async record(event: ToolAuditEvent): Promise<void> {
    await this.client.recordAuditEvent(
      {
        action: "mcp.tool.called",
        category: "mcp",
        duration_ms: event.durationMs,
        error_code: event.errorCode,
        metadata: {
          delegated_actor_id: event.actorId,
          delegated_actor_kind: event.actorKind,
          gateway_session_id: event.sessionId,
        },
        occurred_at: event.occurredAt,
        outcome: event.outcome,
        project_id: event.projectId,
        resource_id: event.toolName,
        resource_type: "mcp-tool",
        source: "mcp-gateway",
      },
      {
        accessToken: this.accessToken,
        projectId: event.projectId,
        requestId: event.requestId,
      },
    );
  }
}

export class CompositeAuditSink implements AuditSink {
  constructor(private readonly sinks: readonly AuditSink[]) {}

  async record(event: ToolAuditEvent): Promise<void> {
    for (const sink of this.sinks) {
      await sink.record(event);
    }
  }
}
import type { CoreClient } from "@mmdash/core-client";
