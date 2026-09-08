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

const mocks = vi.hoisted(() => ({
  projectId: "00000000-0000-4000-8000-000000000001",
  role: "owner",
  settings: {
    project_id: "00000000-0000-4000-8000-000000000001",
    timezone: "Asia/Shanghai",
    default_runtime_policy: "auto",
    default_limits: {
      cpu_millis: 1000,
      memory_bytes: 1 << 30,
      timeout_seconds: 3600,
      disk_bytes: 1 << 30,
      pids: 128,
      network: "enabled",
    },
    git_large_file_threshold_bytes: 52428800,
    updated_by: "00000000-0000-4000-8000-000000000009",
    updated_at: "2026-09-08T00:00:00Z",
  },
  updateSettings: vi.fn(),
}));

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({
    id: mocks.projectId,
    name: "Project",
    role: mocks.role,
  }),
}));

vi.mock("@/features/experiment/api", () => ({
  experimentApi: {
    settings: vi.fn().mockResolvedValue(mocks.settings),
    updateSettings: mocks.updateSettings,
  },
}));

import { ExperimentSettingsPanel } from "@/features/experiment/experiment-settings-panel";

function withProviders(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("ExperimentSettingsPanel", () => {
  it("submits the full settings payload on save", async () => {
    mocks.updateSettings.mockResolvedValue(mocks.settings);
    render(withProviders(<ExperimentSettingsPanel />));

    const timezone = await screen.findByLabelText("IANA 时区");
    expect(timezone).toHaveValue("Asia/Shanghai");
    fireEvent.change(timezone, { target: { value: "UTC" } });
    fireEvent.change(await screen.findByLabelText("默认 Runtime"), {
      target: { value: "local-docker" },
    });
    fireEvent.change(screen.getByLabelText("超时（秒）"), {
      target: { value: "1800" },
    });

    fireEvent.click(screen.getByRole("button", { name: /保存 Experiment 设置/ }));
    await waitFor(() => expect(mocks.updateSettings).toHaveBeenCalledTimes(1));
    expect(mocks.updateSettings).toHaveBeenCalledWith(mocks.projectId, {
      timezone: "UTC",
      default_runtime_policy: "local-docker",
      git_large_file_threshold_bytes: 52428800,
      default_limits: expect.objectContaining({
        cpu_millis: 1000,
        timeout_seconds: 1800,
        network: "enabled",
      }),
    });
  });

  it("degrades to read-only for non-managing roles", async () => {
    mocks.role = "viewer";
    render(withProviders(<ExperimentSettingsPanel />));

    const timezone = await screen.findByLabelText("IANA 时区");
    expect(timezone).toBeDisabled();
    expect(screen.queryByRole("button", { name: /保存/ })).not.toBeInTheDocument();
  });
});
