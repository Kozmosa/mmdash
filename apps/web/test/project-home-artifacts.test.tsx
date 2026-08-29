import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import ProjectHomePage from "@/app/projects/[projectId]/page";

const apiRequest = vi.hoisted(() => vi.fn());

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({
    id: "00000000-0000-4000-8000-000000000001",
    name: "Artifact Project",
    role: "owner",
  }),
}));

vi.mock("@/lib/api-client", () => ({
  apiClient: { request: apiRequest },
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

describe("Project home Artifact aggregate", () => {
  afterEach(() => {
    cleanup();
    apiRequest.mockReset();
  });

  it("renders the validated source Artifact from the typed home fragment", async () => {
    apiRequest.mockResolvedValue({
      fragments: {
        home: {
          agent: { available: false, items: [], total: 0 },
          article: { available: false, items: [], total: 0 },
          experiments: { available: false, items: [], total: 0 },
          generated_at: "2026-07-30T00:00:00Z",
          milestones: { available: false, items: [], total: 0 },
          models: { available: false, items: [], total: 0 },
          problem: {
            available: true,
            items: [
              {
                detail: {
                  artifact: {
                    artifact_id: "00000000-0000-4000-8000-000000000002",
                    kind: "problem",
                    name: "Problem statement",
                  },
                  current_version: {
                    filename: "problem.pdf",
                    size_bytes: 42,
                  },
                },
                previews: { items: [] },
              },
            ],
            total: 1,
          },
          project_id: "00000000-0000-4000-8000-000000000001",
          todos: { available: false, items: [], total: 0 },
        },
      },
    });

    render(<ProjectHomePage />, { wrapper: Providers });

    expect(await screen.findByText("Problem statement")).toBeInTheDocument();
    expect(screen.getByText("problem.pdf")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /Problem statement/ }),
    ).toHaveAttribute(
      "href",
      "/projects/00000000-0000-4000-8000-000000000001/artifacts?artifact=00000000-0000-4000-8000-000000000002",
    );
  });

  it("replaces the Stage 4 placeholder with live Model question links", async () => {
    apiRequest.mockResolvedValue({
      fragments: {
        home: {
          agent: { available: false, items: [], total: 0 },
          article: { available: false, items: [], total: 0 },
          experiments: { available: false, items: [], total: 0 },
          generated_at: "2026-08-09T00:00:00Z",
          milestones: { available: true, items: [], total: 0 },
          models: {
            available: true,
            items: [
              {
                code: "Q1",
                question_id: "00000000-0000-4000-8000-000000000003",
                snapshot_count: 2,
                sync_status: "succeeded",
                title: "传染病传播模型",
              },
            ],
            total: 1,
          },
          problem: { available: true, items: [], total: 0 },
          project_id: "00000000-0000-4000-8000-000000000001",
          todos: { available: true, items: [], total: 0 },
        },
      },
    });

    render(<ProjectHomePage />, { wrapper: Providers });

    expect(await screen.findByText("Q1 · 传染病传播模型")).toHaveAttribute(
      "href",
      "/projects/00000000-0000-4000-8000-000000000001/models/00000000-0000-4000-8000-000000000003",
    );
    expect(screen.getByText("2 个版本 · succeeded")).toBeInTheDocument();
    expect(
      screen.queryByText("模型、实验与论文尚未接入"),
    ).not.toBeInTheDocument();
  });

  it("renders the real Hermes connection state instead of an Agent placeholder", async () => {
    apiRequest.mockImplementation(async (path: string) => {
      if (path.endsWith("/agent-instances")) {
        return {
          items: [
            {
              agent_instance_id: "instance-1",
              display_name: "Research Hermes",
              grant: { project_access_status: "verified" },
              management_mode: "manual",
              status: "active",
            },
          ],
        };
      }
      return {
        fragments: {
          home: {
            agent: { available: false, items: [], total: 0 },
            article: { available: false, items: [], total: 0 },
            experiments: { available: false, items: [], total: 0 },
            generated_at: "2026-08-06T00:00:00Z",
            milestones: { available: true, items: [], total: 0 },
            models: { available: false, items: [], total: 0 },
            problem: { available: true, items: [], total: 0 },
            project_id: "00000000-0000-4000-8000-000000000001",
            todos: { available: true, items: [], total: 0 },
          },
          project: { problem_title: "Problem", project_constraints: [] },
        },
      };
    });

    render(<ProjectHomePage />, { wrapper: Providers });

    expect(await screen.findByText("Research Hermes")).toBeInTheDocument();
    expect(screen.getByText("verified")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /打开 Agent/ })).toHaveAttribute(
      "href",
      "/projects/00000000-0000-4000-8000-000000000001/agent",
    );
    expect(screen.queryByText("Agent 状态尚未接入")).not.toBeInTheDocument();
  });
});
