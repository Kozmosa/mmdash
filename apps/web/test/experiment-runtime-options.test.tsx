import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  projectId: "00000000-0000-4000-8000-000000000001",
  role: "owner",
}));

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({ id: mocks.projectId, name: "Project", role: mocks.role }),
}));

vi.mock("@/features/experiment/api", () => ({
  experimentApi: {
    cancel: vi.fn(),
    compare: vi.fn(),
    create: vi.fn(),
    list: vi.fn().mockResolvedValue({ items: [] }),
    projectBoxes: vi.fn().mockResolvedValue({ items: [] }),
    rerun: vi.fn(),
    run: vi.fn(),
    settings: vi.fn().mockResolvedValue({
      project_id: mocks.projectId,
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
      updated_at: "2026-08-29T00:00:00Z",
    }),
    updateSettings: vi.fn(),
  },
}));

import { ExperimentWorkbench } from "@/features/experiment/experiment-workbench";
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

describe("local-process runtime options", () => {
  it("offers local-process in the workbench and warns about missing isolation", async () => {
    render(withProviders(<ExperimentWorkbench />));
    // The create form lives inside a modal since the cards-layout rework.
    fireEvent.click(await screen.findByRole("button", { name: "新建实验" }));
    const select = await screen.findByLabelText("Runtime 策略");
    expect(select).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "仅 Local Process（裸机）" })).toBeInTheDocument();
    expect(screen.queryByRole("note")).not.toBeInTheDocument();

    fireEvent.change(screen.getByRole("combobox", { name: "Runtime 策略" }), {
      target: { value: "local-process" },
    });

    const warning = screen.getByRole("note");
    expect(warning).toHaveTextContent("trusted-host");
    expect(warning).toHaveTextContent("没有容器隔离");
  });

  it("offers local-process as the project default and warns when selected", async () => {
    render(withProviders(<ExperimentSettingsPanel />));
    const option = await screen.findByRole("option", { name: "仅 Local Process（裸机）" });
    expect(option).toBeInTheDocument();

    fireEvent.change(await screen.findByLabelText("默认 Runtime"), {
      target: { value: "local-process" },
    });

    const warning = await waitFor(() => screen.getByRole("note"));
    expect(warning).toHaveTextContent("没有容器隔离");
  });
});
