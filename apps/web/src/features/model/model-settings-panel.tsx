"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Cable, CheckCircle2, Clock3, KeyRound, ShieldCheck } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { toast } from "sonner";

import { useCurrentProject } from "@/components/providers/project-provider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { apiClient } from "@/lib/api-client";
import { optionalRequest } from "@/features/repo/optional-request";

import type { ModelOverview } from "./types";

const settingType = "model.notion";
const redactedSecret = "********";

type ModelSetting = {
  values: Record<string, unknown>;
  version: number;
  updated_at: string;
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
  const [token, setToken] = useState("");
  const [rootUrl, setRootUrl] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [intervalMinutes, setIntervalMinutes] = useState(5);
  const [testResult, setTestResult] = useState<ConnectionTest>();
  const initialized = useRef<string | undefined>(undefined);
  const setting = useQuery({ queryFn: () => optionalRequest<ModelSetting>(apiClient, settingPath), queryKey: ["model-setting", project.id], retry: false });
  const overview = useQuery({ queryFn: () => apiClient.request<ModelOverview>(`/projects/${encodeURIComponent(project.id)}/models`), queryKey: ["models", project.id], retry: false });
  useEffect(() => {
    const key = setting.data ? String(setting.data.version) : "empty";
    if (setting.isPending || initialized.current === key) return;
    initialized.current = key;
    const values = setting.data?.values;
    setToken("");
    setRootUrl(typeof values?.root_page_url === "string" ? values.root_page_url : "");
    setEnabled(typeof values?.auto_sync_enabled === "boolean" ? values.auto_sync_enabled : true);
    setIntervalMinutes(typeof values?.auto_sync_interval_seconds === "number" ? Math.max(1, Math.round(values.auto_sync_interval_seconds / 60)) : 5);
  }, [setting.data, setting.isPending]);
  const tokenConfigured = setting.data?.values.integration_token === redactedSecret;
  const saveSetting = async () => {
    const minutes = Math.round(intervalMinutes);
    if (!rootUrl.trim() || minutes < 1 || minutes > 1_440 || (!token.trim() && !tokenConfigured)) throw new Error("请填写 Token、根页面 URL，并使用 1–1440 分钟的同步间隔");
    const values: Record<string, unknown> = { root_page_url: rootUrl.trim(), auto_sync_enabled: enabled, auto_sync_interval_seconds: minutes * 60, integration_token: token.trim() || redactedSecret };
    const saved = await apiClient.request<ModelSetting>(settingPath, { body: { values }, method: "PATCH" });
    initialized.current = undefined;
    await Promise.all([queryClient.invalidateQueries({ queryKey: ["model-setting", project.id] }), queryClient.invalidateQueries({ queryKey: ["models", project.id] })]);
    return saved;
  };
  const save = useMutation({ mutationFn: saveSetting, onSuccess: () => toast.success("Notion 模型来源已保存，正在递归发现子页面") });
  const test = useMutation({ mutationFn: async () => { await saveSetting(); return apiClient.request<ConnectionTest>(`${settingPath}/test`, { method: "POST" }); }, onSuccess: (result) => { setTestResult(result); if (result.status === "passed") toast.success("Notion 根页面读取测试通过"); else toast.error("Notion 连接测试未通过"); } });
  const error = save.error ?? test.error ?? setting.error;
  const source = overview.data?.source;
  const countdown = useCountdown(source?.next_sync_at);
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); save.mutate(); }

  return (
    <section className="scroll-mt-6 space-y-4" id="model-settings" aria-labelledby="model-settings-title">
      <div className="flex flex-wrap items-start justify-between gap-3"><div><h2 className="text-lg font-semibold" id="model-settings-title">Model · Notion</h2><p className="text-sm text-muted-foreground">一个项目只绑定一个 Notion 根页面。Token 由 Core 加密保存，仅需读取内容权限。</p></div><Badge>{source ? source.sync_status : "未绑定"}</Badge></div>
      <Card><CardHeader><CardTitle className="flex items-center gap-2 text-base"><KeyRound className="size-4" />Notion 来源</CardTitle><CardDescription>把 Internal Integration 共享给根页面后，所有可访问的子页面会被递归发现；题号绑定在模型页完成。</CardDescription></CardHeader><CardContent><form className="space-y-4" onSubmit={submit}><div className="grid gap-4 lg:grid-cols-2"><label className="space-y-2 text-sm"><span className="font-medium">Integration Token</span><Input autoComplete="off" onChange={(event) => setToken(event.target.value)} placeholder={tokenConfigured ? "已保存；留空保持不变" : "ntn_…"} type="password" value={token} /><span className="block text-xs text-muted-foreground">不会发送给浏览器、Worker、MCP 或日志。</span></label><label className="space-y-2 text-sm"><span className="font-medium">Notion 根页面 URL</span><Input onChange={(event) => setRootUrl(event.target.value)} placeholder="https://www.notion.so/…" type="url" value={rootUrl} /><span className="block text-xs text-muted-foreground">该页面本身作为项目来源，Q1/Q2 只能绑定其后代页面。</span></label></div><div className="grid items-end gap-4 sm:grid-cols-[1fr_auto]"><label className="space-y-2 text-sm"><span className="font-medium">自动同步间隔（分钟）</span><Input max={1_440} min={1} onChange={(event) => setIntervalMinutes(Number(event.target.value))} type="number" value={intervalMinutes} /></label><label className="flex h-9 items-center gap-2 text-sm"><input checked={enabled} onChange={(event) => setEnabled(event.target.checked)} type="checkbox" />启用自动同步</label></div>{source?.next_sync_at && enabled ? <p className="flex items-center gap-2 rounded-lg bg-muted p-3 text-sm"><Clock3 className="size-4" />自动同步倒计时 {formatCountdown(countdown)} · 下次触发 {new Date(source.next_sync_at).toLocaleString()}</p> : null}{error ? <p className="text-sm text-destructive">{error.message}</p> : null}<div className="flex flex-wrap gap-2"><Button disabled={save.isPending || test.isPending} type="submit"><ShieldCheck className="size-4" />保存并绑定</Button><Button disabled={save.isPending || test.isPending} onClick={() => test.mutate()} type="button" variant="outline"><Cable className="size-4" />保存并测试</Button></div></form></CardContent></Card>
      {testResult ? <Card><CardContent className="space-y-2 p-4"><p className="flex items-center gap-2 text-sm font-medium"><CheckCircle2 className="size-4" />连接测试：{testResult.status}</p>{testResult.checks.map((check) => <div className="flex items-center justify-between gap-3 text-sm" key={check.name}><span>{check.name}</span><span className={check.status === "passed" ? "text-emerald-600" : "text-destructive"}>{check.message || check.status}</span></div>)}</CardContent></Card> : null}
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
  return nextSyncAt ? Math.max(0, Math.ceil((new Date(nextSyncAt).getTime() - now) / 1_000)) : 0;
}

function formatCountdown(seconds: number) {
  return `${Math.floor(seconds / 60).toString().padStart(2, "0")}:${(seconds % 60).toString().padStart(2, "0")}`;
}
