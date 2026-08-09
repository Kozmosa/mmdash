import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  approveRun: vi.fn(),
  continueSession: vi.fn(),
  createSession: vi.fn(),
  endSession: vi.fn(),
  forkSession: vi.fn(),
  getPrompt: vi.fn(),
  getRun: vi.fn(),
  listInstances: vi.fn(),
  listContextProposals: vi.fn(),
  listMessages: vi.fn(),
  listSessions: vi.fn(),
  regenerateRun: vi.fn(),
  renameSession: vi.fn(),
  rerunRun: vi.fn(),
  reviewContextProposal: vi.fn(),
  resetPrompt: vi.fn(),
  setDefaultSession: vi.fn(),
  startRun: vi.fn(),
  stopRun: vi.fn(),
  streamAgentRun: vi.fn(),
  updatePrompt: vi.fn(),
  projectRole: { value: "owner" },
}));

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({
    id: "project-1",
    name: "Project",
    role: mocks.projectRole.value,
  }),
}));

vi.mock("@/features/agent/agent-api", () => ({
  agentApi: {
    approveRun: mocks.approveRun,
    continueSession: mocks.continueSession,
    createSession: mocks.createSession,
    endSession: mocks.endSession,
    forkSession: mocks.forkSession,
    getPrompt: mocks.getPrompt,
    getRun: mocks.getRun,
    listInstances: mocks.listInstances,
    listMessages: mocks.listMessages,
    listSessions: mocks.listSessions,
    regenerateRun: mocks.regenerateRun,
    renameSession: mocks.renameSession,
    rerunRun: mocks.rerunRun,
    resetPrompt: mocks.resetPrompt,
    setDefaultSession: mocks.setDefaultSession,
    startRun: mocks.startRun,
    stopRun: mocks.stopRun,
    updatePrompt: mocks.updatePrompt,
  },
  streamAgentRun: mocks.streamAgentRun,
}));

