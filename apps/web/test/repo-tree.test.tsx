import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RepoTree } from "@/features/repo-browser/repo-tree";
import { apiClient } from "@/lib/api-client";

const revision = "a".repeat(40);

afterEach(() => {
  vi.restoreAllMocks();
});

describe("Repo ARIA tree", () => {
  it("loads one level on expansion and supports keyboard navigation", async () => {
    const onSelect = vi.fn();
    const request = vi
      .spyOn(apiClient, "request")
      .mockImplementation(async (_path, options) => {
        const directory = String(options?.query?.path ?? "");
        return {
          branch: "main",
          has_more: false,
          items:
            directory === "src"
              ? [
                  {
                    kind: "file",
                    mode: "100644",
                    name: "index.ts",
                    object_id: "b".repeat(40),
                    path: "src/index.ts",
                    size: 24,
                  },
                ]
              : [
                  {
                    kind: "directory",
                    mode: "040000",
                    name: "src",
                    object_id: "c".repeat(40),
                    path: "src",
                    size: null,
                  },
                  {
                    kind: "file",
                    mode: "100644",
                    name: "README.md",
                    object_id: "d".repeat(40),
                    path: "README.md",
                    size: 12,
                  },
                ],
          next_cursor: null,
          path: directory,
          resolved_revision: revision,
          workspace: "code",
        } as never;
      });

    render(
      <TestQueryProvider>
        <RepoTree
          onSelect={onSelect}
          projectId="project-1"
          revision={revision}
          selectedPath=""
          workspace="code"
        />
      </TestQueryProvider>,
    );

    const directory = await screen.findByRole("treeitem", { name: /src/ });
    expect(directory).toHaveAttribute("aria-expanded", "false");
    fireEvent.keyDown(directory, { key: "ArrowRight" });
    expect(directory).toHaveAttribute("aria-expanded", "true");
    const child = await screen.findByRole("treeitem", { name: /index\.ts/ });
    expect(request).toHaveBeenCalledWith(
      "/projects/project-1/repository/tree",
      expect.objectContaining({
        query: expect.objectContaining({
          path: "src",
          revision,
          workspace: "code",
        }),
        signal: expect.any(AbortSignal),
      }),
    );

    directory.focus();
    fireEvent.keyDown(directory, { key: "ArrowDown" });
    expect(child).toHaveFocus();
    fireEvent.click(child);
    expect(onSelect).toHaveBeenCalledWith("src/index.ts");

    fireEvent.keyDown(child, { key: "ArrowLeft" });
    await waitFor(() => expect(directory).toHaveFocus());
  });
});

function TestQueryProvider({ children }: Readonly<{ children: ReactNode }>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
