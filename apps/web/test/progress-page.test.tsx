import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import ProgressPage from "@/app/projects/[projectId]/progress/page";
import { localDayKey } from "@/features/progress/calendar-time";

const mocks = vi.hoisted(() => ({
  project: { id: "project-1", name: "Nanako", role: "owner" as const },
  request: vi.fn(),
}));

vi.mock("@/components/providers/project-provider", () => ({ useCurrentProject: () => mocks.project }));
vi.mock("@/lib/api-client", () => ({ apiClient: { request: mocks.request } }));

function isoToday(hour: number, minute = 0) {
  const value = new Date();
  value.setHours(hour, minute, 0, 0);
  return value.toISOString();
}

const completeProposal = {
  changes: {},
  created_at: isoToday(7),
  proposal_id: "00000000-0000-4000-8000-000000000041",
  proposal_type: "task.complete",
  rationale: "已发现交付物和验证记录",
  source: "system",
  source_evaluation_id: "00000000-0000-4000-8000-000000000051",
  status: "pending",
  target_id: "00000000-0000-4000-8000-000000000021",
  title: "确认整理参数已完成",
};

const progress = {
  blocked: [],
  board: { blocked: [], done: [], in_progress: [], todo: [] },
  gantt: [],
  latest_evaluation: {
    attempts: 1, blockers: [], changes_since_last: [], completed_items: ["整理参数"], created_at: isoToday(7), detected_stage: "execution", evaluation_id: "00000000-0000-4000-8000-000000000051", evaluator_mode: "core_agent", in_progress_items: [], input_snapshot: {}, input_version: "a".repeat(64), pending_questions: [], project_id: "project-1", request_id: "00000000-0000-4000-8000-000000000052", requested_by: "00000000-0000-4000-8000-000000000001", risks: [], source_event_ids: [], status: "succeeded", summary: "已完成一次评估", trigger_kind: "manual", triggers: [], updated_at: isoToday(7),
  },
  milestones: [{ critical: true, description: "冻结输入", milestone_id: "00000000-0000-4000-8000-000000000011", project_id: "project-1", source: "human", status: "planned", target_at: isoToday(13), target_has_time: true, title: "模型冻结" }],
  overdue: [],
  proposals: [completeProposal],
  reminders: [],
  settings: { auto_task_changes: false, auto_tracking_enabled: true, cron_enabled: false, cron_schedule: "0 */6 * * *", cron_sync_status: "disabled", debounce_seconds: 60, evaluator_mode: "core_agent", event_triggers_enabled: true, min_interval_seconds: 300, project_id: "project-1", updated_at: isoToday(7), updated_by: "00000000-0000-4000-8000-000000000001" },
  tasks: [{ description: "收拢建模参数", due_at: isoToday(11), manual_override_fields: [], project_id: "project-1", source: "human", start_at: isoToday(9), status: "in_progress", work_state: "in_progress", task_id: "00000000-0000-4000-8000-000000000021", title: "整理参数" }],
  today: [],
  tracking: { blockers: ["等待算力"], changes_since_last: [], completed_items: ["整理参数"], detected_stage: "execution", effective_stage: "execution", in_progress_items: [], last_evaluated_at: isoToday(7), pending_questions: [], project_id: "project-1", stage_overridden: false, summary: "评估完成，等待确认。", updated_at: isoToday(7) },
};

function renderPage() {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false }, queries: { retry: false } } });
  render(<ProgressPage />, { wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider> });
}

function useRequests() {
  mocks.request.mockImplementation((path: string, options?: { method?: string }) => {
    if (path === "/projects/project-1/progress" && !options) return Promise.resolve(progress);
    if (path === "/projects/project-1/agent-instances" && !options) return Promise.resolve({ items: [{ agent_instance_id: "00000000-0000-4000-8000-000000000061", display_name: "Hermes", grant: { status: "active" }, status: "active" }] });
    if (options?.method) return Promise.resolve({ status: "ok" });
    return Promise.reject(new Error(`Unexpected request: ${path}`));
  });
}