vi.mock("@/features/agent/context-proposal-api", () => ({
  contextProposalApi: {
    list: mocks.listContextProposals,
    review: mocks.reviewContextProposal,
  },
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { AgentWorkbench } from "@/features/agent/agent-workbench";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.projectRole.value = "owner";
});

describe("Agent workbench", () => {
  it("renders history, streams replies and Tool Calls, and responds to approval", async () => {
    const session = sessionFixture();
    const launch = { run: runFixture({ status: "running" }), session };
    prepareQueries(session);
    mocks.startRun.mockResolvedValue(launch);
    mocks.stopRun.mockResolvedValue(runFixture({ status: "stopping" }));
    mocks.approveRun.mockResolvedValue(runFixture({ status: "running" }));
    mocks.streamAgentRun.mockImplementation(async (_input, onEvent) => {
      await onEvent(
        streamEvent("message.started", { message_id: "message-2" }),
      );
      await onEvent(
        streamEvent("message.delta", {
          delta: "流式回答",
          message_id: "message-2",
        }),
      );
      await onEvent(
        streamEvent("tool.progress", {
          tool_call: {
            input_summary: "读取项目任务",
            name: "data.read",
            status: "running",
            tool_call_id: "tool-1",
          },
        }),
      );
      await onEvent(
        streamEvent("approval.required", {
          approval: {
            approval_id: "approval-1",
            choices: ["once", "session", "deny"],
          },
          run: runFixture({ status: "waiting_for_approval" }),
        }),
      );
      return "event-4";
    });

    render(<AgentWorkbench />, { wrapper: Providers });

    expect(await screen.findByText("历史消息")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("发给 Hermes 的消息"), {
      target: { value: "分析当前项目" },
    });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));

    await waitFor(() =>
      expect(mocks.startRun).toHaveBeenCalledWith(
        "project-1",
        "instance-1",
        "session-1",
        "分析当前项目",
      ),
    );
    expect(await screen.findByText(/流式回答/)).toBeInTheDocument();
    expect(screen.getByText("data.read")).toBeInTheDocument();
    expect(screen.getByText("Agent 请求工具审批")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "仅本次" }));
    await waitFor(() =>
      expect(mocks.approveRun).toHaveBeenCalledWith(
        "project-1",
        "instance-1",
        "session-1",
        "run-1",
        "approval-1",
        "once",
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "停止" }));
    await waitFor(() =>
      expect(mocks.stopRun).toHaveBeenCalledWith(
        "project-1",
        "instance-1",
        "session-1",
        "run-1",
      ),
    );
  });

  it("keeps consecutive approvals FIFO and advances only after the head succeeds", async () => {
    const session = sessionFixture();
    prepareQueries(session);
    mocks.startRun.mockResolvedValue({
      run: runFixture({ status: "running" }),
      session,
    });
    mocks.approveRun.mockResolvedValue(
      runFixture({ status: "waiting_for_approval" }),
    );
    mocks.streamAgentRun.mockImplementation(async (_input, onEvent) => {
      await onEvent(
        streamEvent("approval.required", {
          approval: { approval_id: "approval-1", choices: ["once"] },
        }),
      );
      await onEvent(
        streamEvent("approval.required", {
          approval: { approval_id: "approval-2", choices: ["deny"] },
        }),
      );
      return "approval-event-2";
    });

    render(<AgentWorkbench />, { wrapper: Providers });
    await sendMessage("触发连续审批");

    const firstChoice = await screen.findByRole("button", { name: "仅本次" });
    expect(
      screen.queryByRole("button", { name: "拒绝" }),
    ).not.toBeInTheDocument();
    fireEvent.click(firstChoice);

    await waitFor(() =>
      expect(mocks.approveRun).toHaveBeenNthCalledWith(
        1,
        "project-1",
        "instance-1",
        "session-1",
        "run-1",
        "approval-1",
        "once",
      ),
    );

    const secondChoice = await screen.findByRole("button", { name: "拒绝" });
    expect(
      screen.queryByRole("button", { name: "仅本次" }),
    ).not.toBeInTheDocument();
    fireEvent.click(secondChoice);

    await waitFor(() =>
      expect(mocks.approveRun).toHaveBeenNthCalledWith(
        2,
        "project-1",
        "instance-1",
        "session-1",
        "run-1",
        "approval-2",
        "deny",
      ),
    );
  });

  it("updates duplicate requests in place and ignores duplicate non-head responses", async () => {
    const session = sessionFixture();
    prepareQueries(session);
    mocks.startRun.mockResolvedValue({
      run: runFixture({ status: "running" }),
      session,
    });
    mocks.approveRun.mockResolvedValue(runFixture({ status: "running" }));
    mocks.streamAgentRun.mockImplementation(async (_input, onEvent) => {
      await onEvent(
        streamEvent("approval.required", {
          approval: { approval_id: "approval-1", choices: ["once"] },
        }),
      );
      await onEvent(
        streamEvent("approval.required", {
          approval: { approval_id: "approval-2", choices: ["deny"] },
        }),
      );
      await onEvent(
        streamEvent("approval.required", {
          approval: { approval_id: "approval-1", choices: ["session"] },
        }),
      );
      await onEvent(
        streamEvent("approval.responded", {
          approval: { approval_id: "approval-2", choices: [] },
        }),
      );
      await onEvent(
        streamEvent("approval.responded", {
          approval: { approval_id: "approval-2", choices: [] },
        }),
      );
      return "approval-event-5";
    });

    render(<AgentWorkbench />, { wrapper: Providers });
    await sendMessage("触发重复审批事件");

    expect(
      await screen.findByRole("button", { name: "本 Session" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "仅本次" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "拒绝" }),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "本 Session" }));
    await waitFor(() =>
      expect(mocks.approveRun).toHaveBeenCalledWith(
        "project-1",
        "instance-1",
        "session-1",
        "run-1",
        "approval-1",
        "session",
      ),
    );
    await waitFor(() =>
      expect(screen.queryByText("Agent 请求工具审批")).not.toBeInTheDocument(),
    );
  });

  it("advances on a matching head response without letting its replay clear the next item", async () => {
    const session = sessionFixture();
    prepareQueries(session);
    mocks.startRun.mockResolvedValue({
      run: runFixture({ status: "running" }),
      session,
    });
    mocks.approveRun.mockResolvedValue(runFixture({ status: "running" }));
    mocks.streamAgentRun.mockImplementation(async (_input, onEvent) => {
      await onEvent(
        streamEvent("approval.required", {
          approval: { approval_id: "approval-1", choices: ["once"] },
        }),
      );
      await onEvent(
        streamEvent("approval.required", {
          approval: { approval_id: "approval-2", choices: ["deny"] },
        }),
      );
      await onEvent(
        streamEvent("approval.responded", {
          approval: { approval_id: "approval-1", choices: [] },
        }),
      );
      await onEvent(
        streamEvent("approval.responded", {
          approval: { approval_id: "approval-1", choices: [] },
        }),
      );
      return "approval-event-4";
    });

    render(<AgentWorkbench />, { wrapper: Providers });
    await sendMessage("推进审批队列");

    const remainingChoice = await screen.findByRole("button", { name: "拒绝" });
    expect(
      screen.queryByRole("button", { name: "仅本次" }),
    ).not.toBeInTheDocument();
    fireEvent.click(remainingChoice);

    await waitFor(() =>
      expect(mocks.approveRun).toHaveBeenCalledWith(
        "project-1",
        "instance-1",
        "session-1",
        "run-1",
        "approval-2",
        "deny",
      ),
    );
  });

  it("clears approvals on Session switch and ignores late events from the old stream", async () => {
    const session = sessionFixture();
    const secondary = sessionFixture({
      default: false,
      session_id: "session-2",
      title: "Secondary",
    });
    prepareQueries(session);
    mocks.listSessions.mockResolvedValue({ items: [session, secondary] });
    mocks.startRun.mockResolvedValue({
      run: runFixture({ status: "running" }),
      session,
    });
    let emitStreamEvent:
      ((event: ReturnType<typeof streamEvent>) => Promise<void>) | null = null;
    mocks.streamAgentRun.mockImplementation(async (_input, onEvent) => {
      emitStreamEvent = async (event) => void (await onEvent(event));
      await onEvent(
        streamEvent("approval.required", {
          approval: { approval_id: "approval-1", choices: ["once"] },
        }),
      );
      return "approval-event-1";
    });

    render(<AgentWorkbench />, { wrapper: Providers });
    await sendMessage("切换 Session");

    expect(await screen.findByText("Agent 请求工具审批")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Secondary/ }));
    await waitFor(() =>
      expect(screen.queryByText("Agent 请求工具审批")).not.toBeInTheDocument(),
    );

    await act(async () => {
      await emitStreamEvent!(
        streamEvent("approval.required", {
          approval: { approval_id: "approval-late", choices: ["deny"] },
        }),
      );
    });
    expect(screen.queryByText("Agent 请求工具审批")).not.toBeInTheDocument();
  });

  it("clears approvals at Run terminal state and ignores later replayed requests", async () => {
    const session = sessionFixture();
    prepareQueries(session);
    mocks.startRun.mockResolvedValue({
      run: runFixture({ status: "running" }),
      session,
    });
    let emitStreamEvent:
      ((event: ReturnType<typeof streamEvent>) => Promise<void>) | null = null;
    mocks.streamAgentRun.mockImplementation(async (_input, onEvent) => {
      emitStreamEvent = async (event) => void (await onEvent(event));
      await onEvent(
        streamEvent("approval.required", {
          approval: { approval_id: "approval-1", choices: ["once"] },
        }),
      );
      return "approval-event-1";
    });

    render(<AgentWorkbench />, { wrapper: Providers });
    await sendMessage("结束 Run");
    expect(await screen.findByText("Agent 请求工具审批")).toBeInTheDocument();

    await act(async () => {
      await emitStreamEvent!(
        streamEvent("run.completed", {
          run: runFixture({ status: "completed" }),
        }),
      );
    });
    expect(screen.queryByText("Agent 请求工具审批")).not.toBeInTheDocument();

    await act(async () => {
      await emitStreamEvent!(
        streamEvent("approval.required", {
          approval: { approval_id: "approval-late", choices: ["deny"] },
        }),
      );
    });
    expect(screen.queryByText("Agent 请求工具审批")).not.toBeInTheDocument();
  });

  it("supports ended Session continuation and non-destructive Run replay", async () => {
    const session = sessionFixture({
      last_run_id: "run-1",
      status: "ended",
    });
    const continuedSession = sessionFixture({
      last_run_id: "run-1",
      status: "active",
    });
    prepareQueries(session);
    mocks.listSessions
      .mockResolvedValueOnce({ items: [session] })
      .mockResolvedValue({ items: [continuedSession] });
    mocks.getRun.mockResolvedValue(runFixture({ status: "completed" }));
    mocks.continueSession.mockResolvedValue(continuedSession);
    mocks.regenerateRun.mockResolvedValue({
      run: runFixture({ source: "regenerate", status: "running" }),
      session: sessionFixture({ session_id: "session-2", title: "Fork" }),
    });
    mocks.rerunRun.mockResolvedValue({
      run: runFixture({ source: "rerun", status: "running" }),
      session: sessionFixture(),
    });
    mocks.streamAgentRun.mockResolvedValue("event-final");

    render(<AgentWorkbench />, { wrapper: Providers });

    await screen.findByRole("button", { name: "重新生成" });
    const continueButton = screen.getByRole("button", { name: "继续" });
    expect(continueButton).toBeEnabled();
    fireEvent.click(continueButton);
    await waitFor(() =>
      expect(mocks.continueSession).toHaveBeenCalledWith(
        "project-1",
        "instance-1",
        "session-1",
      ),
    );
    await screen.findByRole("button", { name: "结束" });

    const regenerate = screen.getByRole("button", { name: "重新生成" });
    fireEvent.click(regenerate);
    await waitFor(() =>
      expect(mocks.regenerateRun).toHaveBeenCalledWith(
        "project-1",
        "instance-1",
        "session-1",
        "run-1",
      ),
    );
  });

  it("reviews only pending Agent Context Proposals and retains Session and Run provenance", async () => {
    const session = sessionFixture();
    const acceptedProposal = contextProposalFixture({
      proposal_id: "proposal-accepted",
      status: "accepted",
      title: "已审核结论",
    });
    const humanProposal = contextProposalFixture({
      agent_run_id: undefined,
      agent_session_id: undefined,
      proposal_id: "proposal-human",
      proposed_by_actor_id: "user-2",
      proposed_by_actor_kind: "session",
      title: "人工提议",
    });
    const first = contextProposalFixture({
      proposal_id: "proposal-1",
      title: "边界条件结论",
    });
    const second = contextProposalFixture({
      agent_run_id: "run-2",
      agent_session_id: "session-2",
      proposal_id: "proposal-2",
      title: "误差来源结论",
    });
    prepareQueries(session);
    mocks.listContextProposals.mockResolvedValue({
      items: [acceptedProposal, humanProposal, first, second],
    });
    mocks.reviewContextProposal
      .mockResolvedValueOnce({
        ...first,
        review_note: "验证记录一致",
        status: "accepted",
      })
      .mockResolvedValueOnce({
        ...second,
        review_note: "需要补充证据",
        status: "rejected",
      });

    render(<AgentWorkbench />, { wrapper: Providers });

    expect(await screen.findByText("边界条件结论")).toBeInTheDocument();
    expect(screen.getByText("误差来源结论")).toBeInTheDocument();
    expect(screen.queryByText("已审核结论")).not.toBeInTheDocument();
    expect(screen.queryByText("人工提议")).not.toBeInTheDocument();

    fireEvent.click(screen.getAllByText("查看 Agent / Session / Run 来源")[0]!);
    expect(screen.getAllByText("instance-1")).toHaveLength(2);
    expect(screen.getByText("session-1")).toBeInTheDocument();
    expect(screen.getByText("run-context-1")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("审核备注：边界条件结论"), {
      target: { value: "验证记录一致" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "接受提议：边界条件结论" }),
    );
    await waitFor(() =>
      expect(mocks.reviewContextProposal).toHaveBeenNthCalledWith(
        1,
        "project-1",
        "proposal-1",
        "accepted",
        "验证记录一致",
      ),
    );
    await waitFor(() =>
      expect(screen.queryByText("边界条件结论")).not.toBeInTheDocument(),
    );

    fireEvent.change(screen.getByLabelText("审核备注：误差来源结论"), {
      target: { value: "需要补充证据" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "拒绝提议：误差来源结论" }),
    );
    await waitFor(() =>
      expect(mocks.reviewContextProposal).toHaveBeenNthCalledWith(
        2,
        "project-1",
        "proposal-2",
        "rejected",
        "需要补充证据",
      ),
    );
    await waitFor(() =>
      expect(screen.queryByText("误差来源结论")).not.toBeInTheDocument(),
    );
  });

  it("does not request Context Proposals for a viewer without review permission", async () => {
    mocks.projectRole.value = "viewer";
    prepareQueries(sessionFixture());

    render(<AgentWorkbench />, { wrapper: Providers });

    expect(
      await screen.findByText("当前项目角色没有上下文审核权限。"),
    ).toBeInTheDocument();
    expect(mocks.listContextProposals).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("button", { name: /接受提议/ }),
    ).not.toBeInTheDocument();
  });
});

