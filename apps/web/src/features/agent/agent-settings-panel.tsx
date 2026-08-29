"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bot,
  CheckCircle2,
  Copy,
  ExternalLink,
  KeyRound,
  Plus,
  RefreshCw,
  ShieldCheck,
  Trash2,
  X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { useCurrentProject } from "@/components/providers/project-provider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";

import { agentApi, reviewedAgentTools } from "./agent-api";
import type {
  AgentInstance,
  AgentInstanceInput,
  AgentInstanceProvisioningResult,
  AgentManagementMode,
  OneTimeAgentToken,
} from "./types";

type AgentForm = {
  allowedTools: string[];
  cloudflareClientId: string;
  cloudflareClientSecret: string;
  dashboardSessionToken: string;
  displayName: string;
  hermesApiKey: string;
  managementMode: AgentManagementMode;
  managementUrl: string;
  profile: string;
  requestTimeoutSeconds: string;
  runtimeUrl: string;
};

const emptyForm: AgentForm = {
  allowedTools: [...reviewedAgentTools],
  cloudflareClientId: "",
  cloudflareClientSecret: "",
  dashboardSessionToken: "",
  displayName: "Hermes",
  hermesApiKey: "",
  managementMode: "manual",
  managementUrl: "",
  profile: "default",
  requestTimeoutSeconds: "30",
  runtimeUrl: "",
};

