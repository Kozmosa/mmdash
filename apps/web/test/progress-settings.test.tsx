import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ProgressSettingsPanel } from "@/features/progress/progress-settings-panel";

const mocks = vi.hoisted(() => ({ request: vi.fn() }));

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({
    id: "project-1",
    name: "Settings Project",
    role: "owner",
  }),
}));
vi.mock("@/lib/api-client", () => ({ apiClient: { request: mocks.request } }));

const settings = {
  auto_task_changes: false,
  auto_tracking_enabled: false,
  cron_enabled: false,
  cron_schedule: "0 */6 * * *",
  cron_sync_status: "disabled",
  debounce_seconds: 60,
  evaluator_mode: "core_agent",
  event_triggers_enabled: true,
  min_interval_seconds: 300,
  project_id: "project-1",
  updated_at: "2026-08-11T08:00:00Z",
  updated_by: "user-1",
};

function renderPanel() {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  render(<ProgressSettingsPanel />, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });
}

describe("Progress settings", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("keeps the automatic evaluation switch in Project settings and persists it", async () => {
    mocks.request.mockImplementation(
      (path: string, options?: { body?: unknown; method?: string }) => {
        if (path === "/projects/project-1/progress/settings" && !options) {
          return Promise.resolve(settings);
        }
        if (path === "/projects/project-1/agent-instances" && !options) {
          return Promise.resolve({
            items: [
              {
                agent_instance_id: "00000000-0000-4000-8000-000000000061",
                display_name: "Hermes",
                grant: { status: "active" },
                status: "active",
              },
            ],
          });
        }
        if (
          path === "/projects/project-1/progress/settings" &&
          options?.method === "PATCH"
        ) {
          return Promise.resolve({
            ...settings,
            ...(options.body as Record<string, unknown>),
          });
        }
        return Promise.reject(new Error(`Unexpected request: ${path}`));
      },
    );

    renderPanel();

    const automatic = await screen.findByRole("switch", {
      name: "启用自动进度评估",
    });
    expect(automatic).not.toBeChecked();
    fireEvent.change(
      screen.getByRole("combobox", { name: "设置 Progress Agent" }),
      {
        target: { value: "00000000-0000-4000-8000-000000000061" },
      },
    );
    fireEvent.click(automatic);
    fireEvent.click(screen.getByRole("button", { name: "保存自动评估设置" }));

    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith(
        "/projects/project-1/progress/settings",
        {
          body: expect.objectContaining({
            agent_instance_id: "00000000-0000-4000-8000-000000000061",
            auto_tracking_enabled: true,
          }),
          method: "PATCH",
        },
      ),
    );
  });
});
