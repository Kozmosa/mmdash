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
  downloadArtifact: vi.fn(),
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
  multipartOptions: [] as unknown[],
  multipartStart: vi.fn(),
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

vi.mock("@/features/artifact/artifact-api", () => ({
  artifactApi: { download: mocks.downloadArtifact },
}));

vi.mock("@/features/artifact/artifact-detail-drawer", () => ({
  ArtifactDetailDrawer: ({ artifactId }: { artifactId?: string }) =>
    artifactId ? (
      <div aria-label="Artifact 详情测试" role="dialog">
        {artifactId}
      </div>
    ) : null,
}));

vi.mock("@/features/artifact/multipart-upload", () => ({
  MultipartUploadTask: class {
    constructor(options: unknown) {
      mocks.multipartOptions.push(options);
    }

    cancel() {
      return Promise.resolve();
    }

    start() {
      return mocks.multipartStart();
    }

    subscribe(listener: (snapshot: { progress: number }) => void) {
      listener({ progress: 0 });
      return () => undefined;
    }
  },
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { AgentWorkbench } from "@/features/agent/agent-workbench";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  window.localStorage.clear();
  window.history.replaceState({}, "", "/");
  mocks.projectRole.value = "owner";
  mocks.multipartOptions.length = 0;
});

describe("Agent workbench", () => {
  it("restores the selected Session from the URL and the collapsed Session list", async () => {
    const defaultSession = sessionFixture();
    const rememberedSession = sessionFixture({
      default: false,
      remote_session_id: "hermes-session-2",
      session_id: "session-2",
      title: "Remembered Session",
    });
    prepareQueries(defaultSession);
    mocks.listSessions.mockResolvedValue({
      items: [defaultSession, rememberedSession],
    });
    window.localStorage.setItem(
      "mmdash.agent.session-sidebar.project-1.instance-1",
      "false",
    );
    window.history.replaceState(
      {},
      "",
      "/projects/project-1/agent?agent=instance-1&session=session-2",
    );

    render(<AgentWorkbench />, { wrapper: Providers });

    expect(
      await screen.findByRole("heading", { name: "Remembered Session" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "展开会话列表" }),
    ).toBeInTheDocument();
    expect(new URL(window.location.href).searchParams.get("session")).toBe(
      "session-2",
    );
  });

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

    fireEvent.click(screen.getByRole("button", { name: "停止输出" }));
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
    fireEvent.click(screen.getByRole("button", { name: "Secondary active" }));
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
    fireEvent.contextMenu(
      screen.getByRole("button", { name: /^Main .*ended/ }),
    );
    const continueButton = await screen.findByRole("menuitem", {
      name: "继续",
    });
    expect(continueButton).toBeEnabled();
    fireEvent.click(continueButton);
    await waitFor(() =>
      expect(mocks.continueSession).toHaveBeenCalledWith(
        "project-1",
        "instance-1",
        "session-1",
      ),
    );
    await screen.findByRole("button", { name: "打开 Main 会话菜单" });

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

  it("shows only main Sessions and creates a main Session from the on-demand form", async () => {
    const main = sessionFixture();
    const second = sessionFixture({
      default: false,
      session_id: "session-2",
      title: "Second",
    });
    const internal = sessionFixture({
      default: false,
      session_id: "session-progress",
      session_type: "progress",
      title: "Internal progress",
    });
    prepareQueries(main);
    mocks.listSessions.mockResolvedValue({ items: [main, internal] });
    mocks.createSession.mockResolvedValue(second);
    mocks.startRun
      .mockResolvedValueOnce({
        run: runFixture({ status: "running" }),
        session: main,
      })
      .mockResolvedValueOnce({
        run: runFixture({ run_id: "run-2", status: "running" }),
        session: second,
      });
    mocks.streamAgentRun.mockImplementation(() => new Promise(() => {}));

    render(<AgentWorkbench />, { wrapper: Providers });

    await screen.findByText("历史消息");
    expect(screen.queryByText("Internal progress")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("新 Session 名称")).not.toBeInTheDocument();

    await sendMessage("旧 Session 中正在运行");
    await screen.findByRole("button", { name: "停止输出" });

    fireEvent.click(screen.getByRole("button", { name: "新建会话" }));
    fireEvent.change(screen.getByLabelText("新 Session 名称"), {
      target: { value: "Second" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建" }));

    await waitFor(() =>
      expect(mocks.createSession).toHaveBeenCalledWith(
        "project-1",
        "instance-1",
        { default: false, session_type: "main", title: "Second" },
      ),
    );
    await screen.findByRole("heading", { name: "Second" });
    expect(
      screen.queryByRole("button", { name: "停止输出" }),
    ).not.toBeInTheDocument();

    const composer = screen.getByLabelText("发给 Hermes 的消息");
    fireEvent.change(composer, { target: { value: "新 Session 消息" } });
    fireEvent.keyDown(composer, { key: "Enter" });
    await waitFor(() =>
      expect(mocks.startRun).toHaveBeenLastCalledWith(
        "project-1",
        "instance-1",
        "session-2",
        "新 Session 消息",
      ),
    );
  });

  it("sends with Enter, preserves Shift+Enter, and collapses auxiliary panels", async () => {
    const session = sessionFixture();
    prepareQueries(session);
    mocks.startRun.mockResolvedValue({
      run: runFixture({ status: "running" }),
      session,
    });
    mocks.streamAgentRun.mockResolvedValue("event-final");

    render(<AgentWorkbench />, { wrapper: Providers });

    await screen.findByText("历史消息");
    const composer = screen.getByLabelText("发给 Hermes 的消息");
    fireEvent.change(composer, { target: { value: "第一行\n第二行" } });
    fireEvent.keyDown(composer, { key: "Enter", shiftKey: true });
    expect(mocks.startRun).not.toHaveBeenCalled();

    fireEvent.keyDown(composer, { key: "Enter" });
    await waitFor(() =>
      expect(mocks.startRun).toHaveBeenCalledWith(
        "project-1",
        "instance-1",
        "session-1",
        "第一行\n第二行",
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "收起会话列表" }));
    expect(
      screen.getByRole("button", { name: "展开会话列表" }),
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getAllByRole("button", { name: "关闭项目上下文" })[0]!,
    );
    expect(
      screen.getByRole("button", { name: "打开项目上下文" }),
    ).toBeInTheDocument();
  });

  it("reattaches to a running Run and renders safe reasoning, tools, and the final reply without a refresh", async () => {
    const session = sessionFixture({ last_run_id: "run-1" });
    prepareQueries(session);
    mocks.getRun.mockResolvedValue(runFixture({ status: "running" }));
    mocks.streamAgentRun.mockImplementation(async (_input, onEvent) => {
      await onEvent(streamEvent("reasoning.available"));
      await onEvent(
        streamEvent("tool.progress", {
          tool_call: {
            name: "project.get",
            status: "completed",
            tool_call_id: "tool-resumed",
          },
        }),
      );
      await onEvent(
        streamEvent("message.completed", {
          delta: "恢复连接后的最终回答",
          message_id: "message-resumed",
        }),
      );
      await onEvent(
        streamEvent("run.completed", {
          run: runFixture({ status: "completed" }),
        }),
      );
      return "event-run.completed";
    });

    render(<AgentWorkbench />, { wrapper: Providers });

    await waitFor(() => expect(mocks.streamAgentRun).toHaveBeenCalled());
    expect(await screen.findByText("恢复连接后的最终回答")).toBeInTheDocument();
    expect(screen.queryByText("正在思考…")).toBeNull();
    expect(screen.getAllByText("project.get").length).toBeGreaterThan(0);
    expect(mocks.startRun).not.toHaveBeenCalled();
    expect(mocks.streamAgentRun).toHaveBeenCalledWith(
      expect.objectContaining({
        instanceId: "instance-1",
        runId: "run-1",
        sessionId: "session-1",
      }),
      expect.any(Function),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });

  it("reconnects an unexpectedly ended stream while the Run keeps producing output", async () => {
    const session = sessionFixture({ last_run_id: "run-1" });
    prepareQueries(session);
    mocks.getRun.mockResolvedValue(runFixture({ status: "running" }));
    let connection = 0;
    mocks.streamAgentRun.mockImplementation(async (_input, onEvent) => {
      connection += 1;
      if (connection === 1) {
        await onEvent(
          streamEvent("error", {
            safe_error_message: "The Agent event stream ended unexpectedly",
          }),
        );
        return "event-error";
      }
      await onEvent(
        streamEvent("message.completed", {
          delta: "重连后继续收到最终回答",
          message_id: "message-after-reconnect",
        }),
      );
      await onEvent(
        streamEvent("run.completed", {
          run: runFixture({ status: "completed" }),
        }),
      );
      return "event-run.completed";
    });

    render(<AgentWorkbench />, { wrapper: Providers });

    expect(
      await screen.findByText("重连后继续收到最终回答"),
    ).toBeInTheDocument();
    await waitFor(() => expect(mocks.streamAgentRun).toHaveBeenCalledTimes(2));
    expect(
      screen.queryByText("The Agent event stream ended unexpectedly"),
    ).toBeNull();
    expect(screen.queryByText("回复流暂时中断，正在自动重连…")).toBeNull();
  });

  it("shows persisted Agent files inline and suppresses duplicated streamed final output", async () => {
    const session = sessionFixture({ last_run_id: "run-1" });
    prepareQueries(session);
    mocks.listMessages.mockResolvedValue({
      items: [
        {
          attachments: [
            {
              artifact_id: "artifact-1",
              created_at: "2026-08-11T00:00:00Z",
              direction: "output",
              filename: "heart.png",
              mime_type: "image/png",
              name: "心形曲线",
              run_id: "run-1",
              size_bytes: 50547,
              version_id: "version-1",
            },
          ],
          content: "",
          message_id: "mmdash-artifacts-run-1-output",
          role: "assistant",
          tool_calls: [],
        },
        {
          content: "唯一最终回答",
          message_id: "message-final",
          role: "assistant",
          tool_calls: [
            {
              name: "artifact.upload",
              status: "completed",
              tool_call_id: "tool-upload",
            },
          ],
        },
        {
          content: "   ",
          message_id: "empty-assistant-projection",
          role: "assistant",
          tool_calls: [],
        },
        {
          content: "唯一最终回答",
          message_id: "message-final-duplicate",
          role: "assistant",
          tool_calls: [
            {
              name: "artifact.upload",
              status: "completed",
              tool_call_id: "tool-upload",
            },
          ],
        },
      ],
    });
    mocks.downloadArtifact.mockResolvedValue({
      transfer: { url: "https://object.test/heart.png" },
    });
    mocks.getRun.mockResolvedValue(runFixture({ status: "completed" }));
    mocks.streamAgentRun.mockImplementation(async (_input, onEvent) => {
      await onEvent(
        streamEvent("tool.progress", {
          tool_call: {
            name: "artifact.upload",
            status: "completed",
            tool_call_id: "tool-upload",
          },
        }),
      );
      await onEvent(
        streamEvent("message.completed", {
          delta: "唯一最终回答",
          message_id: "message-final",
        }),
      );
      await onEvent(
        streamEvent("run.completed", {
          run: runFixture({ status: "completed" }),
        }),
      );
      return "event-run.completed";
    });

    render(<AgentWorkbench />, { wrapper: Providers });

    expect(await screen.findByAltText("心形曲线")).toHaveAttribute(
      "src",
      "https://object.test/heart.png",
    );
    fireEvent.click(
      screen.getByRole("button", { name: "打开 Artifact 详情：心形曲线" }),
    );
    expect(
      screen.getByRole("dialog", { name: "Artifact 详情测试" }),
    ).toHaveTextContent("artifact-1");
    expect(window.location.pathname).toBe("/");
    await waitFor(() =>
      expect(screen.getAllByText("唯一最终回答")).toHaveLength(1),
    );
    expect(screen.getAllByText("artifact.upload")).toHaveLength(1);
    expect(mocks.downloadArtifact).toHaveBeenCalledWith(
      "project-1",
      "artifact-1",
      "version-1",
    );
  });

  it("does not place output attachments in the active thinking chain", async () => {
    const session = sessionFixture({ last_run_id: "run-1" });
    prepareQueries(session);
    mocks.listMessages.mockResolvedValue({
      items: [
        {
          attachments: [
            {
              artifact_id: "artifact-active",
              created_at: "2026-08-11T00:00:00Z",
              direction: "output",
              filename: "chart.png",
              mime_type: "image/png",
              name: "生成中的图",
              run_id: "run-1",
              size_bytes: 128,
              version_id: "version-active",
            },
          ],
          content: "",
          message_id: "mmdash-artifacts-run-1-output",
          role: "assistant",
          tool_calls: [],
        },
      ],
    });
    mocks.downloadArtifact.mockResolvedValue({
      transfer: { url: "https://object.test/chart.png" },
    });
    mocks.getRun.mockResolvedValue(runFixture({ status: "running" }));
    mocks.streamAgentRun.mockImplementation(async (_input, onEvent) => {
      await onEvent(streamEvent("reasoning.available"));
      return "event-running";
    });

    render(<AgentWorkbench />, { wrapper: Providers });

    await waitFor(() => expect(mocks.streamAgentRun).toHaveBeenCalled());
    expect(screen.queryByAltText("生成中的图")).toBeNull();
  });

  it("persists the selected reasoning effort and sends it with the Run", async () => {
    const session = sessionFixture();
    prepareQueries(session);
    mocks.startRun.mockResolvedValue({
      run: runFixture({ status: "running" }),
      session,
    });
    mocks.streamAgentRun.mockImplementation(async (_input, onEvent) => {
      await onEvent(
        streamEvent("run.completed", {
          run: runFixture({ status: "completed" }),
        }),
      );
      return "event-run.completed";
    });

    render(<AgentWorkbench />, { wrapper: Providers });
    await screen.findByText("历史消息");
    fireEvent.change(screen.getByLabelText("思考强度"), {
      target: { value: "xhigh" },
    });
    await sendMessage("深度分析");

    expect(mocks.startRun).toHaveBeenCalledWith(
      "project-1",
      "instance-1",
      "session-1",
      "深度分析",
      [],
      "xhigh",
    );
    expect(
      window.localStorage.getItem(
        "mmdash.agent.reasoning-effort.project-1.instance-1",
      ),
    ).toBe("xhigh");
  });

  it("recovers a running Run after switching away from and back to its Session", async () => {
    const runningSession = sessionFixture({ last_run_id: "run-1" });
    const secondary = sessionFixture({
      default: false,
      remote_session_id: "hermes-session-2",
      session_id: "session-2",
      title: "Secondary",
    });
    prepareQueries(runningSession);
    mocks.listSessions.mockResolvedValue({ items: [runningSession, secondary] });
    mocks.getRun.mockResolvedValue(runFixture({ status: "running" }));
    mocks.streamAgentRun.mockResolvedValue("event-running");

    render(<AgentWorkbench />, { wrapper: Providers });
    await waitFor(() => expect(mocks.streamAgentRun).toHaveBeenCalledTimes(1));
    expect(screen.getByRole("button", { name: "停止输出" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Secondary active" }));
    await waitFor(() =>
      expect(new URL(window.location.href).searchParams.get("session")).toBe(
        "session-2",
      ),
    );
    const streamCallsBeforeReturn = mocks.streamAgentRun.mock.calls.length;
    fireEvent.click(screen.getByRole("button", { name: /^Main .*active/ }));

    await waitFor(() =>
      expect(mocks.streamAgentRun.mock.calls.length).toBeGreaterThan(
        streamCallsBeforeReturn,
      ),
    );
    expect(screen.getByRole("button", { name: "停止输出" })).toBeInTheDocument();
    expect(mocks.getRun).toHaveBeenCalledWith(
      "project-1",
      "instance-1",
      "session-1",
      "run-1",
    );
  });

  it("merges repeated cumulative stream deltas without duplicating text", async () => {
    const session = sessionFixture({ last_run_id: "run-1" });
    prepareQueries(session);
    mocks.listMessages.mockResolvedValue({ items: [] });
    mocks.getRun.mockResolvedValue(runFixture({ status: "running" }));
    mocks.streamAgentRun.mockImplementation(async (_input, onEvent) => {
      await onEvent(
        streamEvent("message.started", { message_id: "message-repeated" }),
      );
      await onEvent(
        streamEvent("message.delta", {
          delta: "流式回答只有一份",
          message_id: "message-repeated",
        }),
      );
      await onEvent(
        streamEvent("message.delta", {
          delta: "流式回答只有一份",
          message_id: "message-repeated",
        }),
      );
      await onEvent(
        streamEvent("run.completed", {
          run: runFixture({ status: "completed" }),
        }),
      );
      return "event-run.completed";
    });

    render(<AgentWorkbench />, { wrapper: Providers });

    expect(await screen.findByText("流式回答只有一份")).toBeInTheDocument();
    expect(screen.queryByText("流式回答只有一份流式回答只有一份")).toBeNull();
  });

  it("uploads composer files as Artifacts and binds them to the next Run", async () => {
    const session = sessionFixture();
    prepareQueries(session);
    mocks.multipartStart.mockResolvedValue({
      artifact: { artifact_id: "artifact-input-1" },
      current_version: { version_id: "version-input-1" },
    });
    mocks.startRun.mockResolvedValue({
      run: runFixture({ status: "running" }),
      session,
    });
    mocks.streamAgentRun.mockResolvedValue("event-final");

    const rendered = render(<AgentWorkbench />, { wrapper: Providers });
    await screen.findByText("历史消息");
    const input =
      rendered.container.querySelector<HTMLInputElement>('input[type="file"]');
    expect(input).not.toBeNull();
    const file = new File(["attachment-memory-cobalt"], "memory.txt", {
      type: "text/plain",
    });
    fireEvent.change(input!, { target: { files: [file] } });

    expect(await screen.findByText("memory.txt")).toBeInTheDocument();
    await waitFor(() => expect(mocks.multipartStart).toHaveBeenCalled());
    fireEvent.change(screen.getByLabelText("发给 Hermes 的消息"), {
      target: { value: "读取附件" },
    });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));

    await waitFor(() =>
      expect(mocks.startRun).toHaveBeenCalledWith(
        "project-1",
        "instance-1",
        "session-1",
        "读取附件",
        ["artifact-input-1"],
      ),
    );
    expect(mocks.multipartOptions).toEqual([
      expect.objectContaining({
        file,
        kind: "attachment",
        projectId: "project-1",
        tags: ["agent-chat"],
      }),
    ]);
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