export function AgentSettingsPanel() {
  const project = useCurrentProject();
  const queryClient = useQueryClient();
  const canManage = project.role === "owner" || project.role === "maintainer";
  const instances = useQuery({
    queryFn: () => agentApi.listInstances(project.id),
    queryKey: ["agent-instances", project.id],
  });
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState<AgentForm>(emptyForm);
  const [oneTimeToken, setOneTimeToken] = useState<OneTimeAgentToken | null>(
    null,
  );
  const selected = useMemo(
    () =>
      instances.data?.items.find(
        (instance) => instance.agent_instance_id === selectedId,
      ) ?? null,
    [instances.data?.items, selectedId],
  );

  useEffect(() => {
    if (creating || selectedId || !instances.data?.items.length) {
      return;
    }
    const first =
      instances.data.items.find((instance) => instance.status !== "disabled") ??
      instances.data.items[0];
    setSelectedId(first?.agent_instance_id ?? null);
  }, [creating, instances.data?.items, selectedId]);

  useEffect(() => {
    if (!selected || creating) {
      return;
    }
    setForm(formFromInstance(selected));
  }, [creating, selected]);

  const refreshInstances = async () => {
    await queryClient.invalidateQueries({
      queryKey: ["agent-instances", project.id],
    });
  };

  const save = useMutation({
    mutationFn: async () => {
      if (!canManage) {
        throw new Error("当前角色不能管理 Agent 连接");
      }
      if (form.allowedTools.length === 0) {
        throw new Error("至少选择一个项目 Tool");
      }
      parseRequestTimeout(form.requestTimeoutSeconds);
      if (!selected || creating) {
        return agentApi.createInstance(project.id, createInput(form));
      }
      return agentApi.updateInstance(
        project.id,
        selected.agent_instance_id,
        updateInput(form),
      );
    },
    onError: showError("Agent 连接保存失败"),
    onSuccess: async (result) => {
      const normalized = normalizeProvisioningResult(result);
      setSelectedId(normalized.instance.agent_instance_id);
      setCreating(false);
      setForm((current) => clearSecrets(current));
      if (
        normalized.instance.management_mode === "manual" &&
        normalized.one_time_credential
      ) {
        setOneTimeToken(normalized.one_time_credential);
      }
      await refreshInstances();
      toast.success("Agent 连接已保存");
    },
  });

  const checks = useMutation({
    mutationFn: () =>
      agentApi.checkInstance(project.id, selected!.agent_instance_id),
    onError: showError("连接检查失败"),
    onSuccess: async (result) => {
      await refreshInstances();
      toast.success(`连接检查：${result.status}`);
    },
  });

  const verifyAccess = useMutation({
    mutationFn: () =>
      agentApi.verifyProjectAccess(project.id, selected!.agent_instance_id),
    onError: showError("MCP 反向连接验证失败"),
    onSuccess: async (result) => {
      await refreshInstances();
      toast.success(result.verified ? "MCP 访问已验证" : "MCP 访问尚未验证");
    },
  });

  const rotate = useMutation({
    mutationFn: () =>
      agentApi.rotateToken(project.id, selected!.agent_instance_id),
    onError: showError("Agent Token 轮换失败；旧 Token 保持有效"),
    onSuccess: async (result) => {
      if (
        selected?.management_mode === "manual" &&
        result.one_time_credential
      ) {
        setOneTimeToken(result.one_time_credential);
      }
      await refreshInstances();
      toast.success(
        result.rotation_status === "completed"
          ? "Agent Token 已安全轮换"
          : "新 Token 已签发；旧 Token 仍保持有效",
      );
    },
  });

  const disable = useMutation({
    mutationFn: () =>
      agentApi.disableInstance(project.id, selected!.agent_instance_id),
    onError: showError("Agent 实例删除失败"),
    onSuccess: async () => {
      setSelectedId(null);
      setCreating(false);
      setForm(emptyForm);
      await refreshInstances();
      toast.success("Agent 实例已从工作区删除，关联 Token 已撤销");
    },
  });

  const pending = selected?.credentials.find(
    (credential) => credential.status === "pending",
  );

  return (
    <section
      aria-labelledby="agent-settings-title"
      className="space-y-4"
      id="agent-settings"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2
            className="flex items-center gap-2 text-lg font-semibold"
            id="agent-settings-title"
          >
            <Bot aria-hidden="true" className="size-5" />
            Hermes Agent
          </h2>
          <p className="text-sm text-muted-foreground">
            Hermes API Key 用于 mmdash 调用运行时；Agent Token 用于 Hermes 直连
            MCP Gateway。两者方向和权限不同。
          </p>
        </div>
        {canManage ? (
          <Button
            onClick={() => {
              setCreating(true);
              setSelectedId(null);
              setForm(emptyForm);
            }}
            size="sm"
            variant="outline"
          >
            <Plus aria-hidden="true" className="size-4" />
            新增实例
          </Button>
        ) : null}
      </div>

      {instances.isLoading ? (
        <p className="text-sm text-muted-foreground">正在读取 Agent 配置…</p>
      ) : null}
      {instances.isError ? (
        <p className="text-sm text-destructive">Agent 配置读取失败。</p>
      ) : null}

      {instances.data?.items.length ? (
        <div className="flex flex-wrap gap-2" aria-label="Agent 实例">
          {instances.data.items.map((instance) => (
            <Button
              aria-pressed={selectedId === instance.agent_instance_id}
              key={instance.agent_instance_id}
              onClick={() => {
                setCreating(false);
                setSelectedId(instance.agent_instance_id);
              }}
              size="sm"
              variant={
                selectedId === instance.agent_instance_id
                  ? "secondary"
                  : "outline"
              }
            >
              {instance.display_name}
              <Badge>{instance.status}</Badge>
            </Button>
          ))}
        </div>
      ) : null}

      {!instances.isLoading && !instances.data?.items.length && !creating ? (
        <Card className="border-dashed">
          <CardContent className="py-8 text-center">
            <Bot
              aria-hidden="true"
              className="mx-auto size-8 text-muted-foreground"
            />
            <p className="mt-3 font-medium">尚未配置 Hermes 连接</p>
            <p className="mt-1 text-sm text-muted-foreground">
              先保存运行时连接，再按 manual 或 auto 模式完成 MCP 绑定。
            </p>
            {canManage ? (
              <Button
                className="mt-4"
                onClick={() => setCreating(true)}
                size="sm"
              >
                配置 Hermes 连接
              </Button>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {creating || selected ? (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1.3fr)_minmax(300px,0.7fr)]">
          <AgentConnectionForm
            canManage={canManage}
            form={form}
            instance={selected}
            onChange={setForm}
            onSubmit={() => save.mutate()}
            saving={save.isPending}
          />
          <AgentConnectionStatus
            canManage={canManage}
            checking={checks.isPending}
            disabling={disable.isPending}
            instance={selected}
            onCheck={() => checks.mutate()}
            onDisable={() => {
              if (
                selected &&
                window.confirm(
                  `删除 ${selected.display_name}？所有 Agent Token 将立即撤销；历史会话仅按审计和留存策略保留。`,
                )
              ) {
                disable.mutate();
              }
            }}
            onRotate={() => rotate.mutate()}
            onVerifyAccess={() => verifyAccess.mutate()}
            pending={pending}
            projectId={project.id}
            refreshing={rotate.isPending || verifyAccess.isPending}
            refreshInstances={refreshInstances}
          />
        </div>
      ) : null}

      {oneTimeToken ? (
        <OneTimeTokenDialog
          credential={oneTimeToken}
          onClose={() => setOneTimeToken(null)}
        />
      ) : null}
    </section>
  );
}

function AgentConnectionForm({
  canManage,
  form,
  instance,
  onChange,
  onSubmit,
  saving,
}: {
  canManage: boolean;
  form: AgentForm;
  instance: AgentInstance | null;
  onChange: (form: AgentForm) => void;
  onSubmit: () => void;
  saving: boolean;
}) {
  const set = <Key extends keyof AgentForm>(key: Key, value: AgentForm[Key]) =>
    onChange({ ...form, [key]: value });
  const secretPlaceholder = (configured: boolean, label: string) =>
    configured ? "已加密配置；留空保持原值" : `输入${label}`;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">
          {instance ? "编辑连接" : "配置 Hermes 连接"}
        </CardTitle>
        <CardDescription>
          Secret 保存后立即清空，页面和 API 响应都不会回填明文。
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="grid gap-4 md:grid-cols-2"
          onSubmit={(event) => {
            event.preventDefault();
            onSubmit();
          }}
        >
          <Field label="显示名称">
            <Input
              disabled={!canManage}
              onChange={(event) => set("displayName", event.target.value)}
              required
              value={form.displayName}
            />
          </Field>
          <Field label="Profile / Agent 标识">
            <Input
              disabled={!canManage}
              onChange={(event) => set("profile", event.target.value)}
              value={form.profile}
            />
          </Field>
          <Field className="md:col-span-2" label="Hermes API Server 地址">
            <Input
              disabled={!canManage}
              onChange={(event) => set("runtimeUrl", event.target.value)}
              placeholder="https://hermes.example.com"
              required
              type="url"
              value={form.runtimeUrl}
            />
          </Field>
          <Field label="Hermes 请求超时（秒）">
            <Input
              disabled={!canManage}
              max={300}
              min={1}
              onChange={(event) =>
                set("requestTimeoutSeconds", event.target.value)
              }
              required
              step={1}
              type="number"
              value={form.requestTimeoutSeconds}
            />
          </Field>
          <Field className="md:col-span-2" label="Hermes API Key">
            <Input
              autoComplete="new-password"
              disabled={!canManage}
              onChange={(event) => set("hermesApiKey", event.target.value)}
              placeholder={secretPlaceholder(
                Boolean(instance?.secrets.hermes_api_key_configured),
                "Hermes API Key",
              )}
              required={!instance}
              type="password"
              value={form.hermesApiKey}
            />
          </Field>
          <fieldset className="space-y-2 md:col-span-2">
            <legend className="text-sm font-medium">MCP 管理模式</legend>
            <div className="grid gap-2 sm:grid-cols-2">
              {(["manual", "auto"] as const).map((mode) => {
                const projectAccess = instance?.capabilities.project_access;
                const autoSupported =
                  !instance ||
                  Boolean(projectAccess?.configure && projectAccess?.rotate);
                return (
                  <label
                    className="flex cursor-pointer gap-3 rounded-md border border-border p-3 text-sm"
                    key={mode}
                  >
                    <input
                      checked={form.managementMode === mode}
                      disabled={
                        !canManage || (mode === "auto" && !autoSupported)
                      }
                      name="management-mode"
                      onChange={() => set("managementMode", mode)}
                      type="radio"
                    />
                    <span>
                      <strong>
                        {mode === "manual" ? "手动管理" : "自动管理"}
                      </strong>
                      <span className="mt-1 block text-xs text-muted-foreground">
                        {mode === "manual"
                          ? "Token 只显示一次，由用户写入 Hermes。"
                          : autoSupported
                            ? "服务端通过受控 Dashboard 连接配置和轮换。"
                            : "此 Adapter 未声明自动管理能力，仅支持手动管理。"}
                      </span>
                    </span>
                  </label>
                );
              })}
            </div>
          </fieldset>
          <Field
            className="md:col-span-2"
            label={
              form.managementMode === "manual"
                ? "Hermes Dashboard 地址（可选跳转）"
                : "Hermes Dashboard 管理地址"
            }
          >
            <Input
              disabled={!canManage}
              onChange={(event) => set("managementUrl", event.target.value)}
              required={form.managementMode === "auto"}
              type="url"
              value={form.managementUrl}
            />
          </Field>
          {form.managementMode === "auto" ? (
            <>
              <Field className="md:col-span-2" label="Dashboard Session Token">
                <Input
                  autoComplete="new-password"
                  disabled={!canManage}
                  onChange={(event) =>
                    set("dashboardSessionToken", event.target.value)
                  }
                  placeholder={secretPlaceholder(
                    Boolean(
                      instance?.secrets.dashboard_session_token_configured,
                    ),
                    "Dashboard Session Token",
                  )}
                  required={!instance}
                  type="password"
                  value={form.dashboardSessionToken}
                />
              </Field>
              <Field label="Cloudflare Access Client ID（按需）">
                <Input
                  autoComplete="new-password"
                  disabled={!canManage}
                  onChange={(event) =>
                    set("cloudflareClientId", event.target.value)
                  }
                  placeholder={secretPlaceholder(
                    Boolean(instance?.secrets.cloudflare_access_configured),
                    "Client ID",
                  )}
                  type="password"
                  value={form.cloudflareClientId}
                />
              </Field>
              <Field label="Cloudflare Access Client Secret（按需）">
                <Input
                  autoComplete="new-password"
                  disabled={!canManage}
                  onChange={(event) =>
                    set("cloudflareClientSecret", event.target.value)
                  }
                  placeholder={secretPlaceholder(
                    Boolean(instance?.secrets.cloudflare_access_configured),
                    "Client Secret",
                  )}
                  type="password"
                  value={form.cloudflareClientSecret}
                />
              </Field>
            </>
          ) : null}
          <fieldset className="space-y-2 md:col-span-2">
            <legend className="text-sm font-medium">允许的 MCP Tools</legend>
            <p className="text-xs text-muted-foreground">
              授权绑定当前 Agent 与 Project，不接受通配符。修改范围会安全轮换
              Token。
            </p>
            <div className="grid gap-2 sm:grid-cols-2">
              {reviewedAgentTools.map((tool) => (
                <label className="flex items-center gap-2 text-sm" key={tool}>
                  <input
                    checked={form.allowedTools.includes(tool)}
                    disabled={!canManage}
                    onChange={(event) =>
                      set(
                        "allowedTools",
                        event.target.checked
                          ? [...form.allowedTools, tool]
                          : form.allowedTools.filter((item) => item !== tool),
                      )
                    }
                    type="checkbox"
                  />
                  <code>{tool}</code>
                </label>
              ))}
            </div>
          </fieldset>
          {canManage ? (
            <div className="md:col-span-2">
              <Button disabled={saving} type="submit">
                {saving
                  ? "正在保存…"
                  : instance
                    ? "保存 Agent 设置"
                    : "创建 Agent 实例"}
              </Button>
            </div>
          ) : null}
        </form>
      </CardContent>
    </Card>
  );
}

function AgentConnectionStatus({
  canManage,
  checking,
  disabling,
  instance,
  onCheck,
  onDisable,
  onRotate,
  onVerifyAccess,
  pending,
  projectId,
  refreshing,
  refreshInstances,
}: {
  canManage: boolean;
  checking: boolean;
  disabling: boolean;
  instance: AgentInstance | null;
  onCheck: () => void;
  onDisable: () => void;
  onRotate: () => void;
  onVerifyAccess: () => void;
  pending: AgentInstance["credentials"][number] | undefined;
  projectId: string;
  refreshing: boolean;
  refreshInstances: () => Promise<void>;
}) {
  if (!instance) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-base">绑定流程</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm text-muted-foreground">
          <p>1. 验证 Hermes 运行时健康、认证与能力。</p>
          <p>2. 建立 Project Grant 并签发项目受限 Agent Token。</p>
          <p>3. 独立验证 Hermes → MCP Gateway 反向连接。</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center justify-between gap-2 text-base">
          连接状态 <Badge>{instance.status}</Badge>
        </CardTitle>
        <CardDescription>
          {instance.management_mode} · {instance.management_path} · MCP{" "}
          {instance.grant.project_access_status ?? "pending"}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <StatusLine
          configured={instance.secrets.hermes_api_key_configured}
          label="Hermes API Key"
        />
        <StatusLine
          configured={instance.runtime_check?.status === "passed"}
          label="运行时连接"
        />
        <StatusLine
          configured={instance.project_access_check?.status === "passed"}
          label="Hermes → MCP Gateway"
        />
        {instance.management_mode === "auto" ? (
          <>
            <StatusLine
              configured={instance.management_check?.status === "passed"}
              label={`自动管理（${instance.management_path}）`}
            />
            <p className="rounded-md bg-muted p-3 text-xs text-muted-foreground">
              管理地址由服务端按
              SSRF、DNS、redirect、超时与私网策略探测；浏览器不直接测试该地址。
            </p>
          </>
        ) : instance.management_url ? (
          <a
            className="inline-flex items-center gap-1 text-sm text-primary hover:underline"
            href={instance.management_url}
            rel="noreferrer"
            target="_blank"
          >
            打开 Hermes MCP 配置
            <ExternalLink aria-hidden="true" className="size-3" />
          </a>
        ) : null}

        <div className="flex flex-wrap gap-2">
          <Button
            disabled={checking}
            onClick={onCheck}
            size="sm"
            variant="outline"
          >
            <RefreshCw aria-hidden="true" className="size-3" />
            全量检查
          </Button>
          <Button
            disabled={refreshing}
            onClick={onVerifyAccess}
            size="sm"
            variant="outline"
          >
            <ShieldCheck aria-hidden="true" className="size-3" />
            验证 MCP
          </Button>
          {canManage ? (
            <Button
              disabled={refreshing || Boolean(pending)}
              onClick={onRotate}
              size="sm"
              variant="outline"
            >
              <KeyRound aria-hidden="true" className="size-3" />
              轮换 Token
            </Button>
          ) : null}
        </div>

        <div className="space-y-2 border-t border-border pt-4">
          <h4 className="text-sm font-medium">Agent Tokens</h4>
          {instance.credentials.map((credential) => (
            <CredentialRow
              canManage={canManage}
              credential={credential}
              instance={instance}
              key={credential.id}
              projectId={projectId}
              refreshInstances={refreshInstances}
            />
          ))}
        </div>

        {canManage ? (
          <Button
            className="text-destructive"
            disabled={disabling}
            onClick={onDisable}
            size="sm"
            variant="ghost"
          >
            <Trash2 aria-hidden="true" className="size-3" />
            删除实例并撤销 Token
          </Button>
        ) : null}
      </CardContent>
    </Card>
  );
}