describe("Progress human workbench", () => {
  afterEach(() => { cleanup(); vi.clearAllMocks(); });

  it("starts in Calendar and cycles the repeated multi-day button", async () => {
    useRequests(); renderPage();
    expect(await screen.findByRole("region", { name: "进度日历" })).toBeInTheDocument();
    const multi = screen.getByRole("button", { name: "重复点击切换双日、三日、四日" });
    expect(multi).toHaveTextContent("双日");
    fireEvent.click(multi); expect(multi).toHaveTextContent("三日");
    fireEvent.click(multi); expect(multi).toHaveTextContent("四日");
    fireEvent.click(multi); expect(multi).toHaveTextContent("双日");
  });

  it("shows AI completion as a yellow review state and accepts it from the task card", async () => {
    useRequests(); renderPage();
    const accept = await screen.findByRole("button", { name: "认可 整理参数 已完成" });
    expect(accept.closest("article")).toHaveAttribute("data-progress-status", "ai-complete");
    fireEvent.click(accept);
    await waitFor(() => expect(mocks.request).toHaveBeenCalledWith(
      "/projects/project-1/progress/proposals/00000000-0000-4000-8000-000000000041/review",
      { body: { decision: "accepted" }, method: "POST" },
    ));
    expect(screen.getByText("进行中")).toBeInTheDocument();
  });

  it("opens creation from a double-click and uses the 15-minute calendar position", async () => {
    useRequests(); renderPage();
    const day = await screen.findByTestId(`calendar-day-${localDayKey(new Date())}`);
    fireEvent.doubleClick(day, { clientY: 91 });
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("新建安排")).toBeInTheDocument();
    expect(screen.getByLabelText("标题")).toBeInTheDocument();
  });

  it("renders completion controls for a timed milestone in both the node strip and time grid", async () => {
    useRequests(); renderPage();
    const controls = await screen.findAllByRole("button", { name: "将 模型冻结 标为完成" });
    expect(controls).toHaveLength(2);
    expect(controls.some((control) => control.closest(`[data-testid="calendar-day-${localDayKey(new Date())}"]`))).toBe(true);
  });

  it("stretches the task card live while resizing and snaps to 15 minutes", async () => {
    useRequests(); renderPage();
    const task = await screen.findByTestId("calendar-task-00000000-0000-4000-8000-000000000021");
    expect(task).toHaveStyle({ height: "144px" });
    fireEvent(screen.getByRole("button", { name: "调整 整理参数 结束时间" }), new MouseEvent("pointerdown", { bubbles: true, button: 0, clientX: 100, clientY: 100 }));
    await screen.findByTestId("calendar-resize-preview");
    fireEvent(window, new MouseEvent("pointermove", { bubbles: true, clientX: 100, clientY: 118 }));
    await waitFor(() => expect(task).toHaveStyle({ height: "162px" }));
    expect(task).toHaveTextContent("09:00–11:15");
    fireEvent(window, new MouseEvent("pointerup", { bubbles: true, clientX: 100, clientY: 118 }));
  });

  it("shows a translucent task ghost that follows pointer movement", async () => {
    useRequests(); renderPage();
    const task = await screen.findByTestId("calendar-task-00000000-0000-4000-8000-000000000021");
    const movable = task.querySelector<HTMLElement>(".cursor-grab");
    expect(movable).not.toBeNull();
    fireEvent(movable!, new MouseEvent("pointerdown", { bubbles: true, button: 0, clientX: 100, clientY: 100 }));
    const hint = await screen.findByText("松开以放置 · 15 分钟吸附");
    fireEvent(window, new MouseEvent("pointermove", { bubbles: true, clientX: 140, clientY: 180 }));
    await waitFor(() => expect(hint.parentElement).toHaveStyle({ left: "152px", top: "192px" }));
    expect(hint.parentElement).toHaveClass("opacity-70");
    expect(task).toHaveClass("opacity-30");
    fireEvent(window, new MouseEvent("pointerup", { bubbles: true, clientX: 140, clientY: 180 }));
  });

  it("updates human completion immediately while the server request is still pending", async () => {
    useRequests();
    mocks.request.mockImplementation((path: string, options?: { method?: string }) => {
      if (path === "/projects/project-1/progress" && !options) return Promise.resolve(progress);
      if (path === "/projects/project-1/agent-instances" && !options) return Promise.resolve({ items: [] });
      if (path.endsWith("/milestones/00000000-0000-4000-8000-000000000011") && options?.method === "PATCH") return new Promise(() => undefined);
      if (options?.method) return Promise.resolve({ status: "ok" });
      return Promise.reject(new Error(`Unexpected request: ${path}`));
    });
    renderPage();
    const controls = await screen.findAllByRole("button", { name: "将 模型冻结 标为完成" });
    fireEvent.click(controls[0]!);
    expect(await screen.findAllByRole("button", { name: "将 模型冻结 标为未完成" })).toHaveLength(2);
  });

  it("switches to the single TODO stream and exposes period-depth headings", async () => {
    useRequests(); renderPage();
    await screen.findByRole("region", { name: "进度日历" });
    fireEvent.click(screen.getByRole("button", { name: "TODO" }));
    expect(screen.getByRole("region", { name: "TODO 时间流" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "上午/下午/夜晚/半夜" }));
    expect(screen.getAllByText("上午").length).toBeGreaterThan(0);
    expect(screen.getAllByText("半夜").length).toBeGreaterThan(0);
  });

  it("batch-approves all actionable suggestions from the information rail", async () => {
    useRequests(); renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "全部批准" }));
    await waitFor(() => expect(mocks.request).toHaveBeenCalledWith(
      "/projects/project-1/progress/proposals/batch-review",
      { body: { decision: "accepted", proposal_ids: [completeProposal.proposal_id] }, method: "POST" },
    ));
  });

  it("requires and persists the Progress Agent used by manual evaluation", async () => {
    useRequests(); renderPage();
    const trigger = await screen.findByRole("button", { name: "立即评估" });
    expect(trigger).toBeDisabled();
    fireEvent.change(await screen.findByRole("combobox", { name: "Progress Agent" }), { target: { value: "00000000-0000-4000-8000-000000000061" } });
    await waitFor(() => expect(mocks.request).toHaveBeenCalledWith(
      "/projects/project-1/progress/settings",
      {
        body: expect.objectContaining({ agent_instance_id: "00000000-0000-4000-8000-000000000061" }),
        method: "PATCH",
      },
    ));
  });
});
