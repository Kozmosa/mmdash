import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({
    id: "00000000-0000-4000-8000-000000000001",
    name: "Settings Project",
    role: "owner",
  }),
}));
vi.mock("@/features/agent/agent-settings-panel", () => ({
  AgentSettingsPanel: () => (
    <section data-testid="agent-settings-panel">Agent 设置面板</section>
  ),
}));
vi.mock("@/features/model/model-settings-panel", () => ({
  ModelSettingsPanel: () => null,
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
vi.mock("@/features/progress/progress-settings-panel", () => ({
  ProgressSettingsPanel: () => (
    <section data-testid="progress-settings-panel">
      Progress 自动评估设置
    </section>
  ),
}));
vi.mock("@/features/experiment/experiment-settings-panel", () => ({
  ExperimentSettingsPanel: () => (
    <section data-testid="experiment-settings-panel">Experiment 设置面板</section>
  ),
}));
vi.mock("@/features/experiment/project-box-settings-panel", () => ({
  ProjectBoxSettingsPanel: () => (
    <section data-testid="project-box-settings-panel">Project Box 设置面板</section>
  ),
}));
vi.mock("@/features/settings/registered-settings-panel", () => ({
  RegisteredSettingsPanel: () => null,
}));

import SettingsPage from "@/app/projects/[projectId]/settings/page";
import { settingsSlots } from "@/features/settings/registry";

afterEach(cleanup);

describe("Project settings page", () => {
  it("renders implemented Agent and Progress panels without stale placeholders", () => {
    render(<SettingsPage />);

    expect(screen.getByTestId("agent-settings-panel")).toBeInTheDocument();
    expect(
      screen.queryByText("等待 agent 模块注册设置面板"),
    ).not.toBeInTheDocument();
    expect(screen.getByTestId("progress-settings-panel")).toBeInTheDocument();
    expect(
      screen.queryByText("等待 progress 模块注册设置面板"),
    ).not.toBeInTheDocument();
    expect(screen.getByTestId("experiment-settings-panel")).toBeInTheDocument();
    expect(screen.getByTestId("project-box-settings-panel")).toBeInTheDocument();
    expect(
      screen.queryByText("等待 experiment 模块注册设置面板"),
    ).not.toBeInTheDocument();
    expect(
      settingsSlots.list().find((slot) => slot.id === "agent")?.description,
    ).toBe("Hermes、Session 与 Agent Token");
  });
});