function CredentialRow({
  canManage,
  credential,
  instance,
  projectId,
  refreshInstances,
}: {
  canManage: boolean;
  credential: AgentInstance["credentials"][number];
  instance: AgentInstance;
  projectId: string;
  refreshInstances: () => Promise<void>;
}) {
  const verify = useMutation({
    mutationFn: () =>
      agentApi.verifyToken(
        projectId,
        instance.agent_instance_id,
        credential.id,
      ),
    onError: showError("新 Token 验证失败；旧 Token 保持有效"),
    onSuccess: async () => {
      await refreshInstances();
      toast.success("新 Token 已激活，旧 Token 已安全撤销");
    },
  });
  const abort = useMutation({
    mutationFn: () =>
      agentApi.abortToken(projectId, instance.agent_instance_id, credential.id),
    onError: showError("轮换中止失败"),
    onSuccess: async () => {
      await refreshInstances();
      toast.success("待生效 Token 已撤销，旧 Token 保持有效");
    },
  });
  const revoke = useMutation({
    mutationFn: () =>
      agentApi.revokeToken(
        projectId,
        instance.agent_instance_id,
        credential.id,
      ),
    onError: showError("Token 撤销失败"),
    onSuccess: async () => {
      await refreshInstances();
      toast.success("Agent Token 已撤销");
    },
  });

  return (
    <div className="rounded-md border border-border p-3 text-xs">
      <div className="flex items-center justify-between gap-2">
        <span className="truncate font-medium">{credential.name}</span>
        <Badge>{credential.status}</Badge>
      </div>
      <p className="mt-1 text-muted-foreground">
        {credential.allowed_tools.join(" · ")}
      </p>
      {canManage && credential.status === "pending" ? (
        <div className="mt-2 flex gap-2">
          <Button
            disabled={verify.isPending}
            onClick={() => verify.mutate()}
            size="sm"
            variant="outline"
          >
            验证并激活
          </Button>
          <Button
            disabled={abort.isPending}
            onClick={() => abort.mutate()}
            size="sm"
            variant="ghost"
          >
            中止轮换
          </Button>
        </div>
      ) : null}
      {canManage && credential.status === "active" ? (
        <Button
          className="mt-2 text-destructive"
          disabled={revoke.isPending}
          onClick={() => {
            if (
              window.confirm("立即撤销此 Agent Token？Hermes 访问会被阻断。")
            ) {
              revoke.mutate();
            }
          }}
          size="sm"
          variant="ghost"
        >
          立即撤销
        </Button>
      ) : null}
    </div>
  );
}

