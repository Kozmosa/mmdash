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

import { MemberManagement } from "@/features/members/member-management";

const mocks = vi.hoisted(() => ({
  confirm: vi.fn(),
  error: vi.fn(),
  request: vi.fn(),
  success: vi.fn(),
  writeText: vi.fn(),
}));

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({
    createdBy: "user-owner",
    id: "project-1",
    name: "Modeling Team",
    role: "owner",
  }),
}));

vi.mock("@/components/providers/user-provider", () => ({
  useCurrentUser: () => ({
    displayName: "Project Creator",
    email: "owner@example.com",
    id: "user-owner",
  }),
}));

vi.mock("@/lib/api-client", () => ({
  apiClient: {
    request: mocks.request,
  },
}));

vi.mock("sonner", () => ({
  toast: {
    error: mocks.error,
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

describe("member management", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("confirm", mocks.confirm);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: mocks.writeText },
    });
    mocks.writeText.mockResolvedValue(undefined);
    mocks.request.mockImplementation(
      (path: string, options?: { method?: string }) => {
        if (path.endsWith("/members") && !options?.method) {
          return Promise.resolve({
            items: [
              {
                display_name: "Project Creator",
                email: "owner@example.com",
                joined_at: "2026-07-29T00:00:00Z",
                role: "owner",
                user_id: "user-owner",
              },
              {
                display_name: "Team Member",
                email: "member@example.com",
                joined_at: "2026-07-29T00:00:00Z",
                role: "editor",
                user_id: "user-member",
              },
            ],
          });
        }
        if (path.endsWith("/invitations") && !options?.method) {
          return Promise.resolve({ items: [] });
        }
        if (path.endsWith("/invitations") && options?.method === "POST") {
          return Promise.resolve({ token: "invitation-token" });
        }
        return Promise.resolve(undefined);
      },
    );
  });

  it("submits an invitation and reports success", async () => {
    const { container } = render(<MemberManagement />, { wrapper: Providers });
    fireEvent.change(screen.getByPlaceholderText("member@example.com"), {
      target: { value: "new-member@example.com" },
    });
    const role = container.querySelector<HTMLSelectElement>(
      'select[name="role"]',
    );
    expect(role).not.toBeNull();
    expect(
      screen.queryByRole("option", { name: "agent" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "box" }),
    ).not.toBeInTheDocument();
    fireEvent.change(role!, { target: { value: "viewer" } });

    const submit = screen.getByRole("button", { name: "发送邀请" });
    expect(submit).toHaveAttribute("type", "submit");
    fireEvent.click(submit);

    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith(
        "/projects/project-1/invitations",
        {
          body: { email: "new-member@example.com", role: "viewer" },
          method: "POST",
        },
      ),
    );
    expect(mocks.success).toHaveBeenCalledWith("邀请已创建");

    const invitationLink = await screen.findByLabelText("邀请链接");
    expect(invitationLink).toHaveValue(
      `${location.origin}/invite?token=invitation-token`,
    );
    fireEvent.click(screen.getByRole("button", { name: "复制邀请链接" }));
    await waitFor(() =>
      expect(mocks.writeText).toHaveBeenCalledWith(
        `${location.origin}/invite?token=invitation-token`,
      ),
    );
    expect(mocks.success).toHaveBeenCalledWith("邀请链接已复制");
  });

  it("keeps the owner immutable and transfers ownership explicitly", async () => {
    mocks.confirm.mockReturnValue(true);
    render(<MemberManagement />, { wrapper: Providers });

    await screen.findByText("Project Creator");
    expect(
      screen.queryByLabelText("修改 Project Creator 的角色"),
    ).not.toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "移除" })).toHaveLength(1);

    fireEvent.click(screen.getByRole("button", { name: "转让所有权" }));

    await waitFor(() =>
      expect(mocks.request).toHaveBeenCalledWith(
        "/projects/project-1/members/user-member",
        {
          body: { role: "owner" },
          method: "PUT",
        },
      ),
    );
    expect(mocks.success).toHaveBeenCalledWith("项目所有权已转让");
  });

  it("shows invitation errors instead of failing silently", async () => {
    mocks.request.mockImplementation(
      (path: string, options?: { method?: string }) => {
        if (path.endsWith("/members")) {
          return Promise.resolve({ items: [] });
        }
        if (path.endsWith("/invitations") && options?.method === "POST") {
          return Promise.reject(new Error("该邮箱已有待处理邀请"));
        }
        return Promise.resolve({ items: [] });
      },
    );
    render(<MemberManagement />, { wrapper: Providers });
    fireEvent.change(screen.getByPlaceholderText("member@example.com"), {
      target: { value: "duplicate@example.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: "发送邀请" }));

    await waitFor(() =>
      expect(mocks.error).toHaveBeenCalledWith("该邮箱已有待处理邀请"),
    );
  });
});
