import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  abortToken: vi.fn(),
  checkInstance: vi.fn(),
  createInstance: vi.fn(),
  disableInstance: vi.fn(),
  listInstances: vi.fn(),
  revokeToken: vi.fn(),
  rotateToken: vi.fn(),
  updateInstance: vi.fn(),
  verifyProjectAccess: vi.fn(),
  verifyToken: vi.fn(),
}));

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({ id: "project-1", name: "Project", role: "owner" }),
}));

vi.mock("@/features/agent/agent-api", () => ({
  agentApi: mocks,
  reviewedAgentTools: [
    "project.get",
    "data.list",
    "data.read",
    "context.promote",
  ],
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { AgentSettingsPanel } from "@/features/agent/agent-settings-panel";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("Agent settings", () => {
  it("shows a manual Agent Token once and never backfills provider secrets", async () => {
    mocks.listInstances.mockResolvedValue({ items: [] });
    mocks.createInstance.mockResolvedValue({
      instance: instanceFixture({ management_mode: "manual" }),
      one_time_credential: oneTimeCredential(),
    });

    render(<AgentSettingsPanel />, { wrapper: Providers });
    fireEvent.click(await screen.findByRole("button", { name: "配置 Hermes 连接" }));

    const apiKey = screen.getByLabelText("Hermes API Key");
    expect(apiKey).toHaveAttribute("type", "password");
    expect(apiKey).toHaveValue("");
    fireEvent.change(screen.getByLabelText("Hermes API Server 地址"), {
      target: { value: "https://hermes.example.test" },
    });
    fireEvent.change(apiKey, { target: { value: "hermes-api-key-input" } });
    fireEvent.change(screen.getByLabelText("Hermes 请求超时（秒）"), {
      target: { value: "45" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建 Agent 实例" }));

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText("mmdash_agent_plaintext_once")).toBeInTheDocument();
    expect(mocks.createInstance).toHaveBeenCalledWith(
      "project-1",
      expect.objectContaining({
        allowed_tools: [
          "project.get",
          "data.list",
          "data.read",
          "context.promote",
        ],
        hermes_api_key: "hermes-api-key-input",
        management_mode: "manual",
        request_timeout_seconds: 45,
        runtime_url: "https://hermes.example.test",
      }),
    );
    expect(mocks.createInstance.mock.calls[0]?.[1]).not.toHaveProperty(
      "dashboard_session_token",
    );

    fireEvent.click(screen.getByRole("button", { name: "关闭一次性 Token" }));
    expect(screen.queryByText("mmdash_agent_plaintext_once")).not.toBeInTheDocument();
  });

  it("keeps auto-management credentials and accidental plaintext out of the DOM", async () => {
    const instance = instanceFixture({
      management_mode: "auto",
      management_path: "cloudflare_access",
      management_url: "https://dashboard.example.test",
      secrets: {
        cloudflare_access_configured: true,
        dashboard_session_token_configured: true,
        hermes_api_key_configured: true,
      },
    });
    mocks.listInstances.mockResolvedValue({ items: [instance] });
    mocks.updateInstance.mockResolvedValue({
      instance,
      one_time_credential: {
        ...oneTimeCredential(),
        token: "auto-token-must-not-enter-dom",
      },
    });

    render(<AgentSettingsPanel />, { wrapper: Providers });

    const apiKey = await screen.findByLabelText("Hermes API Key");
    expect(apiKey).toHaveValue("");
    expect(apiKey).toHaveAttribute("placeholder", "已加密配置；留空保持原值");
    expect(await screen.findByLabelText("Dashboard Session Token")).toHaveValue("");
    expect(screen.getByLabelText(/Cloudflare Access Client Secret/)).toHaveValue("");
    expect(screen.queryByText("auto-token-must-not-enter-dom")).not.toBeInTheDocument();
    expect(screen.getAllByText(/cloudflare_access/).length).toBeGreaterThan(0);

    fireEvent.change(screen.getByLabelText("显示名称"), {
      target: { value: "Managed Hermes" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存 Agent 设置" }));

    await waitFor(() => expect(mocks.updateInstance).toHaveBeenCalled());
    const update = mocks.updateInstance.mock.calls[0]?.[2];
    expect(update).not.toHaveProperty("hermes_api_key");
    expect(update).not.toHaveProperty("dashboard_session_token");
    expect(update).not.toHaveProperty("cloudflare_access_client_secret");
    expect(screen.queryByText("auto-token-must-not-enter-dom")).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("disables auto management when the adapter does not declare configure and rotate", async () => {
    mocks.listInstances.mockResolvedValue({
      items: [
        instanceFixture({
          capabilities: {
            ...instanceFixture().capabilities,
            project_access: { configure: false, rotate: false, verify: true },
          },
        }),
      ],
    });

    render(<AgentSettingsPanel />, { wrapper: Providers });

    const auto = await screen.findByRole("radio", { name: /^自动管理/ });
    expect(auto).toBeDisabled();
    expect(
      screen.getByText("此 Adapter 未声明自动管理能力，仅支持手动管理。"),
    ).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /^手动管理/ })).toBeEnabled();
  });
});

function Providers({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
        })
      }
    >
      {children}
    </QueryClientProvider>
  );
}

function instanceFixture(overrides: Record<string, unknown> = {}) {
  return {
    adapter_type: "hermes",
    agent_instance_id: "instance-1",
    capabilities: {
      jobs: true,
      message_history: true,
      project_access: { configure: true, rotate: true, verify: true },
      run_approval: true,
      run_events: true,
      run_status: true,
      run_stop: true,
      runs: true,
      session_chat_stream: true,
      session_fork: true,
      sessions: true,
    },
    created_at: "2026-08-06T00:00:00Z",
    created_by: "user-1",
    credentials: [credentialFixture()],
    display_name: "Hermes",
    grant: {
      agent_instance_id: "instance-1",
      allowed_tools: ["project.get", "data.list", "data.read", "context.promote"],
      created_at: "2026-08-06T00:00:00Z",
      grant_id: "grant-1",
      project_access_status: "verified",
      project_id: "project-1",
      status: "active",
      updated_at: "2026-08-06T00:00:00Z",
      version: 1,
    },
    management_mode: "manual",
    management_path: "direct",
    management_url: "https://dashboard.example.test",
    profile: "default",
    project_id: "project-1",
    project_access_check: {
      checked_at: "2026-08-06T00:00:00Z",
      code: "OK",
      kind: "project_access",
      status: "passed",
    },
    request_timeout_seconds: 30,
    runtime_check: {
      checked_at: "2026-08-06T00:00:00Z",
      code: "OK",
      kind: "runtime",
      status: "passed",
    },
    runtime_url: "https://hermes.example.test",
    secrets: {
      cloudflare_access_configured: false,
      dashboard_session_token_configured: false,
      hermes_api_key_configured: true,
    },
    status: "active",
    updated_at: "2026-08-06T00:00:00Z",
    version: 1,
    ...overrides,
  };
}

function credentialFixture() {
  return {
    agent_instance_id: "instance-1",
    allowed_tools: ["project.get", "data.read"],
    created_at: "2026-08-06T00:00:00Z",
    grant_id: "grant-1",
    id: "token-1",
    name: "Hermes Token",
    project_id: "project-1",
    status: "active",
  };
}

function oneTimeCredential() {
  return {
    credential: { ...credentialFixture(), id: "token-2", status: "pending" },
    mcp_endpoint: "https://mcp.example.test/mcp?mmdash_challenge=one-time",
    server_name: "mmdash",
    token: "mmdash_agent_plaintext_once",
  };
}