function OneTimeTokenDialog({
  credential,
  onClose,
}: {
  credential: OneTimeAgentToken;
  onClose: () => void;
}) {
  const copy = async (value: string, label: string) => {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(`${label}已复制`);
    } catch {
      toast.error("复制失败，请手工复制后立即安全保存");
    }
  };

  return (
    <div
      aria-labelledby="one-time-token-title"
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
    >
      <Card className="w-full max-w-2xl shadow-xl">
        <CardHeader>
          <div className="flex items-start justify-between gap-3">
            <div>
              <CardTitle id="one-time-token-title">
                一次性 Hermes MCP 凭据
              </CardTitle>
              <CardDescription className="mt-2">
                Agent Token 和带单次验证 challenge 的 MCP
                地址只显示一次。关闭或刷新后无法再次读取；请把两者写入
                Hermes，而不是 CLI。
              </CardDescription>
            </div>
            <Button
              aria-label="关闭一次性 Token"
              onClick={onClose}
              size="icon"
              variant="ghost"
            >
              <X aria-hidden="true" className="size-4" />
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          <OneTimeValue
            label="一次性 MCP 地址"
            onCopy={() => copy(credential.mcp_endpoint, "MCP 地址")}
            secret
            value={credential.mcp_endpoint}
          />
          <OneTimeValue
            label="Server 名称"
            onCopy={() =>
              copy(credential.server_name ?? "mmdash", "Server 名称")
            }
            value={credential.server_name ?? "mmdash"}
          />
          <OneTimeValue
            label="Agent Token"
            onCopy={() => copy(credential.token, "Agent Token")}
            secret
            value={credential.token}
          />
          <p className="rounded-md border border-amber-500/30 bg-amber-500/10 p-3 text-xs">
            完成 Hermes 配置后返回并点击“验证 MCP”。轮换期间，在验证新 Token
            前旧 Token 会保持有效。
          </p>
          <Button onClick={onClose}>我已安全保存</Button>
        </CardContent>
      </Card>
    </div>
  );
}

