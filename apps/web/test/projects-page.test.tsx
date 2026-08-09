import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import ProjectsPage from "@/app/projects/page";

const mocks = vi.hoisted(() => ({
  confirm: vi.fn(),
  push: vi.fn(),
  replace: vi.fn(),
  request: vi.fn(),
  success: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: mocks.push, replace: mocks.replace }),
}));

vi.mock("@/components/user-menu", () => ({
  UserMenu: () => <div>用户菜单</div>,
}));

vi.mock("@/lib/api-client", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api-client")>();
  return {
    ...original,
    apiClient: { request: mocks.request },
  };
});

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: mocks.success,
  },
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

describe("projects recycle bin", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("confirm", mocks.confirm);
    mocks.confirm.mockReturnValue(true);
    mocks.request.mockImplementation(
      (path: string, options?: { method?: string }) => {
        if (path === "/projects" && !options?.method) {
          return Promise.resolve({
            items: [
              {
                id: "00000000-0000-4000-8000-000000000001",
                name: "Active Project",
                problem_summary: "",
                problem_title: "",
                role: "owner",
                updated_at: "2026-07-29T00:00:00Z",
              },
            ],
          });
        }
        if (path === "/inbox/unread-count" && !options?.method) {
          return Promise.resolve({ count: 3 });
        }
        if (options?.method === "DELETE") {
          return Promise.resolve(undefined);
        }
        throw new Error(`Unexpected request: ${path}`);
      },
    );
  });

  it("shows the global Inbox icon and unread badge", async () => {
    render(<ProjectsPage />, { wrapper: Providers });

    const inbox = await screen.findByRole("link", {
      name: "收件箱，3 条未读消息",
    });
    expect(inbox).toHaveAttribute("href", "/inbox");
    expect(inbox).toHaveTextContent("3");
  });

  it("routes a newly created Project to Artifact source setup", async () => {
    const projectId = "00000000-0000-4000-8000-000000000007";
    mocks.request.mockImplementation(
      (path: string, options?: { method?: string }) => {
        if (path === "/projects" && !options?.method) {
          return Promise.resolve({ items: [] });
        }
        if (path === "/projects" && options?.method === "POST") {
          return Promise.resolve({ id: projectId });
        }
        throw new Error(`Unexpected request: ${path}`);
      },
    );
    render(<ProjectsPage />, { wrapper: Providers });

    fireEvent.click(await screen.findByRole("button", { name: "创建项目" }));
    fireEvent.change(screen.getByLabelText("项目名称"), {
      target: { value: "Artifact Project" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认创建" }));

    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith("/projects", {
        body: {
          name: "Artifact Project",
          problem_summary: "",
          problem_title: "",
        },
        method: "POST",
      }),
    );
    expect(mocks.push).toHaveBeenCalledWith(
      `/projects/${projectId}/artifacts?setup=1`,
    );
  });

  it("moves an owned project to the recycle bin", async () => {
    render(<ProjectsPage />, { wrapper: Providers });

    fireEvent.click(
      await screen.findByRole("button", {
        name: "将 Active Project 移入回收站",
      }),
    );

    expect(mocks.confirm).toHaveBeenCalledWith(
      "确定将“Active Project”移入回收站吗？30 天内可以恢复。",
    );
    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith(
        "/projects/00000000-0000-4000-8000-000000000001",
        { method: "DELETE" },
      ),
    );
    expect(mocks.success).toHaveBeenCalledWith(
      "项目已移入回收站，可在 30 天内恢复",
    );
  });
});
