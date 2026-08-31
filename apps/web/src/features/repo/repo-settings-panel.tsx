"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Cable,
  CheckCircle2,
  Copy,
  FileCode2,
  GitBranch,
  KeyRound,
  RefreshCw,
  RotateCw,
  ShieldAlert,
  Unplug,
} from "lucide-react";
import Link from "next/link";
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
  RepoCapabilities,
  RepoCommitPage,
  RepoConnectionTestResult,
  RepoProvider,
  RepoSetting,
  Repository,
} from "./types";

const redactedSecret = "********";
const repoSettingType = "repo.connection";

type FormState = {
  accessToken: string;
  articleBranch: string;
  codeBranch: string;
  provider: RepoProvider;
  remoteUrl: string;
  resultBranch: string;
};

const defaultForm: FormState = {
  accessToken: "",
  articleBranch: "article",
  codeBranch: "main",
  provider: "managed",
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
  const capabilities = useQuery({
    queryFn: () =>
      apiClient.request<RepoCapabilities>(`${repoPath}/capabilities`),
    queryKey: ["repository-capabilities", project.id],
  });
  const disconnectedRepository =
    repository.data?.status === "disconnected" ? repository.data : null;
  const activeRepository = disconnectedRepository
    ? null
    : (repository.data ?? null);
  const recoveryMatchesForm = Boolean(
    disconnectedRepository &&
    disconnectedRepository.provider === form.provider &&
    sameRepositoryRemote(
      form.provider,
      form.remoteUrl,
      disconnectedRepository.remote_url,
      setting.data?.values.remote_url === redactedSecret,
    ),
  );
  const replacingDisconnectedRepository = Boolean(
    disconnectedRepository && !recoveryMatchesForm,
  );
  const permissions = useQuery({
    queryFn: () => apiClient.request<ProjectPermissions>(`${base}/permissions`),
    queryKey: ["project-permissions", project.id],
  });
  const branches = useQuery({
    enabled: Boolean(activeRepository),
    queryFn: () =>
      apiClient.request<{ items: RepoBranch[] }>(`${repoPath}/branches`),
    queryKey: ["repository-branches", project.id],
    retry: false,
  });
  const recentCommits = useQuery({
    enabled: activeRepository?.status === "ready",
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
      provider: repoProviderValue(value.values.provider),
      remoteUrl:
        value.values.remote_url === redactedSecret
          ? ""
          : stringValue(value.values.remote_url),
      resultBranch: stringValue(value.values.result_branch, "result"),
    });
  }, [setting.data, setting.isPending]);

  useEffect(() => {
    const current = repository.data;
    if (!current) {
      return;
    }
    if (
      current.status === "disconnected" &&
      setting.data &&
      setting.data.version > current.settings_version
    ) {
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
      remoteUrl:
        current.provider === "github" && current.remote_url
          ? current.remote_url
          : previous.remoteUrl,
      resultBranch: workspaces.result ?? previous.resultBranch,
    }));
  }, [repository.data, setting.data]);

  const canManage =
    permissions.data?.permissions.includes("project.repo.manage") ?? false;
  const configuredProvider = repoProviderValue(setting.data?.values.provider);
  const tokenConfigured =
    configuredProvider === "github" &&
    setting.data?.values.access_token === redactedSecret;
  const locationConfigured =
    configuredProvider === form.provider &&
    setting.data?.values.remote_url === redactedSecret;
  const serverExistingCapability = capabilities.data?.providers.find(
    (capability) => capability.provider === "server_existing",
  );
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
    validateMappings(form, locationConfigured);
    const values: Record<string, unknown> = {
      article_branch: form.articleBranch.trim(),
      code_branch: form.codeBranch.trim(),
      provider: form.provider,
      result_branch: form.resultBranch.trim(),
    };
    if (form.provider === "managed") {
      values.remote_url = null;
      values.access_token = null;
      values.webhook_secret = null;
    } else if (form.remoteUrl.trim()) {
      values.remote_url = form.remoteUrl.trim();
    } else if (locationConfigured) {
      values.remote_url = redactedSecret;
    }
    if (form.provider !== "github") {
      values.access_token = null;
      values.webhook_secret = null;
    } else if (form.accessToken.trim()) {
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
    mutationFn: async ({
      replaceDisconnected,
    }: {
      replaceDisconnected: boolean;
    }) => {
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
        body: {
          settings_version: saved.version,
          ...(replaceDisconnected ? { replace_disconnected: true } : {}),
        },
        method: "PUT",
      });
    },
    onSuccess: async (connected, { replaceDisconnected }) => {
      setOneTimeSecret(connected.webhook.secret ?? "");
      await refresh();
      toast.success(
        replaceDisconnected
          ? "旧绑定已清理，新的 Repository 已绑定并进入首次同步队列"
          : recoveryMatchesForm
            ? "Repository 已从宽限期恢复，同步已进入队列"
            : "Repository 已绑定，首次同步已进入队列",
      );
    },
  });
  const applyMappings = useMutation({
    mutationFn: () => {
      validateMappings(form, locationConfigured);
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
    mutationFn: async () => {
      try {
        await apiClient.request<void>(repoPath, { method: "DELETE" });
      } catch (error) {
        if (
          !(error instanceof ApiError) ||
          error.code !== "REPOSITORY_NOT_CONFIGURED"
        ) {
          throw error;
        }
      }
    },
    onSuccess: async () => {
      setOneTimeSecret("");
      setTested(null);
      queryClient.setQueryData<Repository | null>(
        ["repository", project.id],
        (current) => (current ? { ...current, status: "disconnected" } : null),
      );
      queryClient.removeQueries({
        queryKey: ["repository-branches", project.id],
      });
      queryClient.removeQueries({
        queryKey: ["repository-recent-commits", project.id],
      });
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
  const connected = Boolean(activeRepository);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (connected) {
      applyMappings.mutate();
    } else {
      if (
        replacingDisconnectedRepository &&
        !window.confirm(
          `将立即删除旧绑定 ${disconnectedRepository?.display_name ?? ""} 的 Core 托管 Git 数据与元数据，并改绑到新仓库。外部 GitHub/服务器仓库不会被删除；mmdash 托管仓库的权威数据会被删除。确认继续？`,
        )
      ) {
        return;
      }
      connect.mutate({
        replaceDisconnected: replacingDisconnectedRepository,
      });
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
              推荐由 mmdash 创建并维护仓库；也可以连接
              GitHub，或使用管理员已挂载并授权的服务器仓库。
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
                    provider: event.target.value as RepoProvider,
                    remoteUrl:
                      event.target.value === "managed" ? "" : current.remoteUrl,
                  }))
                }
                value={form.provider}
              >
                <option value="managed">mmdash 托管仓库（推荐）</option>
                <option value="github">GitHub 仓库</option>
                <option
                  disabled={serverExistingCapability?.enabled === false}
                  value="server_existing"
                >
                  服务器已有仓库
                  {serverExistingCapability?.enabled === false
                    ? "（当前部署未启用）"
                    : ""}
                </option>
              </select>
              {serverExistingCapability?.enabled === false ? (
                <span className="mt-1 block text-xs text-muted-foreground">
                  当前部署未启用服务器仓库接入。
                </span>
              ) : null}
            </Field>
            {form.provider === "managed" ? (
              <div className="rounded-lg border bg-muted/40 p-4 text-sm text-muted-foreground lg:col-span-2">
                Core 将在持久化的 Repo 存储中创建权威 bare 仓库，并自动初始化
                <code className="mx-1">main</code>、
                <code className="mx-1">article</code> 和
                <code className="mx-1">result</code> 三个逻辑工作区分支。 v0.1
                仅允许通过 mmdash 读写，不提供外部 Git clone、fetch 或 push
                地址。
              </div>
            ) : form.provider === "github" ? (
              <>
                <Field label="GitHub HTTPS URL">
                  <Input
                    disabled={!canManage || connected || busy}
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        remoteUrl: event.target.value,
                      }))
                    }
                    placeholder={
                      locationConfigured
                        ? "已安全配置；留空保持原值"
                        : "https://github.com/owner/repository"
                    }
                    required={!locationConfigured}
                    value={form.remoteUrl}
                  />
                </Field>
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
                    <KeyRound
                      aria-hidden="true"
                      className="mr-1 inline size-3"
                    />
                    {tokenConfigured
                      ? "Secret 已配置并脱敏"
                      : "尚未配置 Secret"}
                  </span>
                </Field>
              </>
            ) : (
              <>
                <Field label="Core 服务容器内的绝对路径">
                  <Input
                    disabled={!canManage || connected || busy}
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        remoteUrl: event.target.value,
                      }))
                    }
                    placeholder={
                      locationConfigured
                        ? "已安全配置；留空保持原值"
                        : "/srv/mmdash/repositories/model.git"
                    }
                    required={!locationConfigured}
                    value={form.remoteUrl}
                  />
                </Field>
                <div className="rounded-lg border bg-muted/40 p-4 text-sm text-muted-foreground">
                  仅供管理员或高级部署使用。路径必须已挂载到 Core，并位于
                  <code className="mx-1">REPO_LOCAL_ALLOWED_ROOTS</code>
                  授权范围内；浏览器和 API 不会返回规范化服务器路径。
                </div>
              </>
            )}
            {form.provider !== "managed" ? (
              <>
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
              </>
            ) : null}
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
            <Button
              disabled={
                !canManage || busy || (connected && form.provider === "managed")
              }
              type="submit"
            >
              {recoveryMatchesForm
                ? "恢复 Repository"
                : connected && form.provider === "managed"
                  ? "托管仓库已绑定"
                  : connected
                    ? "应用分支映射"
                    : replacingDisconnectedRepository
                      ? "立即清理旧绑定并改绑"
                      : "绑定 Repository"}
            </Button>
          </CardFooter>
        </form>
      </Card>

      {recoveryMatchesForm ? (
        <div
          className="rounded-lg border border-amber-300 bg-amber-50 p-4 text-sm text-amber-950"
          role="status"
        >
          Repository 已断开，但旧的受管 Git 数据仍在恢复宽限期内。使用相同的
          Provider 与仓库配置可以立即恢复并复用原记录；GitHub PAT
          和外部仓库分支映射可以更新。
        </div>
      ) : null}

      {replacingDisconnectedRepository ? (
        <div
          className="rounded-lg border border-red-300 bg-red-50 p-4 text-sm text-red-950"
          role="status"
        >
          当前地址与已断开的 {disconnectedRepository?.display_name}{" "}
          不同。继续后将立即删除旧绑定的 Core 托管本地 Git
          数据与元数据，再绑定新仓库。GitHub 和服务器已有仓库的外部 Git
          数据不会被删除；mmdash 托管仓库的权威数据会被删除。
        </div>
      ) : null}

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
            {tested.error_code ? (
              <p className="mt-3 text-sm text-muted-foreground">
                {tested.error_code}
                {tested.retryable
                  ? " · 外部网络暂时不可用，可以稍后重试。"
                  : " · 请修正配置或权限后重试。"}
              </p>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {activeRepository ? (
        <div className="grid gap-4 xl:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-base">
                <GitBranch aria-hidden="true" className="size-4" />
                同步状态
              </CardTitle>
              <CardDescription>
                {activeRepository.display_name} · 默认分支{" "}
                {activeRepository.default_branch}
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-3 text-sm">
              <dl className="space-y-3">
                <Definition
                  label="最近同步"
                  value={formatDate(activeRepository.last_synced_at)}
                />
                {activeRepository.workspaces.map((workspace) => (
                  <Definition
                    key={workspace.workspace}
                    label={workspace.workspace}
                    value={`${workspace.remote_branch} · ${shortSha(workspace.head_commit_sha)} · ${workspace.status}`}
                  />
                ))}
              </dl>
              {activeRepository.last_error_code ? (
                <div
                  className={
                    activeRepository.last_error_retryable
                      ? "rounded-md border border-amber-300 bg-amber-50 p-3 text-amber-950"
                      : "rounded-md border border-red-300 bg-red-50 p-3 text-red-950"
                  }
                >
                  <strong>{activeRepository.last_error_code}</strong>
                  <p>{repositoryFailureMessage(activeRepository)}</p>
                </div>
              ) : null}
            </CardContent>
            <CardFooter className="flex-wrap">
              <Link
                className="inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-md border border-border bg-background px-4 py-2 text-sm font-medium shadow-xs transition-colors outline-none hover:bg-accent hover:text-accent-foreground focus-visible:ring-2 focus-visible:ring-ring"
                href={`${base}/repository`}
              >
                <FileCode2 aria-hidden="true" className="size-4" />
                查看 Repository
              </Link>
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
                      activeRepository.provider === "managed"
                        ? "断开后 Core 会在宽限期结束时删除这个 mmdash 托管仓库的权威 Git 数据。确认断开？"
                        : "断开后 Core 会在宽限期结束时清理内部镜像和工作区，但不会删除外部 Git 仓库。确认断开？",
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

          {activeRepository.provider === "github" ? (
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
                  <Input readOnly value={activeRepository.webhook.public_url} />
                </Field>
                <p className="text-sm text-muted-foreground">
                  Secret 状态：{" "}
                  {activeRepository.webhook.secret_configured
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
                    !canManage || busy || activeRepository.provider !== "github"
                  }
                  onClick={() => rotate.mutate()}
                  variant="outline"
                >
                  <RotateCw aria-hidden="true" className="size-4" />
                  轮换 Secret
                </Button>
              </CardFooter>
            </Card>
          ) : null}
        </div>
      ) : null}

      {activeRepository && recentCommits.data?.items.length ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">最近 Code commits</CardTitle>
            <CardDescription>
              仅用于验证连接；完整只读浏览位于独立的 Repository 页面。
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

function validateMappings(form: FormState, locationConfigured: boolean) {
  const branches = [
    form.codeBranch.trim(),
    form.articleBranch.trim(),
    form.resultBranch.trim(),
  ];
  if (
    (form.provider !== "managed" &&
      !form.remoteUrl.trim() &&
      !locationConfigured) ||
    branches.some((branch) => !branch) ||
    new Set(branches).size !== 3
  ) {
    throw new Error(
      form.provider === "managed"
        ? "托管仓库的 Code/Article/Result 初始化分支必须保持为三个不同分支。"
        : "Repository 地址不能为空，且 Code/Article/Result 必须映射到三个不同分支。",
    );
  }
}

function stringValue(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

function sameRepositoryRemote(
  provider: FormState["provider"],
  candidate: string,
  disconnectedRemote: string | null,
  locationConfigured: boolean,
): boolean {
  if (provider === "managed") {
    return true;
  }
  if (provider === "server_existing") {
    return !candidate.trim() && locationConfigured;
  }
  if (!disconnectedRemote) {
    return false;
  }
  return (
    normalizeGitHubRemote(candidate) ===
    normalizeGitHubRemote(disconnectedRemote)
  );
}

function repoProviderValue(value: unknown): RepoProvider {
  switch (value) {
    case "github":
      return "github";
    case "server_existing":
    case "local":
      return "server_existing";
    case "managed":
    default:
      return "managed";
  }
}

function normalizeGitHubRemote(value: string): string {
  const trimmed = value.trim();
  try {
    const parsed = new URL(trimmed);
    const segments = parsed.pathname.split("/").filter(Boolean);
    if (
      parsed.protocol !== "https:" ||
      parsed.hostname.toLowerCase() !== "github.com" ||
      parsed.port ||
      parsed.username ||
      parsed.password ||
      parsed.search ||
      parsed.hash ||
      segments.length !== 2
    ) {
      return trimmed;
    }
    const repository = segments[1].replace(/\.git$/u, "");
    return `https://github.com/${segments[0]}/${repository}`;
  } catch {
    return trimmed;
  }
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
    if (error.retryable) {
      return `${error.code}: 外部网络暂时不可用，请稍后重试。`;
    }
    return `${error.code}: ${error.message}`;
  }
  return error instanceof Error ? error.message : "Repository 操作失败";
}

function repositoryFailureMessage(repository: Repository): string {
  if (!repository.last_error_retryable) {
    return repository.last_error_message ?? "Repository 同步失败";
  }
  switch (repository.last_error_code) {
    case "REPO_GIT_TIMEOUT":
      return "外部仓库操作超时，系统正在按退避策略重试。现有镜像和工作区仍可只读浏览。";
    case "REPO_PROVIDER_TEMPORARILY_UNAVAILABLE":
      return "GitHub 暂时不可用，系统正在按退避策略重试。现有镜像和工作区仍可只读浏览。";
    default:
      return "外部网络暂时不可用，系统正在按退避策略重试。现有镜像和工作区仍可只读浏览。";
  }
}

const selectClass =
  "flex h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-sm shadow-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50";
