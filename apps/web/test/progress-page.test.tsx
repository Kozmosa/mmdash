import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import ProgressPage from "@/app/projects/[projectId]/progress/page";

const mocks = vi.hoisted(() => ({
  project: {
    id: "project-1",
    name: "Modeling Team",
    role: "owner" as "agent" | "box" | "editor" | "maintainer" | "owner" | "viewer",
  },
  request: vi.fn(),
}));

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => mocks.project,
}));

vi.mock("@/lib/api-client", () => ({
  apiClient: { request: mocks.request },
}));

const progress = {
  blocked: [],
  board: { blocked: [], done: [], in_progress: [], todo: [] },
  gantt: [],
  milestones: [
    {
      critical: true,
      description: "",
      milestone_id: "00000000-0000-4000-8000-000000000011",
      status: "planned",
      title: "模型冻结",
    },
  ],
  overdue: [],
  proposals: [],
  reminders: [
    {
      note: "现有提醒",
      remind_at: "2031-01-01T00:00:00.000Z",
      reminder_id: "00000000-0000-4000-8000-000000000031",
      status: "pending",
    },
  ],
  tasks: [
    {
      description: "",
      source: "human",
      status: "todo",
      task_id: "00000000-0000-4000-8000-000000000021",
      title: "整理参数",
    },
  ],
  today: [],
};

function renderProgress(role: typeof mocks.project.role = "owner") {
  mocks.project.role = role;
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false, staleTime: 30_000 },
    },
  });
  const invalidate = vi.spyOn(queryClient, "invalidateQueries");
  render(<ProgressPage />, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  });
  return { invalidate };
}

function useSuccessfulRequests() {
  mocks.request.mockImplementation((path: string, options?: { method?: string }) => {
    if (path === "/projects/project-1/progress" && !options) return Promise.resolve(progress);
    if (path === "/projects/project-1/progress/reminders" && options?.method === "POST") {
      return Promise.resolve({ reminder_id: "created-reminder" });
    }
    return Promise.reject(new Error(`Unexpected request: ${path}`));
  });
}

function reminderPost() {
  return mocks.request.mock.calls.find(
    ([path, options]) => path === "/projects/project-1/progress/reminders" && options?.method === "POST",
  );
}

describe("Progress reminder creation", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    mocks.project.role = "owner";
  });

  it("creates a Task reminder with local datetime converted to ISO and refreshes related queries", async () => {
    useSuccessfulRequests();
    const { invalidate } = renderProgress();
    await screen.findByRole("option", { name: "整理参数" });

    fireEvent.change(screen.getByLabelText("提醒目标"), {
      target: { value: progress.tasks[0].task_id },
    });
    const localDateTime = "2030-01-02T12:30";
    fireEvent.change(screen.getByLabelText("提醒时间"), {
      target: { value: localDateTime },
    });
    fireEvent.change(screen.getByLabelText("提醒备注"), {
      target: { value: "检查实验输入" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建提醒" }));

    await waitFor(() => expect(reminderPost()).toBeDefined());
    expect(reminderPost()?.[1]).toEqual({
      body: {
        note: "检查实验输入",
        remind_at: new Date(localDateTime).toISOString(),
        task_id: progress.tasks[0].task_id,
      },
      method: "POST",
    });
    expect(await screen.findByRole("status")).toHaveTextContent("提醒已创建，列表已刷新。");
    expect(screen.getByLabelText("提醒目标")).toHaveValue("");
    expect(screen.getByLabelText("提醒时间")).toHaveValue("");
    expect(screen.getByLabelText("提醒备注")).toHaveValue("");
    await waitFor(() => {
      expect(invalidate).toHaveBeenCalledWith({ queryKey: ["progress", "project-1"] });
      expect(invalidate).toHaveBeenCalledWith({ queryKey: ["project-home", "project-1"] });
      expect(mocks.request.mock.calls.filter(([path]) => path === "/projects/project-1/progress")).toHaveLength(2);
    });
  });

  it("switches targets mutually and creates a Milestone reminder without a Task field", async () => {
    useSuccessfulRequests();
    renderProgress("maintainer");
    await screen.findByRole("option", { name: "整理参数" });
    fireEvent.change(screen.getByLabelText("提醒目标"), {
      target: { value: progress.tasks[0].task_id },
    });

    fireEvent.click(screen.getByLabelText("目标类型：Milestone"));
    expect(screen.getByLabelText("提醒目标")).toHaveValue("");
    expect(screen.queryByRole("option", { name: "整理参数" })).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: "模型冻结" })).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("提醒目标"), {
      target: { value: progress.milestones[0].milestone_id },
    });
    const alreadyDueLocalTime = "2020-02-03T04:05";
    fireEvent.change(screen.getByLabelText("提醒时间"), {
      target: { value: alreadyDueLocalTime },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建提醒" }));

    await waitFor(() => expect(reminderPost()).toBeDefined());
    expect(reminderPost()?.[1]).toEqual({
      body: {
        milestone_id: progress.milestones[0].milestone_id,
        remind_at: new Date(alreadyDueLocalTime).toISOString(),
      },
      method: "POST",
    });
    expect(reminderPost()?.[1].body).not.toHaveProperty("task_id");
  });

  it("enforces target, datetime, and note boundaries before sending", async () => {
    useSuccessfulRequests();
    renderProgress("editor");
    await screen.findByRole("option", { name: "整理参数" });
    const submit = screen.getByRole("button", { name: "创建提醒" });
    expect(submit).toBeDisabled();

    fireEvent.change(screen.getByLabelText("提醒目标"), {
      target: { value: progress.tasks[0].task_id },
    });
    expect(submit).toBeDisabled();
    fireEvent.change(screen.getByLabelText("提醒时间"), {
      target: { value: "2030-01-02T12:30" },
    });
    expect(submit).toBeEnabled();
    fireEvent.change(screen.getByLabelText("提醒备注"), {
      target: { value: "x".repeat(2_001) },
    });
    expect(submit).toBeDisabled();
    expect(reminderPost()).toBeUndefined();
  });

  it("prevents duplicate submission and shows a stable error without response details", async () => {
    let rejectReminder: (reason: Error) => void = () => undefined;
    const pendingReminder = new Promise((_, reject) => {
      rejectReminder = reject;
    });
    mocks.request.mockImplementation((path: string, options?: { method?: string }) => {
      if (path === "/projects/project-1/progress" && !options) return Promise.resolve(progress);
      if (path === "/projects/project-1/progress/reminders" && options?.method === "POST") return pendingReminder;
      return Promise.reject(new Error(`Unexpected request: ${path}`));
    });
    renderProgress();
    await screen.findByRole("option", { name: "整理参数" });
    fireEvent.change(screen.getByLabelText("提醒目标"), {
      target: { value: progress.tasks[0].task_id },
    });
    fireEvent.change(screen.getByLabelText("提醒时间"), {
      target: { value: "2030-01-02T12:30" },
    });
    const submit = screen.getByRole("button", { name: "创建提醒" });
    fireEvent.click(submit);
    fireEvent.click(submit);
    await waitFor(() => expect(mocks.request.mock.calls.filter(([path, options]) => path === "/projects/project-1/progress/reminders" && options?.method === "POST")).toHaveLength(1));

    rejectReminder(new Error("raw provider response with secret-token"));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("创建提醒失败，请稍后重试。");
    expect(alert).not.toHaveTextContent("secret-token");
  });

  it.each(["viewer", "agent", "box"] as const)("hides human mutation controls from %s roles", async (role) => {
    useSuccessfulRequests();
    renderProgress(role);
    await screen.findByText("现有提醒");
    expect(screen.queryByRole("button", { name: "创建提醒" })).not.toBeInTheDocument();
    expect(screen.queryByText("建立关键节点")).not.toBeInTheDocument();
    expect(screen.queryByText("建立任务")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "触发事件" })).not.toBeInTheDocument();
    expect(mocks.request.mock.calls.some(([, options]) => options?.method === "POST")).toBe(false);
  });
});

