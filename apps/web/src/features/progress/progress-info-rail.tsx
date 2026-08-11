"use client";

import { AlertTriangle, Bot, CheckCheck, Clock3, Play, ShieldQuestion, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/cn";

import { localDayKey } from "./calendar-time";
import type { ProgressAggregate, ProgressProposal } from "./types";

export function ProgressInfoRail({ activeAgents, canEvaluate, canManage, layout, onAgentChange, onBatchReview, onRecalculate, pending, progress, updatingAgent }: Readonly<{
  activeAgents: { id: string; name: string }[];
  canEvaluate: boolean;
  canManage: boolean;
  layout: "horizontal" | "vertical";
  onAgentChange: (id: string) => void;
  onBatchReview: (ids: string[], decision: "accepted" | "rejected") => void;
  onRecalculate: () => void;
  pending: ProgressProposal[];
  progress: ProgressAggregate;
  updatingAgent: boolean;
}>) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), 1_000); return () => window.clearInterval(timer); }, []);
  const todayKey = localDayKey(new Date(now));
  const todayCount = progress.tasks.filter((task) => task.status !== "done" && [task.start_at, task.due_at].some((value) => value && localDayKey(new Date(value)) === todayKey)).length;
  const urgent = useMemo(() => {
    const risks = progress.latest_evaluation?.risks.filter((risk) => risk.status === "open" && (risk.severity === "high" || risk.severity === "critical")).map((risk) => risk.title) ?? [];
    return [...new Set([...progress.tracking.blockers, ...risks])];
  }, [progress.latest_evaluation?.risks, progress.tracking.blockers]);
  const countdown = nextTrackingLabel(progress, now);
  const ids = pending.map((proposal) => proposal.proposal_id);

  return (
    <aside aria-label="Progress 信息栏" className={cn("grid gap-4", layout === "horizontal" && "lg:grid-cols-[minmax(0,1.25fr)_minmax(18rem,.75fr)]")}>
      <Card>
        <CardHeader className="pb-3"><div className="flex flex-wrap items-center justify-between gap-2"><CardTitle className="flex items-center gap-2 text-base"><Bot aria-hidden="true" className="size-4" />自动进度追踪</CardTitle><Badge>{progress.tracking.effective_stage || "尚未评估"}</Badge></div><p className="text-sm text-muted-foreground">{progress.tracking.summary || "等待首次进度评估。"}</p></CardHeader>
        <CardContent className="space-y-4">
          <EvaluationStages status={progress.latest_evaluation?.status} waitingReview={pending.length > 0} />
          <div className={cn("grid gap-3", layout === "horizontal" && "sm:grid-cols-2")}>
            <SummaryList icon={CheckCheck} items={progress.tracking.completed_items} title="AI 判断已完成" empty="本轮没有完成判断" />
            <SummaryList icon={AlertTriangle} items={urgent} title="紧急与阻塞" empty="当前没有紧急或阻塞事项" tone="danger" />
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-muted/30 p-3">
            <div className="min-w-40 flex-1"><p className="flex items-center gap-2 text-xs font-medium"><Clock3 aria-hidden="true" className="size-3.5" />下一次自动追踪</p><p className="mt-1 text-sm">{countdown}</p></div>
            {canManage && progress.settings.evaluator_mode === "core_agent" ? <label className="min-w-44 text-xs"><span className="sr-only">Progress Agent</span><select aria-label="Progress Agent" className="h-8 w-full rounded-md border border-input bg-background px-2 text-xs" disabled={updatingAgent} onChange={(event) => onAgentChange(event.target.value)} value={progress.settings.agent_instance_id ?? ""}><option value="">选择 Progress Agent</option>{activeAgents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}</select></label> : null}
            {canEvaluate ? <Button disabled={progress.settings.evaluator_mode === "core_agent" && !progress.settings.agent_instance_id} onClick={onRecalculate} size="sm"><Play aria-hidden="true" className="size-3.5" />立即评估</Button> : null}
          </div>
          {progress.settings.evaluator_mode === "core_agent" && !progress.settings.agent_instance_id ? <p className="text-xs text-amber-700 dark:text-amber-300">先选择一个可用的 Project Agent，评估才会进入 progress 类型 Session。</p> : null}
        </CardContent>
      </Card>

      <div className="space-y-4">
        <Card><CardContent className="flex items-center justify-between p-4"><div><p className="text-xs text-muted-foreground">今日待完成</p><p className="mt-1 text-3xl font-semibold tabular-nums">{todayCount}</p></div><span className="flex size-11 items-center justify-center rounded-full bg-primary/10 text-primary"><CheckCheck aria-hidden="true" className="size-5" /></span></CardContent></Card>
        <Card>
          <CardHeader className="pb-3"><CardTitle className="flex items-center justify-between text-base"><span className="flex items-center gap-2"><ShieldQuestion aria-hidden="true" className="size-4" />待确认建议</span><Badge>{pending.length}</Badge></CardTitle></CardHeader>
          <CardContent className="space-y-3">
            {pending.length ? <div className="max-h-52 space-y-2 overflow-y-auto pr-1">{pending.map((proposal) => <ProposalSummary key={proposal.proposal_id} proposal={proposal} />)}</div> : <p className="text-sm text-muted-foreground">当前没有需要人工批准的创建、改期或完成建议。</p>}
            {canManage && pending.length ? <div className="grid grid-cols-2 gap-2 border-t border-border pt-3"><Button onClick={() => onBatchReview(ids, "accepted")} size="sm"><CheckCheck aria-hidden="true" className="size-3.5" />全部批准</Button><Button onClick={() => onBatchReview(ids, "rejected")} size="sm" variant="outline"><X aria-hidden="true" className="size-3.5" />全部拒绝</Button></div> : null}
          </CardContent>
        </Card>
      </div>
    </aside>
  );
}