function OneTimeValue({
  label,
  onCopy,
  secret,
  value,
}: {
  label: string;
  onCopy: () => void;
  secret?: boolean;
  value: string;
}) {
  return (
    <div>
      <p className="mb-1 text-xs font-medium">{label}</p>
      <div className="flex gap-2">
        <code
          className="min-w-0 flex-1 overflow-x-auto rounded-md bg-muted p-3 text-xs"
          data-one-time-secret={secret ? "true" : undefined}
        >
          {value}
        </code>
        <Button
          aria-label={`复制${label}`}
          onClick={onCopy}
          size="icon"
          variant="outline"
        >
          <Copy aria-hidden="true" className="size-4" />
        </Button>
      </div>
    </div>
  );
}

function Field({
  children,
  className,
  label,
}: {
  children: React.ReactNode;
  className?: string;
  label: string;
}) {
  return (
    <label className={`space-y-1.5 text-sm ${className ?? ""}`}>
      <span className="font-medium">{label}</span>
      {children}
    </label>
  );
}

function StatusLine({
  configured,
  label,
}: {
  configured: boolean;
  label: string;
}) {
  return (
    <div className="flex items-center justify-between gap-3 text-sm">
      <span>{label}</span>
      <span className="flex items-center gap-1 text-muted-foreground">
        {configured ? (
          <CheckCircle2
            aria-hidden="true"
            className="size-4 text-emerald-600"
          />
        ) : (
          <X aria-hidden="true" className="size-4 text-amber-600" />
        )}
        {configured ? "已验证 / 已配置" : "待检查"}
      </span>
    </div>
  );
}

