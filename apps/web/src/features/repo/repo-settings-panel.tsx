"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Cable,
  CheckCircle2,
  Copy,
  GitBranch,
  KeyRound,
  RefreshCw,
  RotateCw,
  ShieldAlert,
  Unplug,
} from "lucide-react";
import {
  type FormEvent,
  type ReactNode,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { toast } from "sonner";

import { useCurrentProject } from "@/components/providers/project-provider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { ApiError, apiClient } from "@/lib/api-client";

import { optionalRequest } from "./optional-request";
import type {
  ProjectPermissions,
  RepoBranch,
  RepoCommitPage,
  RepoConnectionTestResult,
  RepoSetting,
  Repository,
} from "./types";

const redactedSecret = "********";
const repoSettingType = "repo.connection";

type FormState = {
  accessToken: string;
  articleBranch: string;
  codeBranch: string;
  provider: "github" | "local";
  remoteUrl: string;
  resultBranch: string;
};

const defaultForm: FormState = {
  accessToken: "",
  articleBranch: "article",
  codeBranch: "main",
  provider: "github",
  remoteUrl: "",
  resultBranch: "result",
};

export function RepoSettingsPanel() {
  const project = useCurrentProject();
  const queryClient = useQueryClient();
  const base = `/projects/${encodeURIComponent(project.id)}`;
  const settingPath = `${base}/settings/${encodeURIComponent(repoSettingType)}`;
  const repoPath = `${base}/repository`;
  const initializedVersion = useRef<string | undefined>(undefined);
  const [form, setForm] = useState<FormState>(defaultForm);
  const [tested, setTested] = useState<RepoConnectionTestResult | null>(null);
  const [oneTimeSecret, setOneTimeSecret] = useState("");

  const setting = useQuery({
    queryFn: () => optionalRequest<RepoSetting>(apiClient, settingPath),
    queryKey: ["repo-setting", project.id],
    retry: false,
  });
  const repository = useQuery({
    queryFn: () => optionalRequest<Repository>(apiClient, repoPath),
    queryKey: ["repository", project.id],
    refetchInterval: (query) =>
      isTransitional((query.state.data as Repository | null)?.status)
        ? 2_000
        : false,
    retry: false,
  });
  const permissions = useQuery({
    queryFn: () => apiClient.request<ProjectPermissions>(`${base}/permissions`),
    queryKey: ["project-permissions", project.id],
  });
  const branches = useQuery({
    enabled: Boolean(
      repository.data && repository.data.status !== "disconnected",
    ),
    queryFn: () =>
      apiClient.request<{ items: RepoBranch[] }>(`${repoPath}/branches`),
    queryKey: ["repository-branches", project.id],
    retry: false,
  });
  const recentCommits = useQuery({
    enabled: repository.data?.status === "ready",
    queryFn: () =>
      apiClient.request<RepoCommitPage>(`${repoPath}/commits`, {
        query: { limit: 4, workspace: "code" },
        signal: undefined,
      }),
    queryKey: ["repository-recent-commits", project.id],
  });

  useEffect(() => {
    const value = setting.data;
    const initializationKey = value ? String(value.version) : "empty";
    if (setting.isPending || initializedVersion.current === initializationKey) {
      return;
    }
    initializedVersion.current = initializationKey;
    if (!value) {
      setForm(defaultForm);
      return;
    }
    setForm({
      accessToken: "",
      articleBranch: stringValue(value.values.article_branch, "article"),
      codeBranch: stringValue(value.values.code_branch, "main"),
      provider: value.values.provider === "local" ? "local" : "github",
      remoteUrl: stringValue(value.values.remote_url),
      resultBranch: stringValue(value.values.result_branch, "result"),
    });
  }, [setting.data, setting.isPending]);

  useEffect(() => {
    const current = repository.data;
    if (!current) {
      return;
    }
    const workspaces = Object.fromEntries(
      current.workspaces.map((workspace) => [
        workspace.workspace,
        workspace.remote_branch,
      ]),
    );
    setForm((previous) => ({
      ...previous,
      articleBranch: workspaces.article ?? previous.articleBranch,
      codeBranch: workspaces.code ?? previous.codeBranch,
      provider: current.provider,
      remoteUrl: current.remote_url ?? previous.remoteUrl,
      resultBranch: workspaces.result ?? previous.resultBranch,
    }));
  }, [repository.data]);

  const canManage =
    permissions.data?.permissions.includes("project.repo.manage") ?? false;
  const tokenConfigured = setting.data?.values.access_token === redactedSecret;
  const branchOptions = useMemo(
    () =>
      [
        ...(tested?.branches ?? []),
        ...(branches.data?.items.map((branch) => branch.name) ?? []),
      ].filter((branch, index, all) => all.indexOf(branch) === index),
    [branches.data?.items, tested?.branches],
  );

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: ["repo-setting", project.id],
      }),
      queryClient.invalidateQueries({
        queryKey: ["repository", project.id],
      }),
      queryClient.invalidateQueries({
        queryKey: ["repository-branches", project.id],
      }),
      queryClient.invalidateQueries({
        queryKey: ["repository-recent-commits", project.id],
      }),
    ]);
  };

  const saveSetting = async (): Promise<RepoSetting> => {
    validateMappings(form);
    const values: Record<string, unknown> = {
      article_branch: form.articleBranch.trim(),
      code_branch: form.codeBranch.trim(),
      provider: form.provider,
      remote_url: form.remoteUrl.trim(),
      result_branch: form.resultBranch.trim(),
    };
    if (form.accessToken.trim()) {
      values.access_token = form.accessToken.trim();
    } else if (tokenConfigured) {
      values.access_token = redactedSecret;
    }
    const saved = await apiClient.request<RepoSetting>(settingPath, {
      body: { values },
      method: "PATCH",
    });
    initializedVersion.current = undefined;
    await refresh();
    return saved;
  };

  const save = useMutation({
    mutationFn: saveSetting,
    onSuccess: () => toast.success("Repository 配置已保存"),
  });
  const test = useMutation({
    mutationFn: async () => {
      await saveSetting();
      return apiClient.request<RepoConnectionTestResult>(`${repoPath}/test`, {
        method: "POST",
      });
    },
    onSuccess: (result) => {
      setTested(result);
      if (result.status === "passed") {
        toast.success("连接、权限和分支检查均已通过");
      } else {
        toast.error("连接测试未通过");
      }
    },
  });
  const connect = useMutation({
    mutationFn: async () => {
      const saved = await saveSetting();
      const check = await apiClient.request<RepoConnectionTestResult>(
        `${repoPath}/test`,
        { method: "POST" },
      );
      setTested(check);
      if (check.status !== "passed") {
        throw new Error("Repository connection test failed");
      }
      return apiClient.request<Repository>(repoPath, {
        body: { settings_version: saved.version },
        method: "PUT",
      });
    },
    onSuccess: async (connected) => {
      setOneTimeSecret(connected.webhook.secret ?? "");
      await refresh();
      toast.success("Repository 已绑定，首次同步已进入队列");
    },
  });
  const applyMappings = useMutation({
    mutationFn: () => {
      validateMappings(form);
      return apiClient.request<Repository>(`${repoPath}/workspaces`, {
        body: {
          article_branch: form.articleBranch.trim(),
          code_branch: form.codeBranch.trim(),
          result_branch: form.resultBranch.trim(),
        },
        method: "PATCH",
      });
    },
    onSuccess: async () => {
      await refresh();
      toast.success("工作区映射已更新并请求同步");
    },
  });
  const sync = useMutation({
    mutationFn: () =>
      apiClient.request<Repository>(`${repoPath}/sync`, { method: "POST" }),
    onSuccess: async () => {
      await refresh();
      toast.success("同步请求已合并到队列");
    },
  });
  const rotate = useMutation({
    mutationFn: () =>
      apiClient.request<Repository>(`${repoPath}/webhook-secret`, {
        method: "POST",
      }),
    onSuccess: (updated) => {
      setOneTimeSecret(updated.webhook.secret ?? "");
      toast.success("Webhook Secret 已轮换；请立即更新 GitHub");
    },
  });
  const disconnect = useMutation({
    mutationFn: () => apiClient.request<void>(repoPath, { method: "DELETE" }),
    onSuccess: async () => {
      setOneTimeSecret("");
      await refresh();
      toast.success("Repository 已断开并等待受管清理");
    },
  });

  const mutationError = [
    save.error,
    test.error,
    connect.error,
    applyMappings.error,
    sync.error,
    rotate.error,
    disconnect.error,
  ].find(Boolean);
  const busy = [
    save,
    test,
    connect,
    applyMappings,
    sync,
    rotate,
    disconnect,
  ].some((mutation) => mutation.isPending);
  const connected = Boolean(repository.data);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (connected) {
      applyMappings.mutate();
    } else {
      connect.mutate();
    }
  }

  return (
    <section className="space-y-4" aria-labelledby="repo-settings-title">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold" id="repo-settings-title">
            Repository
          </h2>
          <p className="text-sm text-muted-foreground">
            绑定一个受管 Git 仓库，并把三个不同的实际分支映射为逻辑工作区。
          </p>
        </div>
        {repository.data ? (
          <Badge>
            <span
              aria-hidden="true"
              className="mr-1.5 size-2 rounded-full bg-current"
            />
            {repository.data.status}
          </Badge>
        ) : (
          <Badge>未绑定</Badge>
        )}
      </div>

      {!canManage && !permissions.isPending ? (
        <div
          className="flex items-start gap-3 rounded-lg border border-amber-300 bg-amber-50 p-4 text-sm text-amber-950"
          role="status"
        >
          <ShieldAlert aria-hidden="true" className="mt-0.5 size-4" />
          当前角色可查看 Repository 状态，但没有管理连接的权限。
        </div>
      ) : null}

      <Card>
        <form onSubmit={submit}>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Cable aria-hidden="true" className="size-4" />
              Provider 与分支
            </CardTitle>
            <CardDescription>
              GitHub 使用 HTTPS 与 fine-grained PAT；Local 路径必须位于 Core
              管理员配置的允许目录中。
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4 lg:grid-cols-2">
            <Field label="Provider">
              <select
                className={selectClass}
                disabled={!canManage || connected || busy}
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    provider: event.target.value as "github" | "local",
                  }))
                }
                value={form.provider}
              >
                <option value="github">GitHub</option>
                <option value="local">Local Git（受控路径）</option>
              </select>
            </Field>
            <Field
              label={
                form.provider === "github"
                  ? "GitHub HTTPS URL"
                  : "Core 可见的允许路径"
              }
            >
              <Input
                disabled={!canManage || connected || busy}
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    remoteUrl: event.target.value,
                  }))
                }
                placeholder={
                  form.provider === "github"
                    ? "https://github.com/owner/repository"
                    : "D:\\managed-repositories\\model"
                }
                required
                value={form.remoteUrl}
              />
            </Field>
            {form.provider === "github" ? (
              <Field label="Fine-grained PAT">
                <Input
                  autoComplete="new-password"
                  disabled={!canManage || busy}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      accessToken: event.target.value,
                    }))
                  }
                  placeholder={
                    tokenConfigured
                      ? "已加密配置；留空保持原值"
                      : "github_pat_…"
                  }
                  required={!tokenConfigured}
                  type="password"
                  value={form.accessToken}
                />
                <span className="mt-1 block text-xs text-muted-foreground">
                  <KeyRound aria-hidden="true" className="mr-1 inline size-3" />
                  {tokenConfigured ? "Secret 已配置并脱敏" : "尚未配置 Secret"}
                </span>
              </Field>
            ) : (
              <div className="rounded-lg border bg-muted/40 p-4 text-sm text-muted-foreground">
                Local Provider 不接收 PAT，也不会向浏览器返回规范化服务器路径。
              </div>
            )}
            <BranchField
              disabled={!canManage || busy}
              label="Code branch"
              onChange={(value) =>
                setForm((current) => ({ ...current, codeBranch: value }))
              }
              options={branchOptions}
              value={form.codeBranch}
            />
            <BranchField
              disabled={!canManage || busy}
              label="Article branch"
              onChange={(value) =>
                setForm((current) => ({ ...current, articleBranch: value }))
              }
              options={branchOptions}
              value={form.articleBranch}
            />
            <BranchField
              disabled={!canManage || busy}
              label="Result branch"
              onChange={(value) =>
                setForm((current) => ({ ...current, resultBranch: value }))
              }
              options={branchOptions}
              value={form.resultBranch}
            />
          </CardContent>
          <CardFooter className="flex-wrap">
            <Button
              disabled={!canManage || busy}
              onClick={() => save.mutate()}
              variant="outline"
            >
              保存配置
            </Button>
            <Button
              disabled={!canManage || busy}
              onClick={() => test.mutate()}
              variant="secondary"
            >
              测试连接
            </Button>
            <Button disabled={!canManage || busy} type="submit">
              {connected ? "应用分支映射" : "绑定 Repository"}
            </Button>
          </CardFooter>
        </form>
      </Card>

      {tested ? (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <CheckCircle2 aria-hidden="true" className="size-4" />
              连接测试：{tested.status}
            </CardTitle>
            <CardDescription>
              远程默认分支：{tested.default_branch || "未解析"}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <ul className="grid gap-2 md:grid-cols-2">
              {tested.checks.map((check) => (
                <li className="rounded-md border p-3 text-sm" key={check.name}>
                  <span className="font-medium">{check.name}</span>
                  <span className="ml-2 text-muted-foreground">
                    {check.status}
                    {check.message ? ` · ${check.message}` : ""}
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      ) : null}

      {repository.data ? (
        <div className="grid gap-4 xl:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <GitBranch aria-hidden="true" className="size-4" />
                同步状态
              </CardTitle>
              <CardDescription>
                {repository.data.display_name} · 默认分支{" "}
                {repository.data.default_branch}
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-3 text-sm">
              <dl className="space-y-3">
                <Definition
                  label="最近同步"
                  value={formatDate(repository.data.last_synced_at)}
                />
                {repository.data.workspaces.map((workspace) => (
                  <Definition
                    key={workspace.workspace}
                    label={workspace.workspace}
                    value={`${workspace.remote_branch} · ${shortSha(workspace.head_commit_sha)} · ${workspace.status}`}
                  />
                ))}
              </dl>
              {repository.data.last_error_code ? (
                <div className="rounded-md border border-red-300 bg-red-50 p-3 text-red-950">
                  <strong>{repository.data.last_error_code}</strong>
                  <p>{repository.data.last_error_message}</p>
                </div>
              ) : null}
            </CardContent>
            <CardFooter className="flex-wrap">
              <Button
                disabled={!canManage || busy}
                onClick={() => sync.mutate()}
                variant="outline"
              >
                <RefreshCw aria-hidden="true" className="size-4" />
                手工同步
              </Button>
              <Button
                disabled={!canManage || busy}
                onClick={() => {
                  if (
                    window.confirm(
                      "断开后 Core 会在宽限期后清理受管 Git 数据。确认断开？",
                    )
                  ) {
                    disconnect.mutate();
                  }
                }}
                variant="ghost"
              >
                <Unplug aria-hidden="true" className="size-4" />
                断开
              </Button>
            </CardFooter>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <KeyRound aria-hidden="true" className="size-4" />
                GitHub Webhook
              </CardTitle>
              <CardDescription>
                在 GitHub 配置 Push Event，并使用下方 URL 与一次性 Secret。
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <Field label="Payload URL">
                <Input readOnly value={repository.data.webhook.public_url} />
              </Field>
              <p className="text-sm text-muted-foreground">
                Secret 状态：{" "}
                {repository.data.webhook.secret_configured
                  ? "已配置"
                  : "未配置"}
              </p>
              {oneTimeSecret ? (
                <div
                  className="rounded-md border border-amber-300 bg-amber-50 p-3"
                  role="status"
                >
                  <p className="text-xs font-medium text-amber-950">
                    此 Secret 仅显示一次，请立即保存到 GitHub。
                  </p>
                  <div className="mt-2 flex gap-2">
                    <Input
                      aria-label="一次性 Webhook Secret"
                      readOnly
                      value={oneTimeSecret}
                    />
                    <Button
                      aria-label="复制 Webhook Secret"
                      onClick={async () => {
                        await navigator.clipboard.writeText(oneTimeSecret);
                        toast.success("Webhook Secret 已复制");
                      }}
                      size="icon"
                      variant="outline"
                    >
                      <Copy aria-hidden="true" className="size-4" />
                    </Button>
                  </div>
                </div>
              ) : null}
            </CardContent>
            <CardFooter>
              <Button
                disabled={
                  !canManage || busy || repository.data.provider !== "github"
                }
                onClick={() => rotate.mutate()}
                variant="outline"
              >
                <RotateCw aria-hidden="true" className="size-4" />
                轮换 Secret
              </Button>
            </CardFooter>
          </Card>
        </div>
      ) : null}

      {recentCommits.data?.items.length ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">最近 Code commits</CardTitle>
            <CardDescription>
              仅用于验证连接；完整只读浏览位于求解记录页。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <ul className="divide-y">
              {recentCommits.data.items.map((commit) => (
                <li className="flex gap-3 py-2 text-sm" key={commit.commit_sha}>
                  <code>{shortSha(commit.commit_sha)}</code>
                  <span className="truncate">
                    {firstLine(commit.message) || "(no message)"}
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      ) : null}

      {mutationError ? (
        <div
          className="rounded-md border border-red-300 bg-red-50 p-3 text-sm text-red-950"
          role="alert"
        >
          {safeMessage(mutationError)}
        </div>
      ) : null}
    </section>
  );
}

function Field({
  children,
  label,
}: Readonly<{ children: ReactNode; label: string }>) {
  return (
    <label className="block space-y-1.5 text-sm font-medium">
      <span>{label}</span>
      {children}
    </label>
  );
}

function BranchField({
  disabled,
  label,
  onChange,
  options,
  value,
}: Readonly<{
  disabled: boolean;
  label: string;
  onChange: (value: string) => void;
  options: string[];
  value: string;
}>) {
  if (options.length === 0) {
    return (
      <Field label={label}>
        <Input
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
          required
          value={value}
        />
      </Field>
    );
  }
  return (
    <Field label={label}>
      <select
        className={selectClass}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
        required
        value={value}
      >
        {!options.includes(value) ? <option>{value}</option> : null}
        {options.map((option) => (
          <option key={option}>{option}</option>
        ))}
      </select>
    </Field>
  );
}

function Definition({
  label,
  value,
}: Readonly<{ label: string; value: string }>) {
  return (
    <div className="grid grid-cols-[100px_1fr] gap-3">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 truncate">{value}</dd>
    </div>
  );
}

function validateMappings(form: FormState) {
  const branches = [
    form.codeBranch.trim(),
    form.articleBranch.trim(),
    form.resultBranch.trim(),
  ];
  if (
    !form.remoteUrl.trim() ||
    branches.some((branch) => !branch) ||
    new Set(branches).size !== 3
  ) {
    throw new Error(
      "Repository 地址不能为空，且 Code/Article/Result 必须映射到三个不同分支。",
    );
  }
}

function stringValue(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

function shortSha(value: string | null): string {
  return value ? value.slice(0, 8) : "未解析";
}

function firstLine(value: string): string {
  return value.trim().split("\n", 1)[0] ?? "";
}

function formatDate(value: string | null): string {
  return value ? new Date(value).toLocaleString() : "尚未同步";
}

function isTransitional(status: Repository["status"] | undefined): boolean {
  return Boolean(
    status && ["pending", "cloning", "configuring", "syncing"].includes(status),
  );
}

function safeMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return `${error.code}: ${error.message}`;
  }
  return error instanceof Error ? error.message : "Repository 操作失败";
}

const selectClass =
  "flex h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-sm shadow-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50";