function EvaluationStages({ status, waitingReview }: Readonly<{ status?: string; waitingReview: boolean }>) {
  const stages = ["等待触发", "汇总上下文", "已排队", "Agent 评估", "应用工作状态", "等待确认"];
  const active = status === "running" ? 3 : status === "queued" ? 2 : status === "succeeded" ? (waitingReview ? 5 : 4) : status === "failed" ? 3 : 0;
  return <div aria-label="评估进度" className="grid grid-cols-3 gap-1.5 sm:grid-cols-6">{stages.map((stage, index) => <span className={cn("rounded-md border px-1.5 py-2 text-center text-[10px] leading-tight", index < active && "border-emerald-300 bg-emerald-50 text-emerald-700 dark:bg-emerald-950", index === active && "border-primary bg-primary/10 font-medium text-primary", index > active && "text-muted-foreground")} key={stage}>{stage}</span>)}</div>;
}

function SummaryList({ empty, icon: Icon, items, title, tone = "normal" }: Readonly<{ empty: string; icon: typeof CheckCheck; items: string[]; title: string; tone?: "danger" | "normal" }>) {
  return <div className={cn("rounded-lg border p-3", tone === "danger" && items.length && "border-red-300 bg-red-50/60 dark:border-red-900 dark:bg-red-950/30")}><p className="flex items-center gap-2 text-xs font-medium"><Icon aria-hidden="true" className="size-3.5" />{title}</p>{items.length ? <ul className="mt-2 space-y-1 text-xs text-muted-foreground">{items.slice(0, 5).map((item) => <li className="truncate" key={item}>• {item}</li>)}</ul> : <p className="mt-2 text-xs text-muted-foreground">{empty}</p>}</div>;
}

function ProposalSummary({ proposal }: Readonly<{ proposal: ProgressProposal }>) {
  const labels: Record<string, string> = { "milestone.create": "新建节点", "milestone.update": "调整节点", "milestone.complete": "节点完成", "task.create": "新建任务", "task.update": "调整任务", "task.complete": "任务完成" };
  return <div className={cn("rounded-lg border border-border p-2.5", proposal.proposal_type.endsWith(".complete") && "border-amber-300 bg-amber-50 dark:border-amber-800 dark:bg-amber-950/40")}><div className="flex items-center justify-between gap-2"><p className="truncate text-xs font-medium">{proposal.title}</p><span className="shrink-0 text-[10px] text-muted-foreground">{labels[proposal.proposal_type] ?? proposal.proposal_type}</span></div>{proposal.rationale ? <p className="mt-1 line-clamp-2 text-[11px] text-muted-foreground">{proposal.rationale}</p> : null}</div>;
}

function nextTrackingLabel(progress: ProgressAggregate, now: number): string {
  if (!progress.settings.auto_tracking_enabled) return "自动追踪未启用";
  if (!progress.tracking.last_evaluated_at) return "等待首次触发";
  const target = new Date(progress.tracking.last_evaluated_at).getTime() + progress.settings.min_interval_seconds * 1_000;
  const seconds = Math.max(0, Math.ceil((target - now) / 1_000));
  if (!seconds) return "已可触发";
  const hours = Math.floor(seconds / 3_600);
  const minutes = Math.floor(seconds % 3_600 / 60);
  const rest = seconds % 60;
  return `${hours ? `${hours} 小时 ` : ""}${minutes ? `${minutes} 分 ` : ""}${rest} 秒`;
}