describe("Progress gantt timeline", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    mocks.project.role = "owner";
  });

  it("renders bars at their real shared date offsets and widths", async () => {
    const gantt = [
      {
        id: "00000000-0000-4000-8000-000000000041",
        kind: "milestone",
        status: "planned",
        title: "模型冻结",
        start_at: "2030-01-01T00:00:00Z",
        target_at: "2030-01-11T00:00:00Z",
      },
      {
        id: "00000000-0000-4000-8000-000000000042",
        kind: "task",
        status: "in_progress",
        title: "整理参数",
        start_at: "2030-01-06T00:00:00Z",
        target_at: "2030-01-21T00:00:00Z",
      },
    ];
    mocks.request.mockImplementation((path: string, options?: { method?: string }) => {
      if (path === "/projects/project-1/progress" && !options) return Promise.resolve({ ...progress, gantt });
      return Promise.reject(new Error(`Unexpected request: ${path}`));
    });

    renderProgress();
    await screen.findByText("现有提醒");
    fireEvent.click(screen.getByRole("button", { name: "甘特" }));

    expect(screen.getByTestId("gantt-range")).toHaveTextContent("2030-01-01 — 2030-01-21");
    expect(screen.getByTestId("gantt-bar-00000000-0000-4000-8000-000000000041")).toHaveStyle({ left: "0%", width: "50%" });
    expect(screen.getByTestId("gantt-bar-00000000-0000-4000-8000-000000000042")).toHaveStyle({ left: "25%", width: "75%" });
    expect(screen.getByTestId("gantt-item-00000000-0000-4000-8000-000000000041")).toHaveTextContent("milestone · planned");
    expect(screen.getByTestId("gantt-item-00000000-0000-4000-8000-000000000042")).toHaveTextContent("task · in_progress");
    expect(screen.getAllByTestId("gantt-tick")).toHaveLength(5);
  });

  it("shows the safe empty state when all timeline dates are missing", async () => {
    mocks.request.mockImplementation((path: string, options?: { method?: string }) => {
      if (path === "/projects/project-1/progress" && !options) {
        return Promise.resolve({
          ...progress,
          gantt: [{ id: "00000000-0000-4000-8000-000000000043", kind: "task", status: "todo", title: "未安排" }],
        });
      }
      return Promise.reject(new Error(`Unexpected request: ${path}`));
    });

    renderProgress();
    await screen.findByText("现有提醒");
    fireEvent.click(screen.getByRole("button", { name: "甘特" }));

    expect(screen.getByText("暂无时间安排")).toBeInTheDocument();
    expect(screen.queryByTestId("gantt-timeline")).not.toBeInTheDocument();
  });
});
