import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { WorkspaceSidebar } from "@/components/layout/workspace-sidebar";
import { useWorkspaceStore } from "@/stores/workspace";

vi.mock("next/navigation", () => ({
  usePathname: () => "/projects/project-1",
}));

describe("workspace sidebar", () => {
  beforeEach(() => {
    useWorkspaceStore.setState({ sidebarOpen: false });
  });

  afterEach(() => {
    useWorkspaceStore.setState({ sidebarOpen: true });
  });

  it("reveals the expand control over the logo when collapsed", () => {
    render(<WorkspaceSidebar projectId="project-1" />);

    const expandButton = screen.getByRole("button", { name: "展开导航" });
    expect(expandButton).toHaveClass(
      "group-hover:pointer-events-auto",
      "group-hover:opacity-100",
    );

    fireEvent.click(expandButton);

    expect(useWorkspaceStore.getState().sidebarOpen).toBe(true);
    expect(
      screen.getByRole("button", { name: "收起导航" }),
    ).toBeInTheDocument();
  });
});
