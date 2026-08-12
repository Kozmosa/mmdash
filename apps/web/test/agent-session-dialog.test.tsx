import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

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
});
