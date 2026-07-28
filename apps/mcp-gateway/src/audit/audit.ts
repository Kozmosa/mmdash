import type { TokenKind } from "../auth/token-authenticator.js";

export type AuditOutcome = "denied" | "error" | "success";

export type ToolAuditEvent = {
  actorId: string;
  actorKind: TokenKind;
  durationMs: number;
  errorCode?: string;
  occurredAt: string;
  outcome: AuditOutcome;
  projectId: string;
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