async function sendMessage(content: string) {
  await screen.findByText("历史消息");
  fireEvent.change(screen.getByLabelText("发给 Hermes 的消息"), {
    target: { value: content },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await waitFor(() => expect(mocks.startRun).toHaveBeenCalled());
}

function Providers({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            mutations: { retry: false },
            queries: { retry: false },
          },
        })
      }
    >
      {children}
    </QueryClientProvider>
  );
}

function prepareQueries(session: ReturnType<typeof sessionFixture>) {
  mocks.listInstances.mockResolvedValue({ items: [instanceFixture()] });
  mocks.listContextProposals.mockResolvedValue({ items: [] });
  mocks.listSessions.mockResolvedValue({ items: [session] });
  mocks.listMessages.mockResolvedValue({
    items: [
      {
        content: "历史消息",
        message_id: "message-1",
        role: "assistant",
        tool_calls: [
          {
            name: "project.get",
            output_summary: "读取项目",
            status: "completed",
            tool_call_id: "history-tool",
          },
        ],
      },
    ],
  });
  mocks.getPrompt.mockResolvedValue({
    agent_instance_id: "instance-1",
    custom: false,
    default_prompt: "默认 Prompt",
    effective_prompt: "默认 Prompt",
    project_id: "project-1",
    version: 1,
  });
  mocks.getRun.mockResolvedValue(runFixture({ status: "completed" }));
}

