import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  ProjectProvider,
  useCurrentProject,
} from "@/components/providers/project-provider";

const mocks = vi.hoisted(() => ({
  request: vi.fn(),
}));

vi.mock("@/lib/api-client", () => ({
  apiClient: { request: mocks.request },
}));

function ProjectRole() {
  const project = useCurrentProject();
  return <span>{project.role ?? "loading"}</span>;
}

function Providers({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false, staleTime: 30_000 },
          },
        })
      }
    >
      {children}
    </QueryClientProvider>
  );
}

describe("project provider", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("refreshes placeholder project data immediately to load the current role", async () => {
    mocks.request.mockResolvedValue({
      created_by: "user-owner",
      id: "project-1",
      name: "Modeling Team",
      role: "owner",
    });

    render(
      <ProjectProvider project={{ id: "project-1", name: "加载项目…" }}>
        <ProjectRole />
      </ProjectProvider>,
      { wrapper: Providers },
    );

    expect(screen.getByText("loading")).toBeInTheDocument();
    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith("/projects/project-1"),
    );
    expect(await screen.findByText("owner")).toBeInTheDocument();
  });
});