function formFromInstance(instance: AgentInstance): AgentForm {
  return {
    ...emptyForm,
    allowedTools: instance.grant.allowed_tools,
    displayName: instance.display_name,
    managementMode: instance.management_mode,
    managementUrl: instance.management_url ?? "",
    profile: instance.profile ?? "",
    requestTimeoutSeconds: String(instance.request_timeout_seconds),
    runtimeUrl: instance.runtime_url,
  };
}

function createInput(form: AgentForm): AgentInstanceInput {
  return {
    adapter_type: "hermes",
    allowed_tools: form.allowedTools,
    display_name: form.displayName.trim(),
    hermes_api_key: form.hermesApiKey,
    management_mode: form.managementMode,
    profile: optionalValue(form.profile),
    request_timeout_seconds: parseRequestTimeout(form.requestTimeoutSeconds),
    runtime_url: form.runtimeUrl.trim(),
    ...(form.managementUrl.trim()
      ? { management_url: form.managementUrl.trim() }
      : {}),
    ...(form.managementMode === "auto"
      ? {
          dashboard_session_token: form.dashboardSessionToken,
          ...(form.cloudflareClientId && form.cloudflareClientSecret
            ? {
                cloudflare_access_client_id: form.cloudflareClientId,
                cloudflare_access_client_secret: form.cloudflareClientSecret,
              }
            : {}),
        }
      : {}),
  };
}

