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
  afterEach(cleanup);

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
});
