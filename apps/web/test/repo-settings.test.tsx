import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({ id: "project-1", name: "Project" }),
}));

import { RepoSettingsPanel } from "@/features/repo/repo-settings-panel";
import { ApiError, apiClient } from "@/lib/api-client";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("Repo settings flow", () => {
  it("keeps PAT redacted while saving and testing branch mappings", async () => {
    const request = vi
      .spyOn(apiClient, "request")
      .mockImplementation(async (path, options) => {
        if (path.endsWith("/permissions")) {
          return {
            permissions: ["project.repo.manage", "project.repo.read"],
            role: "owner",
          } as never;
        }
        if (path.includes("/settings/repo.connection")) {
          if (options?.method === "PATCH") {
            return { ...settingFixture(), version: 4 } as never;
          }
          return settingFixture() as never;
        }
        if (path.endsWith("/repository/test")) {
          return {
            branches: ["main", "article", "result"],
            checked_at: "2026-07-29T00:00:00Z",
            checks: [
              { name: "provider", status: "passed" },
              { name: "authentication", status: "passed" },
            ],
            default_branch: "main",
            status: "passed",
          } as never;
        }
        if (path.endsWith("/repository")) {
          throw new ApiError({
            code: "REPOSITORY_NOT_CONFIGURED",
            message: "Not configured",
            status: 404,
          });
        }
        throw new Error(`unexpected request ${path} ${options?.method}`);
      });

    render(
      <TestQueryProvider>
        <RepoSettingsPanel />
      </TestQueryProvider>,
    );

    const token = await screen.findByLabelText(/Fine-grained PAT/);
    expect(token).toHaveValue("");
    await waitFor(() =>
      expect(token).toHaveAttribute("placeholder", "已加密配置；留空保持原值"),
    );
    expect(screen.queryByText("********")).not.toBeInTheDocument();
    const testButton = screen.getByRole("button", { name: "测试连接" });
    await waitFor(() => expect(testButton).toBeEnabled());
    fireEvent.click(testButton);

    expect(await screen.findByText("连接测试：passed")).toBeInTheDocument();
    const patch = request.mock.calls.find(
      ([path, options]) =>
        path.includes("/settings/repo.connection") &&
        options?.method === "PATCH",
    );
    expect(patch?.[1]?.body).toEqual({
      values: expect.objectContaining({
        access_token: "********",
        article_branch: "article",
        code_branch: "main",
        provider: "github",
        remote_url: "https://github.com/acme/model",
        result_branch: "result",
      }),
    });
    expect(request).toHaveBeenCalledWith(
      "/projects/project-1/repository/test",
      { method: "POST" },
    );
  });
});

function settingFixture() {
  return {
    scope: "project",
    scope_id: "project-1",
    type_key: "repo.connection",
    updated_at: "2026-07-29T00:00:00Z",
    updated_by: "user-1",
    values: {
      access_token: "********",
      article_branch: "article",
      code_branch: "main",
      provider: "github",
      remote_url: "https://github.com/acme/model",
      result_branch: "result",
    },
    version: 3,
  };
}

function TestQueryProvider({ children }: Readonly<{ children: ReactNode }>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
