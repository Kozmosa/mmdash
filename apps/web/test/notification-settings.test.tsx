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

import { NotificationSettingsPanel } from "@/features/notification/notification-settings-panel";

const mocks = vi.hoisted(() => ({ request: vi.fn() }));
const projectId = "00000000-0000-4000-8000-000000000001";

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({ id: projectId, name: "数模项目", role: "owner" }),
}));

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

describe("notification settings", () => {
  afterEach(cleanup);

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.request.mockImplementation(
      (path: string, options?: { body?: unknown; method?: string }) => {
        if (path.includes("/notification-channels/")) {
          const enabled = path.endsWith("notification.feishu_webhook");
          return Promise.resolve({
            channel_key: path.split("/").at(-1),
            configured: enabled,
            enabled,
            settings_version: enabled ? 2 : 0,
          });
        }
        if (path.endsWith("/notification-rules/progress.reminder.due")) {
          return Promise.resolve({
            channel_keys:
              options?.method === "PUT"
                ? (options.body as { channel_keys: string[] }).channel_keys
                : [],
            external_enabled: options?.method === "PUT",
            minimum_priority: "normal",
            project_id: projectId,
            type_key: "progress.reminder.due",
            version: 1,
          });
        }
        if (path.endsWith("/notification-deliveries")) {
          return Promise.resolve({
            has_more: false,
            items: [
              {
                attempts: 3,
                channel_key: "notification.feishu_webhook",
                created_at: "2026-08-08T00:00:00Z",
                delivery_id: "00000000-0000-4000-8000-000000000030",
                last_error: "provider timeout",
                last_error_code: "timeout",
                notification_id: "00000000-0000-4000-8000-000000000031",
                status: "failed",
              },
            ],
          });
        }
        if (
          path.includes("/notification-deliveries/") &&
          path.endsWith("/retry")
        ) {
          return Promise.resolve({});
        }
        throw new Error(`Unexpected request: ${path}`);
      },
    );
  });

  it("keeps Inbox policy out of project rules and saves only external routing", async () => {
    render(<NotificationSettingsPanel />, { wrapper: Providers });

    expect(await screen.findByText("必须进入 Inbox")).toBeVisible();
    expect(screen.getByText("默认进入 Inbox")).toBeVisible();
    expect(screen.queryByLabelText(/Inbox.*启用/)).not.toBeInTheDocument();
    expect(
      (await screen.findAllByRole("button", { name: "测试连接" }))[0],
    ).toHaveAttribute("type", "button");
    expect(
      screen.getAllByRole("button", { name: "删除渠道" })[0],
    ).toHaveAttribute("type", "button");

    fireEvent.click(
      await screen.findByRole("checkbox", { name: /飞书群机器人.*已启用/ }),
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: "额外发送到外部渠道" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "保存投递规则" }));

    await waitFor(() => {
      const call = mocks.request.mock.calls.find(
        ([path, options]) =>
          String(path).endsWith("/notification-rules/progress.reminder.due") &&
          options?.method === "PUT",
      );
      expect(call?.[1]?.body).toEqual({
        channel_keys: ["notification.feishu_webhook"],
        external_enabled: true,
        minimum_priority: "normal",
        version: 1,
      });
      expect(call?.[1]?.body).not.toHaveProperty("inbox_enabled");
    });
  });

  it("requires an explicit reason before retrying an external delivery", async () => {
    render(<NotificationSettingsPanel />, { wrapper: Providers });

    const retry = await screen.findByRole("button", { name: "重投" });
    expect(retry).toBeDisabled();
    fireEvent.change(screen.getByLabelText("重投原因"), {
      target: { value: "网络恢复后由管理员重试" },
    });
    fireEvent.click(retry);

    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith(
        expect.stringMatching(/notification-deliveries\/.+\/retry$/),
        {
          body: { reason: "网络恢复后由管理员重试" },
          method: "POST",
        },
      ),
    );
  });
});
