import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/agent/agent-settings-panel", () => ({
  AgentSettingsPanel: () => (
    <section data-testid="agent-settings-panel">Agent 设置面板</section>
  ),
}));
vi.mock("@/features/members/member-management", () => ({
  MemberManagement: () => null,
}));
vi.mock("@/features/notification/notification-settings-panel", () => ({
  NotificationSettingsPanel: () => null,
}));
vi.mock("@/features/repo/repo-settings-panel", () => ({
  RepoSettingsPanel: () => <section>Repo 设置面板</section>,
}));
vi.mock("@/features/settings/registered-settings-panel", () => ({
  RegisteredSettingsPanel: () => null,
}));

import SettingsPage from "@/app/projects/[projectId]/settings/page";
import { settingsSlots } from "@/features/settings/registry";

afterEach(cleanup);

describe("Project settings page", () => {
  it("renders the implemented Agent panel without its stale placeholder", () => {
    render(<SettingsPage />);

    expect(screen.getByTestId("agent-settings-panel")).toBeInTheDocument();
    expect(
      screen.queryByText("等待 agent 模块注册设置面板"),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/自动进度跟踪/)).not.toBeInTheDocument();
    expect(
      settingsSlots.list().find((slot) => slot.id === "agent")?.description,
    ).toBe("Hermes、Session 与 Agent Token");
  });
});
