"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Cable,
  CheckCircle2,
  Clock3,
  ExternalLink,
  KeyRound,
  Save,
  Unplug,
} from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
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
import { optionalRequest } from "@/features/repo/optional-request";
import { apiClient } from "@/lib/api-client";

import type { ModelOverview } from "./types";

const settingType = "model.notion";
const redactedSecret = "********";

type ModelSetting = {
  values: Record<string, unknown>;
  version: number;
  updated_at: string;
};
type NotionOAuthConnection = {
  available: boolean;
  connected: boolean;
  bot_id?: string;
  workspace_id?: string;
  workspace_name?: string;
  workspace_icon?: string;
};
type NotionOAuthAuthorization = {
  authorization_url: string;
  expires_at: string;
};
type ConnectionTest = {
  status: "passed" | "failed" | "unsupported";
  checked_at: string;
  checks: { name: string; status: "passed" | "failed"; message?: string }[];
};

export function ModelSettingsPanel() {
  const project = useCurrentProject();
  const queryClient = useQueryClient();
  const settingPath = `/projects/${encodeURIComponent(project.id)}/settings/${encodeURIComponent(settingType)}`;
  const oauthPath = `/projects/${encodeURIComponent(project.id)}/models/notion/oauth`;
  const [rootUrl, setRootUrl] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [intervalMinutes, setIntervalMinutes] = useState(5);
  const [testResult, setTestResult] = useState<ConnectionTest>();
  const initialized = useRef<string | undefined>(undefined);
  const setting = useQuery({
    queryFn: () => optionalRequest<ModelSetting>(apiClient, settingPath),
    queryKey: ["model-setting", project.id],
    retry: false,
  });
  const oauth = useQuery({
    queryFn: () => apiClient.request<NotionOAuthConnection>(oauthPath),
    queryKey: ["model-notion-oauth", project.id],
    retry: false,
  });
  const overview = useQuery({
    queryFn: () =>
      apiClient.request<ModelOverview>(
        `/projects/${encodeURIComponent(project.id)}/models`,
      ),
    queryKey: ["models", project.id],
    retry: false,
  });

  useEffect(() => {
    const key = setting.data ? String(setting.data.version) : "empty";
    if (setting.isPending || initialized.current === key) return;
    initialized.current = key;
    const values = setting.data?.values;
    setRootUrl(
      typeof values?.root_page_url === "string" ? values.root_page_url : "",
    );
    setEnabled(
      typeof values?.auto_sync_enabled === "boolean"
        ? values.auto_sync_enabled
        : true,
    );
    setIntervalMinutes(
      typeof values?.auto_sync_interval_seconds === "number"
        ? Math.max(1, Math.round(values.auto_sync_interval_seconds / 60))
        : 5,
    );
  }, [setting.data, setting.isPending]);

  const configuration = () => {
    const minutes = Math.round(intervalMinutes);
    if (!rootUrl.trim() || minutes < 1 || minutes > 1_440)
      throw new Error("请填写 Notion 根页面 URL，并使用 1–1440 分钟的同步间隔");
    return {
      root_page_url: rootUrl.trim(),
      auto_sync_enabled: enabled,
      auto_sync_interval_seconds: minutes * 60,
    };
  };
  const invalidate = async () =>
    Promise.all([
      queryClient.invalidateQueries({
        queryKey: ["model-setting", project.id],
      }),
      queryClient.invalidateQueries({
        queryKey: ["model-notion-oauth", project.id],
      }),
      queryClient.invalidateQueries({ queryKey: ["models", project.id] }),
    ]);
  const saveSetting = async () => {
    const saved = await apiClient.request<ModelSetting>(settingPath, {
      body: { values: configuration() },
      method: "PATCH",
    });
    initialized.current = undefined;
    await invalidate();
    return saved;
  };
  const start = useMutation({
    mutationFn: () =>
      apiClient.request<NotionOAuthAuthorization>(
        `${oauthPath}/authorizations`,
        { body: configuration(), method: "POST" },
      ),
    onSuccess: (result) => window.location.assign(result.authorization_url),
  });
  const save = useMutation({
    mutationFn: saveSetting,
    onSuccess: () => toast.success("Notion 根页面与同步策略已保存"),
  });
  const test = useMutation({
    mutationFn: async () => {
      await saveSetting();
      return apiClient.request<ConnectionTest>(`${settingPath}/test`, {
        method: "POST",
      });
    },
    onSuccess: (result) => {
      setTestResult(result);
      if (result.status === "passed")
        toast.success("Notion 根页面读取测试通过");
      else toast.error("Notion 连接测试未通过");
    },
  });
  const disconnect = useMutation({
    mutationFn: () =>
      apiClient.request<void>(`${oauthPath}/connection`, { method: "DELETE" }),
    onSuccess: async () => {
      initialized.current = undefined;
      await invalidate();
      toast.success("已断开 Notion 授权；历史模型版本仍保留");
    },
  });
  const error =
    start.error ??
    save.error ??
    test.error ??
    disconnect.error ??
    oauth.error ??
    setting.error;
  const source = overview.data?.source;
  const countdown = useCountdown(source?.next_sync_at);
  const legacyConnected =
    setting.data?.values.integration_token === redactedSecret &&
    !oauth.data?.connected;
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (oauth.data?.connected) save.mutate();
    else start.mutate();
  }

  return (
    <section
      className="scroll-mt-6 space-y-4"
      id="model-settings"
      aria-labelledby="model-settings-title"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold" id="model-settings-title">
            Model · Notion
          </h2>
          <p className="text-sm text-muted-foreground">
            一个项目只绑定一个 Notion 根页面。用户通过 mmdash
            公共集成授权，无需复制 Integration Token。
          </p>
        </div>
        <Badge>
          {oauth.data?.connected
            ? oauth.data.workspace_name || "已授权"
            : "未授权"}
        </Badge>
      </div>
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <KeyRound className="size-4" />
            Notion 授权与来源
          </CardTitle>
          <CardDescription>
            授权时请在 Notion
            页面选择器中包含这里填写的根页面；根页面下已授权的子页面会被递归发现。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form className="space-y-4" onSubmit={submit}>
            <label className="space-y-2 text-sm">
              <span className="font-medium">Notion 根页面 URL</span>
              <Input
                aria-label="Notion 根页面 URL"
                onChange={(event) => setRootUrl(event.target.value)}
                placeholder="https://workspace.notion.site/…"
                type="url"
                value={rootUrl}
              />
              <span className="block text-xs text-muted-foreground">
                该页面本身作为项目来源，Q1/Q2 只能绑定其后代页面。
              </span>
            </label>
            <div className="grid items-end gap-4 sm:grid-cols-[1fr_auto]">
              <label className="space-y-2 text-sm">
                <span className="font-medium">自动同步间隔（分钟）</span>
                <Input
                  aria-label="自动同步间隔（分钟）"
                  max={1_440}
                  min={1}
                  onChange={(event) =>
                    setIntervalMinutes(Number(event.target.value))
                  }
                  type="number"
                  value={intervalMinutes}
                />
              </label>
              <label className="flex h-9 items-center gap-2 text-sm">
                <input
                  checked={enabled}
                  onChange={(event) => setEnabled(event.target.checked)}
                  type="checkbox"
                />
                启用自动同步
              </label>
            </div>
            {oauth.data?.connected ? (
              <p className="rounded-lg bg-emerald-50 p-3 text-sm text-emerald-800">
                已授权 Workspace：
                {oauth.data.workspace_name || oauth.data.workspace_id}
                。访问令牌与刷新令牌仅在 Core 中加密保存。
              </p>
            ) : null}
            {legacyConnected ? (
              <p className="rounded-lg bg-amber-50 p-3 text-sm text-amber-800">
                当前仍是升级前的 Integration Token 连接。完成一次 OAuth
                授权后会安全替换旧凭据。
              </p>
            ) : null}
            {oauth.data && !oauth.data.available ? (
              <p className="rounded-lg bg-amber-50 p-3 text-sm text-amber-800">
                服务端尚未配置 mmdash Notion 公共集成 Client ID 与 Client
                Secret，暂时无法开始授权。
              </p>
            ) : null}
            {source?.next_sync_at && enabled ? (
              <p className="flex items-center gap-2 rounded-lg bg-muted p-3 text-sm">
                <Clock3 className="size-4" />
                自动同步倒计时 {formatCountdown(countdown)} · 下次触发{" "}
                {new Date(source.next_sync_at).toLocaleString()}
              </p>
            ) : null}
            {error ? (
              <p className="text-sm text-destructive">{error.message}</p>
            ) : null}
            <div className="flex flex-wrap gap-2">
              {oauth.data?.connected ? (
                <Button
                  disabled={
                    save.isPending || test.isPending || disconnect.isPending
                  }
                  type="submit"
                >
                  <Save className="size-4" />
                  保存同步设置
                </Button>
              ) : (
                <Button
                  disabled={
                    start.isPending || oauth.isPending || !oauth.data?.available
                  }
                  type="submit"
                >
                  <ExternalLink className="size-4" />
                  授权并绑定 Notion
                </Button>
              )}
              {oauth.data?.connected ? (
                <Button
                  disabled={
                    save.isPending || test.isPending || disconnect.isPending
                  }
                  onClick={() => test.mutate()}
                  type="button"
                  variant="outline"
                >
                  <Cable className="size-4" />
                  保存并测试
                </Button>
              ) : null}
              {oauth.data?.connected ? (
                <Button
                  disabled={disconnect.isPending}
                  onClick={() => disconnect.mutate()}
                  type="button"
                  variant="outline"
                >
                  <Unplug className="size-4" />
                  断开授权
                </Button>
              ) : null}
            </div>
          </form>
        </CardContent>
      </Card>
      {testResult ? (
        <Card>
          <CardContent className="space-y-2 p-4">
            <p className="flex items-center gap-2 text-sm font-medium">
              <CheckCircle2 className="size-4" />
              连接测试：{testResult.status}
            </p>
            {testResult.checks.map((check) => (
              <div
                className="flex items-center justify-between gap-3 text-sm"
                key={check.name}
              >
                <span>{check.name}</span>
                <span
                  className={
                    check.status === "passed"
                      ? "text-emerald-600"
                      : "text-destructive"
                  }
                >
                  {check.message || check.status}
                </span>
              </div>
            ))}
          </CardContent>
        </Card>
      ) : null}
    </section>
  );
}

function useCountdown(nextSyncAt?: string) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!nextSyncAt) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1_000);
    return () => window.clearInterval(timer);
  }, [nextSyncAt]);
  return nextSyncAt
    ? Math.max(0, Math.ceil((new Date(nextSyncAt).getTime() - now) / 1_000))
    : 0;
}

function formatCountdown(seconds: number) {
  return `${Math.floor(seconds / 60)
    .toString()
    .padStart(2, "0")}:${(seconds % 60).toString().padStart(2, "0")}`;
}
