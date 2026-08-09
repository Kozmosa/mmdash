import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { NotificationInbox } from "@/features/notification/notification-inbox";

const mocks = vi.hoisted(() => ({ request: vi.fn() }));

vi.mock("@/lib/api-client", () => ({
  apiClient: { request: mocks.request },
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

function Providers({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: { queries: { retry: false } },
        })
      }
    >
      {children}
    </QueryClientProvider>
  );
}

const projectId = "00000000-0000-4000-8000-000000000001";
const inboxItem = {
  archived_at: undefined,
  created_at: "2026-08-08T01:00:00Z",
  inbox_item_id: "00000000-0000-4000-8000-000000000010",
  notification: {
    action: {
      action_resource_id: "00000000-0000-4000-8000-000000000020",
      action_type: "project.invitation.accept",
    },
    created_at: "2026-08-08T01:00:00Z",
    data: { project_name: "raw source data must not be used as copy" },
    notification_id: "00000000-0000-4000-8000-000000000011",
    occurred_at: "2026-08-08T01:00:00Z",
    priority: "high",
    project_id: projectId,
    rendered_snapshot: {
      body: "你被邀请以 maintainer 身份加入项目。",
      title: "加入“数模项目”的邀请",
    },
    resource_id: "00000000-0000-4000-8000-000000000020",
    resource_type: "invitation",
    source_event_id: "00000000-0000-4000-8000-000000000012",
    template_version: 1,
    type_key: "project.invitation.received",
  },
  outcome: "active",
  read_state: "unread",
  updated_at: "2026-08-08T01:00:00Z",
} as const;

describe("Notification Inbox", () => {
  afterEach(cleanup);

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.request.mockImplementation(
      (path: string, options?: { method?: string }) => {
        if (path === "/projects") {
          return Promise.resolve({
            items: [{ id: projectId, name: "数模项目" }],
          });
        }
        if (path === "/inbox" && !options?.method) {
          return Promise.resolve({ has_more: false, items: [inboxItem] });
        }
        if (path === "/inbox/mark-all-read" && options?.method === "POST") {
          return Promise.resolve(undefined);
        }
        throw new Error(`Unexpected request: ${path}`);
      },
    );
  });

  it("defaults to unread, supports processed/project/type filters, and uses the rendered snapshot", async () => {
    render(<NotificationInbox />, { wrapper: Providers });

    expect(await screen.findByText("加入“数模项目”的邀请")).toBeVisible();
    expect(
      screen.getByText("你被邀请以 maintainer 身份加入项目。"),
    ).toBeVisible();
    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith(
        "/inbox",
        expect.objectContaining({
          query: expect.objectContaining({
            archived: "false",
            read_state: "unread",
          }),
        }),
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "已处理" }));
    fireEvent.change(screen.getByLabelText("项目"), {
      target: { value: projectId },
    });
    fireEvent.change(screen.getByLabelText("消息类型"), {
      target: { value: "progress.reminder.due" },
    });

    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith(
        "/inbox",
        expect.objectContaining({
          query: expect.objectContaining({
            outcome_group: "processed",
            project_id: projectId,
            read_state: undefined,
            type_key: "progress.reminder.due",
          }),
        }),
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "全部标为已读" }));
    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith("/inbox/mark-all-read", {
        body: {
          project_id: projectId,
          type_key: "progress.reminder.due",
        },
        method: "POST",
      }),
    );
  });

  it("does not show an empty state at the same time as an error", async () => {
    mocks.request.mockImplementation((path: string) => {
      if (path === "/projects") return Promise.resolve({ items: [] });
      if (path === "/inbox") return Promise.reject(new Error("Inbox offline"));
      throw new Error(`Unexpected request: ${path}`);
    });

    render(<NotificationInbox />, { wrapper: Providers });

    expect(await screen.findByText("无法读取收件箱")).toBeVisible();
    expect(screen.queryByText("没有未读消息")).not.toBeInTheDocument();
  });
});
