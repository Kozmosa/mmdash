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

import ProjectTrashPage from "@/app/projects/trash/page";

const mocks = vi.hoisted(() => ({
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

describe("project trash page", () => {
  afterEach(cleanup);

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.request.mockImplementation(
      (path: string, options?: { method?: string }) => {
        if (path === "/projects/trash") {
          return Promise.resolve({
            items: [
              {
                deleted_at: "2026-07-29T00:00:00Z",
                id: "00000000-0000-4000-8000-000000000002",
                name: "Recoverable Project",
                problem_summary: "",
                problem_title: "",
                purge_at: "2026-08-28T00:00:00Z",
                role: "owner",
                updated_at: "2026-07-29T00:00:00Z",
              },
            ],
          });
        }
        if (path.endsWith("/restore") && options?.method === "POST") {
          return Promise.resolve({ id: path.split("/")[2] });
        }
        throw new Error(`Unexpected request: ${path}`);
      },
    );
  });

  it("lists recoverable projects and restores one", async () => {
    render(<ProjectTrashPage />, { wrapper: Providers });

    expect(
      await screen.findByRole("heading", { name: "项目回收站" }),
    ).toBeInTheDocument();
    expect(await screen.findByText("Recoverable Project")).toBeInTheDocument();
    expect(screen.getAllByText(/永久删除/)).toHaveLength(2);

    fireEvent.click(screen.getByRole("button", { name: "恢复项目" }));
    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith(
        "/projects/00000000-0000-4000-8000-000000000002/restore",
        { method: "POST" },
      ),
    );
    expect(mocks.success).toHaveBeenCalledWith("项目已恢复");
  });

  it("returns to the project list", async () => {
    render(<ProjectTrashPage />, { wrapper: Providers });

    fireEvent.click(
      await screen.findByRole("button", { name: "返回项目列表" }),
    );
    expect(mocks.push).toHaveBeenCalledWith("/projects");
  });
});
