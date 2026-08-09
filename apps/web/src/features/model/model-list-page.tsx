"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, Clock3, FileText, Plus, Settings2, Waypoints } from "lucide-react";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { useCurrentProject } from "@/components/providers/project-provider";
import { EmptyState } from "@/components/states/empty-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { apiClient } from "@/lib/api-client";

import { RefreshIcon } from "./refresh-icon";
import type { ModelOverview, ModelQuestion } from "./types";

export function ModelListPage() {
  const project = useCurrentProject();
  const queryClient = useQueryClient();
  const base = `/projects/${encodeURIComponent(project.id)}/models`;
  const overview = useQuery({
    queryFn: () => apiClient.request<ModelOverview>(base),
    queryKey: ["models", project.id],
    refetchInterval: (query) => {
      const data = query.state.data as ModelOverview | undefined;
      if (data && !data.configured) return 2_000;
      return data?.source?.sync_status === "queued" || data?.source?.sync_status === "running" || data?.questions.some((item) => item.sync_status === "queued" || item.sync_status === "running") ? 1_500 : false;
    },
  });
  const [code, setCode] = useState("");
  const [codeTouched, setCodeTouched] = useState(false);
  const [title, setTitle] = useState("");
  const [pageId, setPageId] = useState("");
  const defaultCode = useMemo(() => nextQuestionCode(overview.data?.questions ?? []), [overview.data?.questions]);
  const countdown = useCountdown(overview.data?.source?.next_sync_at);
  const availablePages = useMemo(() => overview.data?.discovered_pages.filter((page) => !page.bound_question_id) ?? [], [overview.data?.discovered_pages]);
  useEffect(() => {
    if (!codeTouched && defaultCode) setCode(defaultCode);
  }, [codeTouched, defaultCode]);
  useEffect(() => {
    if (!pageId && availablePages[0]) setPageId(availablePages[0].notion_page_id);
  }, [availablePages, pageId]);
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["models", project.id] });
  const sync = useMutation({
    mutationFn: () => apiClient.request(`${base}/source/sync`, { method: "POST" }),
    onSuccess: async () => { await refresh(); toast.success("已请求同步全部模型，自动同步倒计时已重置"); },
  });
  const create = useMutation({
    mutationFn: () => apiClient.request(`${base}/questions`, { body: { code: code.trim() || defaultCode, title: title.trim(), notion_page_id: pageId, position: overview.data?.questions.length ?? 0 }, method: "POST" }),
    onSuccess: async () => { setCodeTouched(false); setCode(""); setTitle(""); await refresh(); toast.success("题号已创建并绑定 Notion 子页面"); },
  });
  const configured = overview.data?.configured ?? false;

  return (
    <section className="space-y-6" aria-labelledby="models-title">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="mb-3 flex size-10 items-center justify-center rounded-lg border border-border bg-card shadow-xs"><Waypoints aria-hidden="true" className="size-5" /></div>
          <h1 className="text-2xl font-semibold tracking-tight" id="models-title">模型版本</h1>
          <p className="mt-1 text-sm text-muted-foreground">每个题号绑定一个 Notion 子页面，版本链彼此独立。</p>
        </div>
        <div className="flex items-center gap-2">
          {overview.data?.source?.auto_sync_enabled ? <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground"><Clock3 className="size-3.5" />{formatCountdown(countdown)}</span> : null}
          <Link className="inline-flex h-9 items-center justify-center gap-2 rounded-md border border-border bg-background px-4 text-sm font-medium shadow-xs hover:bg-accent" href={`/projects/${encodeURIComponent(project.id)}/settings#model-settings`}><Settings2 className="size-4" />设置</Link>
          <Button disabled={!configured || sync.isPending} onClick={() => sync.mutate()}><RefreshIcon spinning={sync.isPending} />同步</Button>
        </div>
      </header>

      {overview.isLoading ? <p className="text-sm text-muted-foreground">正在读取模型来源…</p> : null}
      {overview.error ? <p className="text-sm text-destructive">{overview.error.message}</p> : null}
      {overview.data && !configured ? (
        <EmptyState action={<Link className="inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground shadow-sm hover:bg-primary/90" href={`/projects/${encodeURIComponent(project.id)}/settings#model-settings`}>现在绑定</Link>} description="先在项目设置中填写只读 Notion Integration Token 和根页面 URL；保存后 mmdash 会递归发现所有子页面。" title="尚未绑定 Notion 模型来源" />
      ) : null}
      {overview.data?.source ? (
        <Card><CardContent className="flex flex-wrap items-center justify-between gap-4 p-4"><div><p className="font-medium">{overview.data.source.notion_root_title || "Notion 根页面"}</p><p className="text-xs text-muted-foreground">已发现 {overview.data.source.discovered_page_count} 个子页面 · 每 {Math.round(overview.data.source.auto_sync_interval_seconds / 60)} 分钟同步</p></div><Badge>{overview.data.source.sync_status}</Badge></CardContent></Card>
      ) : null}

      {configured ? (
        <Card><CardContent className="grid gap-3 p-4 lg:grid-cols-[8rem_minmax(12rem,1fr)_minmax(14rem,1.2fr)_auto]"><Input aria-label="题号" onChange={(event) => { setCodeTouched(true); setCode(event.target.value); }} placeholder={defaultCode || "Q1"} value={code} /><Input aria-label="题目标题" onChange={(event) => setTitle(event.target.value)} placeholder="问题标题" value={title} /><select aria-label="Notion 子页面" className="h-9 rounded-md border border-input bg-background px-3 text-sm" onChange={(event) => setPageId(event.target.value)} value={pageId}><option value="">选择未绑定的子页面</option>{availablePages.map((page) => <option key={page.notion_page_id} value={page.notion_page_id}>{"　".repeat(Math.max(0, page.depth - 1))}{page.title}</option>)}</select><Button disabled={!title.trim() || !pageId || create.isPending} onClick={() => create.mutate()}><Plus className="size-4" />新建题号</Button></CardContent></Card>
      ) : null}
      {create.error ? <p className="text-sm text-destructive">{create.error.message}</p> : null}

      <div className="space-y-3">
        {overview.data?.questions.map((question) => (
          <Link className="group block" href={`/projects/${encodeURIComponent(project.id)}/models/${encodeURIComponent(question.question_id)}`} key={question.question_id}>
            <Card className="transition-colors group-hover:border-foreground/25"><CardContent className="flex items-center gap-4 p-5"><div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted"><FileText className="size-5" /></div><div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><span className="font-semibold">{question.code}</span><span className="truncate">{question.title}</span><Badge>{question.sync_status}</Badge></div><p className="mt-1 text-xs text-muted-foreground">{question.snapshot_count} 个版本{question.last_synced_at ? ` · 最近同步 ${new Date(question.last_synced_at).toLocaleString()}` : " · 尚未生成版本"}</p></div><ArrowRight className="size-4 text-muted-foreground transition-transform group-hover:translate-x-1" /></CardContent></Card>
          </Link>
        ))}
        {configured && overview.data?.questions.length === 0 ? <EmptyState description="在上方创建 Q1、Q2，并分别绑定一个已发现的 Notion 子页面。" title="还没有模型题号" /> : null}
      </div>
    </section>
  );
}

function nextQuestionCode(questions: ModelQuestion[]): string {
  const used = new Set(
    questions
      .map((question) => /^Q(\d+)$/i.exec(question.code)?.[1])
      .filter((value): value is string => Boolean(value)),
  );
  let number = 1;
  while (used.has(String(number))) number += 1;
  return `Q${number}`;
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
  return `自动同步 ${Math.floor(seconds / 60).toString().padStart(2, "0")}:${(seconds % 60).toString().padStart(2, "0")}`;
}
