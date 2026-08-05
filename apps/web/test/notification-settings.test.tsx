import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { NotificationSettingsPanel } from "@/features/notification/notification-settings-panel";

const mocks = vi.hoisted(() => ({
  request: vi.fn(),
}));

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({ id: "project-1", name: "Project" }),
}));

vi.mock("@/lib/api-client", () => ({
  apiClient: { request: mocks.request },
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

describe("notification delivery diagnostics", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("offers manual retry only for failed deliveries", async () => {
    mocks.request.mockImplementation((path: string) => {
      if (path.includes("/notification-deliveries?")) {
        return Promise.resolve({
          items: [
            {
              attempts: 5,
              channel_key: "notification.generic_webhook",
              created_at: "2026-08-06T00:00:00Z",
              delivery_id: "delivery-failed",
              notification_id: "notification-1",
              status: "failed",
            },
            {
              attempts: 2,
              channel_key: "notification.feishu_webhook",
              created_at: "2026-08-06T00:01:00Z",
              delivery_id: "delivery-retrying",
              notification_id: "notification-2",
              status: "retrying",
            },
          ],
        });
      }
      if (path.includes("/notification-channels/")) {
        return Promise.resolve({
          channel_key: path.split("/").at(-1),
          configured: false,
          enabled: false,
          settings_version: 0,
        });
      }
      if (path.includes("/notification-rules/")) {
        return Promise.resolve({
          channel_keys: [],
          external_enabled: false,
          inbox_enabled: true,
          minimum_priority: "normal",
          project_id: "project-1",
          type_key: path.split("/").at(-1),
          version: 0,
        });
      }
      throw new Error(`unexpected request ${path}`);
    });

    render(<NotificationSettingsPanel />, { wrapper: Providers });

    const retryButton = await screen.findByRole("button", { name: "重试" });
    const failedRow = screen
      .getByText("notification.generic_webhook")
      .closest("li");
    const retryingRow = screen
      .getByText("notification.feishu_webhook")
      .closest("li");

    expect(failedRow).toHaveTextContent("failed");
    expect(failedRow).toContainElement(retryButton);
    expect(retryingRow).toHaveTextContent("retrying");
    expect(retryingRow).not.toContainElement(retryButton);
    expect(screen.getAllByRole("button", { name: "重试" })).toHaveLength(1);
  });
});
