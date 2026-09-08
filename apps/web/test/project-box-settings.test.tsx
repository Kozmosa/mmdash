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

const mocks = vi.hoisted(() => ({
  projectId: "00000000-0000-4000-8000-000000000001",
  userId: "00000000-0000-4000-8000-000000000009",
  role: "owner",
  assignBox: vi.fn(),
  removeBox: vi.fn(),
  assignedBox: {
    box_id: "00000000-0000-4000-8000-0000000000b1",
    owner_user_id: "00000000-0000-4000-8000-000000000009",
    name: "Lab Box",
    status: "online",
    version: "0.1.0",
    capabilities: [],
    runtimes: [{ name: "local-docker", version: "1" }],
    limits: {
      cpu_millis: 1000,
      memory_bytes: 1 << 30,
      timeout_seconds: 3600,
      disk_bytes: 1 << 30,
      pids: 128,
      network: "enabled",
    },
    load: { running_tasks: 0, capacity: 2, cpu_millis: 0, memory_bytes: 0 },
    project_assignments: [],
  },
  personalBox: {
    box_id: "00000000-0000-4000-8000-0000000000b2",
    owner_user_id: "00000000-0000-4000-8000-000000000009",
    name: "Spare Box",
    status: "online",
    version: "0.1.0",
    capabilities: [],
    runtimes: [{ name: "e2b", version: "1" }],
    limits: {
      cpu_millis: 1000,
      memory_bytes: 1 << 30,
      timeout_seconds: 3600,
      disk_bytes: 1 << 30,
      pids: 128,
      network: "enabled",
    },
    load: { running_tasks: 1, capacity: 4, cpu_millis: 0, memory_bytes: 0 },
    project_assignments: [],
  },
}));

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({
    id: mocks.projectId,
    name: "Project",
    role: mocks.role,
  }),
}));

vi.mock("@/components/providers/user-provider", () => ({
  useCurrentUser: () => ({ id: mocks.userId, email: "user@test.local" }),
}));

vi.mock("@/features/experiment/api", () => ({
  experimentApi: {
    projectBoxes: vi
      .fn()
      .mockResolvedValue({ items: [mocks.assignedBox] }),
    personalBoxes: vi.fn().mockResolvedValue({ items: [mocks.personalBox] }),
    assignBox: mocks.assignBox,
    removeBox: mocks.removeBox,
  },
}));

import { ProjectBoxSettingsPanel } from "@/features/experiment/project-box-settings-panel";

function withProviders(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  mocks.role = "owner";
  mocks.assignedBox.owner_user_id = mocks.userId;
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("ProjectBoxSettingsPanel", () => {
  it("lists assigned boxes and starts graceful unassignment", async () => {
    mocks.removeBox.mockResolvedValue(undefined);
    render(withProviders(<ProjectBoxSettingsPanel />));

    expect(
      await screen.findByText("Lab Box", { selector: "p" }),
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "等待实验完成后解除" }),
    );
    await waitFor(() =>
      expect(mocks.removeBox).toHaveBeenCalledWith(
        mocks.projectId,
        mocks.assignedBox.box_id,
        false,
      ),
    );
  });

  it("offers personal boxes for assignment to managing roles", async () => {
    mocks.assignBox.mockResolvedValue(undefined);
    render(withProviders(<ProjectBoxSettingsPanel />));

    fireEvent.click(
      await screen.findByRole("button", { name: /^分配$/, exact: true }),
    );
    await waitFor(() =>
      expect(mocks.assignBox).toHaveBeenCalledWith(
        mocks.projectId,
        mocks.personalBox.box_id,
      ),
    );
  });

  it("hides assignment actions from non-managing roles", async () => {
    mocks.role = "viewer";
    mocks.assignedBox.owner_user_id =
      "00000000-0000-4000-8000-0000000000ff";
    render(withProviders(<ProjectBoxSettingsPanel />));

    await screen.findByText("Lab Box", { selector: "p" });
    expect(
      screen.queryByRole("button", { name: "等待实验完成后解除" }),
    ).not.toBeInTheDocument();
  });
});
