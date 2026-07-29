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

import InvitePage from "@/app/invite/page";

const mocks = vi.hoisted(() => ({
  currentUser: {
    displayName: "Invited Member",
    email: "member@example.com",
    id: "user-member",
  } as null | { displayName: string; email: string; id: string },
  replace: vi.fn(),
  request: vi.fn(),
  search: "token=invitation-token",
  success: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: mocks.replace }),
  useSearchParams: () => new URLSearchParams(mocks.search),
}));

vi.mock("@/components/providers/user-provider", () => ({
  useCurrentUser: () => mocks.currentUser,
  useCurrentUserLoading: () => false,
}));

vi.mock("@/lib/api-client", () => ({
  apiClient: { request: mocks.request },
}));

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

describe("invitation page actions", () => {
  afterEach(cleanup);

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.currentUser = {
      displayName: "Invited Member",
      email: "member@example.com",
      id: "user-member",
    };
    mocks.search = "token=invitation-token";
    mocks.request.mockImplementation(
      (path: string, options?: { method?: string }) => {
        if (path === "/auth/invitations/preview") {
          return Promise.resolve({
            email: "member@example.com",
            project_name: "Modeling Team",
            role: "viewer",
          });
        }
        if (
          path === "/auth/invitations/accept" ||
          path === "/auth/invitations/reject"
        ) {
          return Promise.resolve(undefined);
        }
        throw new Error(`Unexpected request: ${path} ${options?.method ?? ""}`);
      },
    );
  });

  it("shows accept and reject actions to a signed-in user", async () => {
    render(<InvitePage />, { wrapper: Providers });

    expect(
      await screen.findByRole("button", { name: "接收邀请" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "拒绝" })).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "登录并接收" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "注册并接受" }),
    ).not.toBeInTheDocument();
  });

  it("shows login, registration, and reject actions to a guest", async () => {
    mocks.currentUser = null;
    render(<InvitePage />, { wrapper: Providers });

    const login = await screen.findByRole("link", { name: "登录并接收" });
    expect(login).toHaveAttribute(
      "href",
      expect.stringContaining("/login?returnTo="),
    );
    expect(screen.getByRole("link", { name: "注册并接受" })).toHaveAttribute(
      "href",
      "/register?token=invitation-token",
    );
    expect(screen.getByRole("button", { name: "拒绝" })).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "接收邀请" }),
    ).not.toBeInTheDocument();
  });

  it("blocks acceptance when the signed-in email does not match the invitation", async () => {
    mocks.currentUser = {
      displayName: "Different Member",
      email: "different@example.com",
      id: "user-different",
    };
    mocks.search = "token=invitation-token&autoAccept=1";
    render(<InvitePage />, { wrapper: Providers });

    const mismatch = await screen.findByText(
      /请使用受邀邮箱对应的账号登录后再接受邀请/,
    );
    expect(mismatch).toHaveTextContent("different@example.com");
    expect(mismatch).toHaveTextContent("member@example.com");
    expect(
      screen.queryByRole("button", { name: "接收邀请" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "拒绝" })).toBeInTheDocument();

    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith(
        "/auth/invitations/preview",
        expect.anything(),
      ),
    );
    expect(mocks.request).not.toHaveBeenCalledWith(
      "/auth/invitations/accept",
      expect.anything(),
    );
  });

  it("matches invitation emails without case sensitivity", async () => {
    mocks.currentUser = {
      displayName: "Invited Member",
      email: "MEMBER@EXAMPLE.COM",
      id: "user-member",
    };
    render(<InvitePage />, { wrapper: Providers });

    expect(
      await screen.findByRole("button", { name: "接收邀请" }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/与受邀邮箱.*不一致/)).not.toBeInTheDocument();
  });

  it("permanently rejects an invitation", async () => {
    render(<InvitePage />, { wrapper: Providers });
    fireEvent.click(await screen.findByRole("button", { name: "拒绝" }));

    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith("/auth/invitations/reject", {
        body: { token: "invitation-token" },
        method: "POST",
      }),
    );
    expect(await screen.findByText("你已拒绝该项目邀请。")).toBeInTheDocument();
    expect(mocks.success).toHaveBeenCalledWith("已拒绝邀请");
  });

  it("accepts automatically after the login-and-accept flow returns", async () => {
    mocks.search = "token=invitation-token&autoAccept=1";
    render(<InvitePage />, { wrapper: Providers });

    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith("/auth/invitations/accept", {
        body: { token: "invitation-token" },
        method: "POST",
      }),
    );
    expect(mocks.replace).toHaveBeenCalledWith("/projects");
  });
});