function updateInput(form: AgentForm) {
  const input: Record<string, unknown> = {
    allowed_tools: form.allowedTools,
    display_name: form.displayName.trim(),
    management_mode: form.managementMode,
    profile: optionalValue(form.profile),
    request_timeout_seconds: parseRequestTimeout(form.requestTimeoutSeconds),
    runtime_url: form.runtimeUrl.trim(),
    ...(form.managementUrl.trim()
      ? { management_url: form.managementUrl.trim() }
      : {}),
  };
  if (form.hermesApiKey) {
    input.hermes_api_key = form.hermesApiKey;
  }
  if (form.managementMode === "auto") {
    if (form.dashboardSessionToken) {
      input.dashboard_session_token = form.dashboardSessionToken;
    }
    if (form.cloudflareClientId && form.cloudflareClientSecret) {
      input.cloudflare_access_client_id = form.cloudflareClientId;
      input.cloudflare_access_client_secret = form.cloudflareClientSecret;
    }
  }
  return input;
}

function clearSecrets(form: AgentForm): AgentForm {
  return {
    ...form,
    cloudflareClientId: "",
    cloudflareClientSecret: "",
    dashboardSessionToken: "",
    hermesApiKey: "",
  };
}

function optionalValue(value: string): string | undefined {
  return value.trim() || undefined;
}

function parseRequestTimeout(value: string): number {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 1 || parsed > 300) {
    throw new Error("Hermes 请求超时必须是 1 到 300 秒之间的整数");
  }
  return parsed;
}

function normalizeProvisioningResult(
  result: AgentInstanceProvisioningResult,
): AgentInstanceProvisioningResult {
  return result;
}

function showError(fallback: string) {
  return (error: unknown) =>
    toast.error(error instanceof Error ? error.message : fallback);
}
