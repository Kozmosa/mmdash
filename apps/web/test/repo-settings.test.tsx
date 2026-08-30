import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({ id: "project-1", name: "Project" }),
}));

import { RepoSettingsPanel } from "@/features/repo/repo-settings-panel";
import { ApiError, apiClient } from "@/lib/api-client";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("Repo settings flow", () => {
  it("defaults to managed and disables server repositories when deployment support is absent", async () => {
    vi.spyOn(apiClient, "request").mockImplementation(async (path) => {
      if (path.endsWith("/permissions")) {
        return {
          permissions: ["project.repo.manage", "project.repo.read"],
          role: "owner",
        } as never;
      }
      if (path.endsWith("/repository/capabilities")) {
        return {
          providers: [
            { disabled_reason: null, enabled: true, provider: "managed" },
            { disabled_reason: null, enabled: true, provider: "github" },
            {
              disabled_reason:
                "Current deployment has not enabled server repository access",
              enabled: false,
              provider: "server_existing",
            },
          ],
        } as never;
      }
      if (
        path.includes("/settings/repo.connection") ||
        path.endsWith("/repository")
      ) {
        throw new ApiError({
          code: "REPOSITORY_NOT_CONFIGURED",
          message: "Not configured",
          status: 404,
        });
      }
      throw new Error(`unexpected request ${path}`);
    });

    render(
      <TestQueryProvider>
        <RepoSettingsPanel />
      </TestQueryProvider>,
    );

    const provider = await screen.findByLabelText("Provider");
    await waitFor(() => expect(provider).toHaveValue("managed"));
    expect(
      screen.getByText(/不提供外部 Git clone、fetch 或 push 地址/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("option", { name: /服务器已有仓库（当前部署未启用）/ }),
    ).toBeDisabled();
    expect(
      screen.getByText("当前部署未启用服务器仓库接入。"),
    ).toBeInTheDocument();
    expect(
      screen.queryByLabelText("Core 服务容器内的绝对路径"),
    ).not.toBeInTheDocument();
  });

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
        remote_url: "********",
        result_branch: "result",
      }),
    });
    expect(request).toHaveBeenCalledWith(
      "/projects/project-1/repository/test",
      { method: "POST" },
    );
  });

  it("unlocks settings when disconnect has already completed", async () => {
    let disconnectAttempted = false;
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
          return settingFixture() as never;
        }
        if (path.endsWith("/repository/branches")) {
          return { items: [] } as never;
        }
        if (path.endsWith("/repository/commits")) {
          return {
            branch: "main",
            has_more: false,
            items: [],
            next_cursor: null,
            resolved_revision: "a".repeat(40),
            workspace: "code",
          } as never;
        }
        if (path.endsWith("/repository")) {
          if (options?.method === "DELETE") {
            disconnectAttempted = true;
            throw new ApiError({
              code: "REPOSITORY_NOT_CONFIGURED",
              message: "Repository is not configured",
              status: 404,
            });
          }
          if (disconnectAttempted) {
            throw new ApiError({
              code: "REPOSITORY_NOT_CONFIGURED",
              message: "Repository is not configured",
              status: 404,
            });
          }
          return repositoryFixture() as never;
        }
        throw new Error(`unexpected request ${path} ${options?.method}`);
      });
    vi.spyOn(window, "confirm").mockReturnValue(true);

    render(
      <TestQueryProvider>
        <RepoSettingsPanel />
      </TestQueryProvider>,
    );

    const provider = await screen.findByLabelText("Provider");
    await waitFor(() => expect(provider).toHaveValue("github"));
    const remoteUrl = screen.getByLabelText("GitHub HTTPS URL");
    await waitFor(() => expect(provider).toBeDisabled());
    expect(remoteUrl).toBeDisabled();
    expect(screen.getByText(/acme\/model/)).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "查看 Repository" }),
    ).toHaveAttribute("href", "/projects/project-1/repository");

    fireEvent.click(screen.getByRole("button", { name: "断开" }));

    await waitFor(() => expect(provider).toBeEnabled());
    expect(remoteUrl).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "绑定 Repository" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "断开" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/acme\/model ·/)).not.toBeInTheDocument();
    expect(request).toHaveBeenCalledWith("/projects/project-1/repository", {
      method: "DELETE",
    });
  });

  it("restores the same repository while managed cleanup is pending", async () => {
    let restored = false;
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
            checked_at: "2026-07-30T00:00:00Z",
            checks: [
              { name: "provider", status: "passed" },
              { name: "authentication", status: "passed" },
            ],
            default_branch: "main",
            status: "passed",
          } as never;
        }
        if (path.endsWith("/repository")) {
          if (options?.method === "PUT") {
            restored = true;
            return {
              ...repositoryFixture(),
              settings_version: 4,
              status: "pending",
            } as never;
          }
          return {
            ...repositoryFixture(),
            settings_version: restored ? 4 : 3,
            status: restored ? "pending" : "disconnected",
          } as never;
        }
        throw new Error(`unexpected request ${path} ${options?.method}`);
      });

    render(
      <TestQueryProvider>
        <RepoSettingsPanel />
      </TestQueryProvider>,
    );

    const provider = await screen.findByLabelText("Provider");
    await waitFor(() => expect(provider).toHaveValue("github"));
    const remoteUrl = screen.getByLabelText("GitHub HTTPS URL");
    await waitFor(() => {
      expect(provider).toBeEnabled();
      expect(remoteUrl).toHaveValue("https://github.com/acme/model");
      expect(screen.getByLabelText("Code branch")).toHaveValue("main");
      expect(screen.getByLabelText("Article branch")).toHaveValue("article");
      expect(screen.getByLabelText("Result branch")).toHaveValue("result");
    });
    expect(remoteUrl).toBeEnabled();
    expect(screen.getByText("disconnected")).toBeInTheDocument();
    const restore = screen.getByRole("button", { name: "恢复 Repository" });
    expect(restore).toBeEnabled();
    expect(screen.getByText(/可以立即恢复并复用原记录/)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "断开" }),
    ).not.toBeInTheDocument();

    fireEvent.change(remoteUrl, {
      target: { value: "https://github.com/acme/replacement" },
    });
    expect(
      screen.getByRole("button", { name: "立即清理旧绑定并改绑" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "恢复 Repository" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText(/可以立即恢复并复用原记录/),
    ).not.toBeInTheDocument();

    fireEvent.change(remoteUrl, {
      target: { value: "https://github.com/acme/model.git" },
    });
    const normalizedRestore = screen.getByRole("button", {
      name: "恢复 Repository",
    });
    expect(screen.getByText(/可以立即恢复并复用原记录/)).toBeInTheDocument();

    fireEvent.submit(normalizedRestore.closest("form")!);

    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("/projects/project-1/repository", {
        body: { settings_version: 4 },
        method: "PUT",
      }),
    );
    expect(
      request.mock.calls.filter(
        ([path, options]) =>
          path.endsWith("/repository") && options?.method === "PUT",
      ),
    ).toHaveLength(1);
    expect(await screen.findByText("pending")).toBeInTheDocument();
  });

  it("immediately cleans a disconnected binding before binding a different remote", async () => {
    let replaced = false;
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
          const saved = settingFixture();
          return {
            ...saved,
            version: options?.method === "PATCH" ? 5 : 4,
          } as never;
        }
        if (path.endsWith("/repository/test")) {
          return {
            branches: ["main", "article", "result"],
            checked_at: "2026-07-31T00:00:00Z",
            checks: [
              { name: "provider", status: "passed" },
              { name: "authentication", status: "passed" },
            ],
            default_branch: "main",
            status: "passed",
          } as never;
        }
        if (path.endsWith("/repository/branches")) {
          return { items: [] } as never;
        }
        if (path.endsWith("/repository")) {
          if (options?.method === "PUT") {
            replaced = true;
            return {
              ...repositoryFixture(),
              display_name: "acme/replacement",
              remote_url: "https://github.com/acme/replacement",
              settings_version: 5,
              status: "pending",
            } as never;
          }
          if (replaced) {
            return {
              ...repositoryFixture(),
              display_name: "acme/replacement",
              remote_url: "https://github.com/acme/replacement",
              settings_version: 5,
              status: "pending",
            } as never;
          }
          return { ...repositoryFixture(), status: "disconnected" } as never;
        }
        throw new Error(`unexpected request ${path} ${options?.method}`);
      });
    vi.spyOn(window, "confirm").mockReturnValue(true);

    render(
      <TestQueryProvider>
        <RepoSettingsPanel />
      </TestQueryProvider>,
    );

    const remoteUrl = await screen.findByLabelText("GitHub HTTPS URL");
    await waitFor(() => expect(remoteUrl).toHaveValue(""));
    fireEvent.change(remoteUrl, {
      target: { value: "https://github.com/acme/replacement" },
    });
    expect(
      screen.getByRole("button", { name: "立即清理旧绑定并改绑" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "恢复 Repository" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText(/可以立即恢复并复用原记录/),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText(/GitHub 和服务器已有仓库的外部 Git/),
    ).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: "立即清理旧绑定并改绑" }),
    );

    await waitFor(() =>
      expect(request).toHaveBeenCalledWith("/projects/project-1/repository", {
        body: {
          replace_disconnected: true,
          settings_version: 5,
        },
        method: "PUT",
      }),
    );
    expect(window.confirm).toHaveBeenCalledWith(
      expect.stringContaining("外部 GitHub/服务器仓库不会被删除"),
    );
    expect(await screen.findByText("pending")).toBeInTheDocument();
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
      remote_url: "********",
      result_branch: "result",
    },
    version: 3,
  };
}

function repositoryFixture() {
  return {
    created_at: "2026-07-29T00:00:00Z",
    default_branch: "main",
    display_name: "acme/model",
    last_error_code: null,
    last_error_message: null,
    last_synced_at: "2026-07-29T00:00:00Z",
    project_id: "project-1",
    provider: "github",
    remote_url: "https://github.com/acme/model",
    repository_id: "repository-1",
    settings_version: 3,
    status: "ready",
    updated_at: "2026-07-29T00:00:00Z",
    webhook: {
      hook_id: "hook-1",
      public_url: "https://mmdash.example/api/webhooks/github/hook-1",
      secret_configured: true,
    },
    workspaces: [
      {
        head_commit_sha: "a".repeat(40),
        local_branch: "mmdash/code",
        remote_branch: "main",
        status: "ready",
        tree_sha: "b".repeat(40),
        updated_at: "2026-07-29T00:00:00Z",
        workspace: "code",
      },
    ],
  };
}

function TestQueryProvider({ children }: Readonly<{ children: ReactNode }>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
