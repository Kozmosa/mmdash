import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

const navigation = vi.hoisted(() => ({
  replace: vi.fn(),
  search: new URLSearchParams(),
}));

vi.mock("next/navigation", () => ({
  usePathname: () => "/projects/project-1/experiments",
  useRouter: () => ({ replace: navigation.replace }),
  useSearchParams: () => navigation.search,
}));
vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({ id: "project-1", name: "Project" }),
}));
vi.mock("@/components/ui/code-editor", () => ({
  CodeEditor: ({ value }: { value: string }) => (
    <pre data-testid="read-only-editor">{value}</pre>
  ),
}));

import { RepoBrowser } from "@/features/repo-browser/repo-browser";
import { apiClient } from "@/lib/api-client";

const revision = "a".repeat(40);

afterEach(() => {
  navigation.replace.mockReset();
  vi.restoreAllMocks();
});

describe("read-only Repo browser", () => {
  it("pins every object read to the URL commit and exposes no write action", async () => {
    navigation.search = new URLSearchParams({
      path: "data.bin",
      revision,
      workspace: "code",
    });
    const request = vi
      .spyOn(apiClient, "request")
      .mockImplementation(async (path, options) => {
        if (path.endsWith("/repository")) {
          return repositoryFixture() as never;
        }
        if (path.endsWith("/commits")) {
          return commitPageFixture() as never;
        }
        if (path.includes("/commits/")) {
          return commitFixture() as never;
        }
        if (path.endsWith("/tree")) {
          return {
            branch: "main",
            has_more: false,
            items: [
              {
                kind: "file",
                mode: "100644",
                name: "data.bin",
                object_id: "b".repeat(40),
                path: "data.bin",
                size: 42,
              },
            ],
            next_cursor: null,
            path: "",
            resolved_revision: revision,
            workspace: "code",
          } as never;
        }
        if (path.endsWith("/content")) {
          return {
            branch: "main",
            content: null,
            encoding: null,
            kind: "file",
            mode: "100644",
            object_id: "b".repeat(40),
            path: "data.bin",
            preview_status: "binary",
            resolved_revision: revision,
            size: 42,
            workspace: "code",
          } as never;
        }
        throw new Error(`unexpected request ${path} ${options?.method}`);
      });

    render(
      <TestQueryProvider>
        <RepoBrowser />
      </TestQueryProvider>,
    );

    expect(await screen.findByText("Binary 文件不可预览")).toBeInTheDocument();
    expect(screen.getByTitle(revision)).toHaveTextContent(revision);
    const contentCall = request.mock.calls.find(([path]) =>
      path.endsWith("/content"),
    );
    expect(contentCall?.[1]).toEqual(
      expect.objectContaining({
        query: expect.objectContaining({
          path: "data.bin",
          revision,
          workspace: "code",
        }),
        signal: expect.any(AbortSignal),
      }),
    );
    await waitFor(() => {
      expect(
        request.mock.calls.every(([, options]) =>
          [undefined, "GET"].includes(options?.method),
        ),
      ).toBe(true);
    });
    for (const label of ["Save", "Push", "Delete"]) {
      expect(
        screen.queryByRole("button", { name: new RegExp(label, "i") }),
      ).not.toBeInTheDocument();
    }
  });
});

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
    repository_id: "00000000-0000-4000-8000-000000000011",
    settings_version: 2,
    status: "ready",
    updated_at: "2026-07-29T00:00:00Z",
    webhook: {
      hook_id: "00000000-0000-4000-8000-000000000012",
      public_url: "https://mmdash.example/api/webhooks/github/hook",
      secret_configured: true,
    },
    workspaces: [
      {
        head_commit_sha: revision,
        local_branch: "mmdash/code",
        remote_branch: "main",
        status: "ready",
        tree_sha: "c".repeat(40),
        updated_at: "2026-07-29T00:00:00Z",
        workspace: "code",
      },
      {
        head_commit_sha: "d".repeat(40),
        local_branch: "mmdash/article",
        remote_branch: "article",
        status: "ready",
        tree_sha: "e".repeat(40),
        updated_at: "2026-07-29T00:00:00Z",
        workspace: "article",
      },
      {
        head_commit_sha: "f".repeat(40),
        local_branch: "mmdash/result",
        remote_branch: "result",
        status: "ready",
        tree_sha: "1".repeat(40),
        updated_at: "2026-07-29T00:00:00Z",
        workspace: "result",
      },
    ],
  };
}

function commitFixture() {
  return {
    author: {
      email: "author@example.com",
      name: "Author",
      time: "2026-07-29T00:00:00Z",
    },
    changes: [],
    commit_sha: revision,
    committer: {
      email: "author@example.com",
      name: "Author",
      time: "2026-07-29T00:00:00Z",
    },
    first_seen_at: "2026-07-29T00:00:00Z",
    message: "Initial model",
    parent_shas: [],
    repository_id: "00000000-0000-4000-8000-000000000011",
    source: "connect",
    tree_sha: "c".repeat(40),
  };
}

function commitPageFixture() {
  return {
    branch: "main",
    has_more: false,
    items: [commitFixture()],
    next_cursor: null,
    resolved_revision: revision,
    workspace: "code",
  };
}

function TestQueryProvider({ children }: Readonly<{ children: ReactNode }>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
