import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import RegisterPage from "@/app/register/page";

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
  useSearchParams: () => new URLSearchParams(),
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

describe("registration page", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.request.mockResolvedValue({
      user: {
        display_name: "New Member",
        email: "member@example.com",
        id: "user-1",
      },
    });
  });

  it("submits the registration form and enters the projects page", async () => {
    const queryClient = new QueryClient();
    render(<RegisterPage />, {
      wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
        <QueryClientProvider client={queryClient}>
          {children}
        </QueryClientProvider>
      ),
    });

    fireEvent.change(screen.getByLabelText("显示名称"), {
      target: { value: "New Member" },
    });
    fireEvent.change(screen.getByLabelText("邮箱"), {
      target: { value: "member@example.com" },
    });
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "password-123" },
    });

    const submit = screen.getByRole("button", { name: "注册" });
    expect(submit).toHaveAttribute("type", "submit");
    fireEvent.click(submit);

    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith("/auth/register", {
        body: {
          display_name: "New Member",
          email: "member@example.com",
          invitation_token: undefined,
          password: "password-123",
        },
        method: "POST",
      }),
    );
    expect(mocks.success).toHaveBeenCalledWith("注册成功");
    expect(queryClient.getQueryData(["current-user"])).toEqual({
      displayName: "New Member",
      email: "member@example.com",
      id: "user-1",
    });
    expect(mocks.replace).toHaveBeenCalledWith("/projects");
    expect(mocks.refresh).toHaveBeenCalledOnce();
  });
});
