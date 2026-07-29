import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { UserMenu } from "@/components/user-menu";

const mocks = vi.hoisted(() => ({
  refresh: vi.fn(),
  replace: vi.fn(),
  request: vi.fn(),
  success: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({
    refresh: mocks.refresh,
    replace: mocks.replace,
  }),
}));

vi.mock("@/components/providers/user-provider", () => ({
  useCurrentUser: () => ({
    displayName: "Team Owner",
    email: undefined,
    id: "user-1",
  }),
}));

vi.mock("@/lib/api-client", () => ({
  apiClient: {
    request: mocks.request,
  },
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: mocks.success,
  },
}));

function Providers({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      {children}
    </QueryClientProvider>
  );
}

describe("user menu", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.request.mockResolvedValue(undefined);
  });

  it("opens from the avatar and exposes account, trash, and logout actions", async () => {
    render(<UserMenu />, { wrapper: Providers });

    fireEvent.click(
      screen.getByRole("button", { name: "当前用户：Team Owner" }),
    );

    expect(screen.getByRole("menu", { name: "用户菜单" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "个人中心" })).toHaveAttribute(
      "href",
      "/account",
    );
    expect(
      screen.getByRole("menuitem", { name: "项目回收站" }),
    ).toHaveAttribute("href", "/projects/trash");

    fireEvent.click(screen.getByRole("menuitem", { name: "退出登录" }));

    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith("/auth/logout", {
        method: "POST",
      }),
    );
    expect(mocks.success).toHaveBeenCalledWith("已退出登录");
    expect(mocks.replace).toHaveBeenCalledWith("/login");
    expect(mocks.refresh).toHaveBeenCalledOnce();
  });
});
