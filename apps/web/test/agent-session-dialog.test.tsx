import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AgentSessionDialog } from "@/features/agent/agent-session-dialog";

const mocks = vi.hoisted(() => ({
  getRun: vi.fn(),
  listMessages: vi.fn(),
  request: vi.fn(),
  stream: vi.fn(),
}));

vi.mock("@/features/agent/agent-api", () => ({
  agentApi: {
    getRun: mocks.getRun,
    listMessages: mocks.listMessages,
  },
  streamAgentRun: mocks.stream,
}));
vi.mock("@/features/artifact/artifact-detail-drawer", () => ({
  ArtifactDetailDrawer: () => null,
}));
vi.mock("@/lib/api-client", () => ({
  apiClient: { request: mocks.request },
}));

function renderDialog(
  props: Partial<ComponentProps<typeof AgentSessionDialog>> = {},
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <AgentSessionDialog
      agentInstanceId="agent-1"
      evaluationId="evaluation-1"
      evaluationStatus="succeeded"
      onClose={vi.fn()}
      open
      projectId="project-1"
      runId="run-1"
      sessionId="session-1"
      {...props}
    />,
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    },
  );
}

describe("automatic evaluation Session dialog", () => {
  beforeEach(() => {
    mocks.request.mockResolvedValue({
      agent_instance_id: "agent-1",
      agent_run_id: "run-1",
      agent_session_id: "session-1",
      blockers: [],
      changes_since_last: [],
      completed_items: [],
      detected_stage: "执行阶段",
      evaluation_id: "evaluation-1",
      in_progress_items: [],
      pending_questions: [],
      project_id: "project-1",
      risks: [],
      status: "succeeded",
      summary: "评估完成，项目处于执行阶段。",
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("renders Agent output and tool calls without a message input", async () => {
    mocks.listMessages.mockResolvedValue({
      items: [
        {
          content: "评估完成，项目处于执行阶段。",
          message_id: "message-1",
          role: "assistant",
        },
      ],
    });
    mocks.getRun.mockResolvedValue({
      created_at: "2026-08-11T08:00:00Z",
      remote_run_id: "remote-1",
      run_id: "run-1",
      session_id: "session-1",
      source: "progress_evaluation",
      status: "completed",
      tool_calls: [
        {
          name: "progress.get",
          output_summary: "读取当前项目进度",
          status: "completed",
          tool_call_id: "tool-1",
        },
      ],
      updated_at: "2026-08-11T08:01:00Z",
      version: 1,
    });
    mocks.stream.mockResolvedValue(undefined);

    renderDialog();

    expect(
      await screen.findByText("评估完成，项目处于执行阶段。"),
    ).toBeInTheDocument();
    expect(screen.getByText("读取当前任务与里程碑")).toBeInTheDocument();
    expect(screen.getByText("progress.get")).toBeInTheDocument();
    expect(screen.getByText(/只读视图/)).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("stops waiting and explains a failure that happened before a Run existed", async () => {
    mocks.request.mockResolvedValue({
      agent_instance_id: "agent-1",
      error_code: "PROGRESS_EVALUATOR_CONFIGURATION_INVALID",
      error_message: "Progress evaluator configuration is invalid",
      evaluation_id: "evaluation-1",
      project_id: "project-1",
      status: "failed",
    });

    renderDialog({
      agentInstanceId: undefined,
      evaluationStatus: "failed",
      runId: undefined,
      sessionId: undefined,
    });

    expect(
      await screen.findByText("评估在 Agent Session 或 Run 可用前失败"),
    ).toBeInTheDocument();
    expect(
      await screen.findByText("PROGRESS_EVALUATOR_CONFIGURATION_INVALID"),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("正在等待 Agent Session"),
    ).not.toBeInTheDocument();
  });

  it("falls back to the persisted evaluation and retries messages after the remote Session read fails", async () => {
    mocks.listMessages.mockRejectedValue(new Error("runtime unavailable"));
    mocks.getRun.mockResolvedValue({
      created_at: "2026-08-11T08:00:00Z",
      remote_run_id: "remote-1",
      run_id: "run-1",
      session_id: "session-1",
      source: "progress_evaluation",
      status: "completed",
      tool_calls: [
        {
          name: "mcp__mmdash_project__data_read",
          status: "completed",
          tool_call_id: "tool-1",
        },
      ],
      updated_at: "2026-08-11T08:01:00Z",
      version: 1,
    });
    mocks.stream.mockResolvedValue(undefined);
    mocks.request.mockResolvedValue({
      agent_instance_id: "agent-1",
      agent_run_id: "run-1",
      agent_session_id: "session-1",
      blockers: [],
      changes_since_last: ["论文草稿已更新。"],
      completed_items: [],
      detected_stage: "文章撰写",
      evaluation_id: "evaluation-1",
      in_progress_items: ["论文正文正在整理。"],
      pending_questions: ["是否确认当前草稿用于正式论文？"],
      project_id: "project-1",
      risks: [],
      status: "succeeded",
      summary: "项目正在撰写论文，下一步是完善正文并建立对应任务。",
    });

    renderDialog();

    expect(await screen.findByText("评估结论（已保存）")).toBeInTheDocument();
    expect(
      screen.getByText("项目正在撰写论文，下一步是完善正文并建立对应任务。"),
    ).toBeInTheDocument();
    expect(screen.getByText("读取证据详情")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "重新读取会话" }),
    ).toBeInTheDocument();
  });

  it("reconnects a recoverable live stream and keeps the final evaluation output", async () => {
    const runningRun = {
      created_at: "2026-08-11T08:00:00Z",
      remote_run_id: "remote-1",
      run_id: "run-1",
      session_id: "session-1",
      source: "progress_evaluation",
      status: "running",
      tool_calls: [],
      updated_at: "2026-08-11T08:00:10Z",
      version: 1,
    };
    const completedRun = {
      ...runningRun,
      status: "completed",
      updated_at: "2026-08-11T08:01:00Z",
    };
    mocks.listMessages.mockResolvedValue({ items: [] });
    mocks.getRun.mockResolvedValue(runningRun);
    mocks.request.mockResolvedValue({
      agent_instance_id: "agent-1",
      agent_run_id: "run-1",
      agent_session_id: "session-1",
      evaluation_id: "evaluation-1",
      project_id: "project-1",
      status: "running",
    });
    let connections = 0;
    mocks.stream.mockImplementation(async (_input, onEvent) => {
      connections += 1;
      if (connections === 1) {
        await onEvent({
          event: "error",
          event_id: "event-error",
          occurred_at: "2026-08-11T08:00:20Z",
          run_id: "run-1",
          safe_error_code: "runtime_stream_failed",
          safe_error_message: "The Agent event stream ended unexpectedly",
          sequence: 1,
          session_id: "session-1",
        });
        return;
      }
      await onEvent({
        delta: "恢复连接后的完整评估结论",
        event: "message.completed",
        event_id: "event-message",
        message_id: "message-1",
        occurred_at: "2026-08-11T08:00:50Z",
        run_id: "run-1",
        sequence: 2,
        session_id: "session-1",
      });
      await onEvent({
        event: "run.completed",
        event_id: "event-completed",
        occurred_at: "2026-08-11T08:01:00Z",
        run: completedRun,
        run_id: "run-1",
        sequence: 3,
        session_id: "session-1",
      });
    });

    renderDialog({ evaluationStatus: "running" });

    await waitFor(() => expect(mocks.stream).toHaveBeenCalledTimes(2), {
      timeout: 3_000,
    });
    expect(
      await screen.findByText("恢复连接后的完整评估结论"),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("The Agent event stream ended unexpectedly"),
    ).not.toBeInTheDocument();
  });
});