function contextProposalFixture(overrides: Record<string, unknown> = {}) {
  return {
    agent_run_id: "run-context-1",
    agent_session_id: "session-1",
    content: "校准误差来自边界条件。",
    context_type: "finding",
    created_at: "2026-08-09T00:00:00Z",
    project_id: "project-1",
    proposal_id: "proposal-1",
    proposed_by: "instance-1",
    proposed_by_actor_id: "instance-1",
    proposed_by_actor_kind: "agent",
    rationale: "Run 汇总了验证结果。",
    review_note: "",
    source_object_ids: ["object-1"],
    status: "pending",
    title: "校准误差结论",
    updated_at: "2026-08-09T00:00:00Z",
    ...overrides,
  };
}

function instanceFixture() {
  return {
    adapter_type: "hermes",
    agent_instance_id: "instance-1",
    capabilities: {
      jobs: true,
      message_history: true,
      project_access: { configure: true, rotate: true, verify: true },
      run_approval: true,
      run_events: true,
      run_status: true,
      run_stop: true,
      runs: true,
      session_chat_stream: true,
      session_fork: true,
      sessions: true,
    },
    created_at: "2026-08-06T00:00:00Z",
    created_by: "user-1",
    credentials: [],
    display_name: "Hermes",
    grant: {
      agent_instance_id: "instance-1",
      allowed_tools: ["project.get", "data.read"],
      created_at: "2026-08-06T00:00:00Z",
      grant_id: "grant-1",
      project_access_status: "verified",
      project_id: "project-1",
      status: "active",
      updated_at: "2026-08-06T00:00:00Z",
      version: 1,
    },
    management_mode: "manual",
    management_path: "direct",
    project_id: "project-1",
    request_timeout_seconds: 30,
    runtime_url: "https://hermes.example.test",
    secrets: {
      cloudflare_access_configured: false,
      dashboard_session_token_configured: false,
      hermes_api_key_configured: true,
    },
    status: "active",
    updated_at: "2026-08-06T00:00:00Z",
    version: 1,
  };
}

function sessionFixture(overrides: Record<string, unknown> = {}) {
  return {
    agent_instance_id: "instance-1",
    created_at: "2026-08-06T00:00:00Z",
    default: true,
    project_id: "project-1",
    remote_session_id: "hermes-session-1",
    session_id: "session-1",
    session_type: "main",
    status: "active",
    title: "Main",
    updated_at: "2026-08-06T00:00:00Z",
    version: 1,
    ...overrides,
  };
}

function runFixture(overrides: Record<string, unknown> = {}) {
  return {
    created_at: "2026-08-06T00:00:00Z",
    remote_run_id: "hermes-run-1",
    run_id: "run-1",
    session_id: "session-1",
    source: "message",
    status: "running",
    tool_calls: [],
    updated_at: "2026-08-06T00:00:00Z",
    version: 1,
    ...overrides,
  };
}

function streamEvent(event: string, overrides: Record<string, unknown> = {}) {
  return {
    event,
    event_id: `event-${event}`,
    occurred_at: "2026-08-06T00:00:00Z",
    run_id: "run-1",
    sequence: 1,
    session_id: "session-1",
    ...overrides,
  };
}
