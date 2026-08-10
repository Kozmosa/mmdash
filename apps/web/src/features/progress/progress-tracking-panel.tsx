"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Bot, CheckCircle2, Clock3, History, RotateCcw, Save, Settings2, ShieldCheck } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { agentApi } from "@/features/agent/agent-api";
import { apiClient } from "@/lib/api-client";

import type { ProgressAggregate, ProgressEvaluation, ProgressEvaluationPage, ProgressSettings } from "./types";

type SettingsForm = Pick<
  ProgressSettings,
  | "agent_instance_id"
  | "auto_task_changes"
  | "auto_tracking_enabled"
  | "cron_enabled"
  | "cron_schedule"
  | "debounce_seconds"
  | "evaluator_mode"
  | "event_triggers_enabled"
  | "min_interval_seconds"
>;

export function ProgressTrackingPanel({
  canEvaluate,
  canManage,
  progress,
  projectId,
}: Readonly<{
  canEvaluate: boolean;
  canManage: boolean;
  progress: ProgressAggregate;
  projectId: string;
}>) {
  const queryClient = useQueryClient();
  const [feedback, setFeedback] = useState<string | null>(null);
  const [overrideStage, setOverrideStage] = useState(progress.tracking.effective_stage);
  const [overrideSummary, setOverrideSummary] = useState("");
  const [overrideNote, setOverrideNote] = useState("");
  const [settings, setSettings] = useState<SettingsForm>(() => settingsForm(progress.settings));
  const history = useQuery({
    queryFn: () => apiClient.request<ProgressEvaluationPage>(`${progressPath(projectId)}/evaluations?limit=20`),
    queryKey: ["progress-evaluations", projectId],
  });
  const agents = useQuery({
    enabled: canManage,
    queryFn: () => agentApi.listInstances(projectId),
    queryKey: ["agent-instances", projectId],
  });

  useEffect(() => setSettings(settingsForm(progress.settings)), [progress.settings]);
  useEffect(() => {
    if (!progress.tracking.stage_overridden) setOverrideStage(progress.tracking.effective_stage);
  }, [progress.tracking.effective_stage, progress.tracking.stage_overridden]);

  async function refreshTracking() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["progress", projectId] }),
      queryClient.invalidateQueries({ queryKey: ["progress-evaluations", projectId] }),
      queryClient.invalidateQueries({ queryKey: ["project-home", projectId] }),
    ]);
  }

  const recalculate = useMutation({
    mutationFn: () => apiClient.request(`${progressPath(projectId)}/recalculate`, { body: { force: false, trigger_kind: "manual" }, method: "POST" }),
    onError: () => setFeedback("无法安排进度评估，请稍后重试。"),
    onSuccess: async () => {
      setFeedback("进度评估已进入防抖调度队列。相同输入会自动合并。" );
      await refreshTracking();
    },
  });
  const retry = useMutation({
    mutationFn: (evaluationId: string) => apiClient.request(`${progressPath(projectId)}/evaluations/${encodeURIComponent(evaluationId)}/retry`, { method: "POST" }),
    onError: () => setFeedback("无法重试该评估，请确认它仍处于失败状态。"),
    onSuccess: async () => {
      setFeedback("失败评估已重新排队。" );
      await refreshTracking();
    },
  });
  const setOverride = useMutation({
    mutationFn: () => apiClient.request(`${progressPath(projectId)}/stage-override`, { body: { note: overrideNote.trim() || undefined, stage: overrideStage.trim(), summary: overrideSummary.trim() || undefined }, method: "POST" }),
    onError: () => setFeedback("无法保存阶段覆盖。"),
    onSuccess: async () => {
      setFeedback("人工阶段覆盖已保存，后续评估会保留并读取它。" );
      setOverrideNote("");
      setOverrideSummary("");
      await refreshTracking();
    },
  });
  const clearOverride = useMutation({
    mutationFn: () => apiClient.request(`${progressPath(projectId)}/stage-override`, { method: "DELETE" }),
    onError: () => setFeedback("无法清除阶段覆盖。"),
    onSuccess: async () => {
      setFeedback("已恢复为自动检测阶段。" );
      await refreshTracking();
    },
  });
  const updateSettings = useMutation({
    mutationFn: () => apiClient.request(`${progressPath(projectId)}/settings`, { body: settingsRequest(settings), method: "PATCH" }),
    onError: () => setFeedback("无法保存自动跟踪设置，请检查 Agent、Cron 和时间参数。"),
    onSuccess: async () => {
      setFeedback("自动跟踪设置已保存；Hermes Cron 状态会由 Core 异步协调。" );
      await refreshTracking();
    },
  });

  const state = progress.tracking;
  const evaluations = history.data?.items ?? (progress.latest_evaluation ? [progress.latest_evaluation] : []);
  const activeAgents = (agents.data?.items ?? []).filter((item) => item.status === "active" && item.grant.status === "active");

  return (
    <section className="space-y-4" aria-labelledby="automatic-progress-title">
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(20rem,0.65fr)]">
        <Card>
          <CardHeader className="gap-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <CardTitle className="flex items-center gap-2 text-base" id="automatic-progress-title"><Bot aria-hidden="true" className="size-4" />自动进度跟踪</CardTitle>
              <div className="flex flex-wrap gap-2">
                <Badge>{state.effective_stage || "尚未评估"}</Badge>
                {state.stage_overridden ? <Badge className="gap-1"><ShieldCheck aria-hidden="true" className="size-3" />人工覆盖</Badge> : null}
                {progress.latest_evaluation ? <Badge>{statusLabel(progress.latest_evaluation.status)}</Badge> : null}
              </div>
            </div>
            <p className="text-sm text-muted-foreground">{state.summary || "领域事件、Hermes Cron 或人工操作会生成版本化评估。"}</p>
          </CardHeader>
          <CardContent className="space-y-4">
            <dl className="grid gap-3 text-sm sm:grid-cols-3">
              <SummaryItem label="自动检测" value={state.detected_stage || "—"} />
              <SummaryItem label="当前生效" value={state.effective_stage || "—"} />
              <SummaryItem label="最近评估" value={state.last_evaluated_at ? new Date(state.last_evaluated_at).toLocaleString() : "尚无"} />
            </dl>
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
              <ListSummary icon={CheckCircle2} items={state.completed_items} title="已完成" />
              <ListSummary icon={Clock3} items={state.in_progress_items} title="进行中" />
              <ListSummary icon={AlertTriangle} items={state.blockers} title="阻塞" />
              <ListSummary icon={History} items={state.changes_since_last} title="近期变化" />
            </div>
            <div className="flex flex-wrap items-center gap-3">
              {canEvaluate ? <Button disabled={recalculate.isPending} onClick={() => { setFeedback(null); recalculate.mutate(); }}><RotateCcw aria-hidden="true" className="size-4" />{recalculate.isPending ? "正在安排…" : "重新评估进度"}</Button> : null}
              <span className="text-xs text-muted-foreground">输入版本相同的排队、运行或成功评估不会重复执行。</span>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle className="text-base">阶段覆盖</CardTitle></CardHeader>
          <CardContent className="space-y-3">
            <p className="text-sm text-muted-foreground">自动判断始终保留在历史中；人工覆盖只改变当前生效阶段，并进入后续评估输入。</p>
            {canManage ? <>
              <Input aria-label="覆盖阶段" maxLength={100} onChange={(event) => setOverrideStage(event.target.value)} placeholder="例如：模型验证" value={overrideStage} />
              <Input aria-label="覆盖摘要" maxLength={2000} onChange={(event) => setOverrideSummary(event.target.value)} placeholder="可选：替换首页与 Progress 摘要" value={overrideSummary} />
              <Input aria-label="覆盖说明" maxLength={2000} onChange={(event) => setOverrideNote(event.target.value)} placeholder="可选：记录人工判断依据" value={overrideNote} />
              <div className="flex flex-wrap gap-2">
                <Button disabled={!overrideStage.trim() || setOverride.isPending} onClick={() => { setFeedback(null); setOverride.mutate(); }} size="sm"><Save aria-hidden="true" className="size-4" />保存覆盖</Button>
                {state.stage_overridden ? <Button disabled={clearOverride.isPending} onClick={() => { setFeedback(null); clearOverride.mutate(); }} size="sm" variant="outline">清除覆盖</Button> : null}
              </div>
            </> : <p className="text-sm">当前阶段：{state.effective_stage || "尚未评估"}</p>}
          </CardContent>
        </Card>
      </div>

      {feedback ? <p aria-live="polite" className="text-sm text-muted-foreground" role="status">{feedback}</p> : null}

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.25fr)_minmax(22rem,0.75fr)]">
        <Card>
          <CardHeader><CardTitle className="flex items-center gap-2 text-base"><History aria-hidden="true" className="size-4" />评估历史</CardTitle></CardHeader>
          <CardContent className="space-y-3">
            {history.isError ? <p className="text-sm text-destructive">无法读取评估历史。</p> : null}
            {!evaluations.length && !history.isLoading ? <p className="text-sm text-muted-foreground">尚无评估。首次手动重算或启用自动跟踪后会显示输入、触发来源和输出。</p> : null}
            {evaluations.map((evaluation) => <EvaluationDetail canRetry={canManage} evaluation={evaluation} key={evaluation.evaluation_id} onRetry={() => retry.mutate(evaluation.evaluation_id)} retrying={retry.isPending} />)}
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle className="flex items-center gap-2 text-base"><Settings2 aria-hidden="true" className="size-4" />自动跟踪设置</CardTitle></CardHeader>
          <CardContent>
            {canManage ? <form className="space-y-4" onSubmit={(event) => submitSettings(event, settings, setFeedback, updateSettings.mutate)}>
              <Toggle checked={settings.auto_tracking_enabled} label="启用自动跟踪" onChange={(checked) => setSettings((current) => ({ ...current, auto_tracking_enabled: checked }))} />
              <Toggle checked={settings.event_triggers_enabled} label="消费领域事件" onChange={(checked) => setSettings((current) => ({ ...current, event_triggers_enabled: checked }))} />
              <Toggle checked={settings.cron_enabled} label="启用 Hermes Cron" onChange={(checked) => setSettings((current) => ({ ...current, cron_enabled: checked }))} />
              <Toggle checked={settings.auto_task_changes} label="允许 Agent 自动调整普通 TODO" onChange={(checked) => setSettings((current) => ({ ...current, auto_task_changes: checked }))} />
              <label className="block space-y-2 text-sm"><span>Progress Agent</span><select aria-label="Progress Agent" className={selectClass} onChange={(event) => setSettings((current) => ({ ...current, agent_instance_id: event.target.value || undefined }))} value={settings.agent_instance_id ?? ""}><option value="">未选择</option>{activeAgents.map((item) => <option key={item.agent_instance_id} value={item.agent_instance_id}>{item.display_name}</option>)}</select></label>
              <label className="block space-y-2 text-sm"><span>Cron（5 字段）</span><Input aria-label="Progress Cron" maxLength={100} onChange={(event) => setSettings((current) => ({ ...current, cron_schedule: event.target.value }))} value={settings.cron_schedule} /></label>
              <div className="grid gap-3 sm:grid-cols-2"><NumberField label="防抖秒数" max={3600} value={settings.debounce_seconds} onChange={(value) => setSettings((current) => ({ ...current, debounce_seconds: value }))} /><NumberField label="最短间隔秒数" max={86400} value={settings.min_interval_seconds} onChange={(value) => setSettings((current) => ({ ...current, min_interval_seconds: value }))} /></div>
              <div className="rounded-lg border border-border p-3 text-xs text-muted-foreground">评估器：{progress.settings.evaluator_mode} · Cron 同步：{progress.settings.cron_sync_status}{progress.settings.cron_error_code ? ` · ${progress.settings.cron_error_code}` : ""}{progress.settings.cron_synced_at ? ` · ${new Date(progress.settings.cron_synced_at).toLocaleString()}` : ""}</div>
              <Button disabled={updateSettings.isPending} type="submit"><Save aria-hidden="true" className="size-4" />{updateSettings.isPending ? "正在保存…" : "保存设置"}</Button>
            </form> : <div className="space-y-2 text-sm text-muted-foreground"><p>自动跟踪：{progress.settings.auto_tracking_enabled ? "已启用" : "未启用"}</p><p>事件触发：{progress.settings.event_triggers_enabled ? "已启用" : "未启用"}</p><p>Cron：{progress.settings.cron_enabled ? progress.settings.cron_schedule : "未启用"}</p></div>}
          </CardContent>
        </Card>
      </div>
    </section>
  );
}

function EvaluationDetail({ canRetry, evaluation, onRetry, retrying }: Readonly<{ canRetry: boolean; evaluation: ProgressEvaluation; onRetry: () => void; retrying: boolean }>) {
  return <details className="rounded-lg border border-border p-3"><summary className="cursor-pointer list-none"><div className="flex flex-wrap items-center justify-between gap-2"><div><p className="text-sm font-medium">{evaluation.detected_stage || "待评估"} · {new Date(evaluation.created_at).toLocaleString()}</p><p className="mt-1 text-xs text-muted-foreground">{evaluation.trigger_kind} · {evaluation.evaluator_mode} · 输入 {evaluation.input_version.slice(0, 12)}</p></div><Badge>{statusLabel(evaluation.status)}</Badge></div></summary><div className="mt-4 space-y-4 border-t border-border pt-4 text-sm"><p>{evaluation.summary || evaluation.error_message || "评估仍在处理中。"}</p>{evaluation.error_code ? <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-3"><p className="font-medium text-destructive">{evaluation.error_code}</p><p className="mt-1 text-xs text-muted-foreground">已尝试 {evaluation.attempts} 次。可由有管理权限的人重新排队。</p>{canRetry && evaluation.status === "failed" ? <Button className="mt-3" disabled={retrying} onClick={onRetry} size="sm" variant="outline">重试评估</Button> : null}</div> : null}<div className="grid gap-3 md:grid-cols-2"><DetailList title="触发来源" values={evaluation.triggers.map((trigger) => `${trigger.trigger_type}${trigger.source_event_type ? ` · ${trigger.source_event_type}` : ""}`)} /><DetailList title="风险" values={evaluation.risks.map((risk) => `${risk.severity.toUpperCase()} · ${risk.title}`)} /></div><details><summary className="cursor-pointer text-xs font-medium text-muted-foreground">查看版本化输入与来源标识</summary><pre className="mt-2 max-h-80 overflow-auto rounded-lg bg-muted p-3 text-xs">{JSON.stringify({ agent_instance_id: evaluation.agent_instance_id, agent_run_id: evaluation.agent_run_id, agent_session_id: evaluation.agent_session_id, input_snapshot: evaluation.input_snapshot, source_event_ids: evaluation.source_event_ids }, null, 2)}</pre></details></div></details>;
}

function submitSettings(event: FormEvent<HTMLFormElement>, settings: SettingsForm, feedback: (value: string | null) => void, submit: () => void) {
  event.preventDefault();
  if (settings.cron_enabled && !settings.agent_instance_id) {
    feedback("启用 Hermes Cron 前请选择一个处于 active 状态的 Project Agent。" );
    return;
  }
  if (settings.auto_tracking_enabled && !settings.agent_instance_id && settings.evaluator_mode !== "mock") {
    feedback("启用自动跟踪前请选择一个处于 active 状态的 Project Agent。" );
    return;
  }
  if (settings.cron_schedule.trim().split(/\s+/).length !== 5) {
    feedback("Cron 必须使用标准 5 字段表达式。" );
    return;
  }
  feedback(null);
  submit();
}

function settingsForm(value: ProgressSettings): SettingsForm {
  return { agent_instance_id: value.agent_instance_id, auto_task_changes: value.auto_task_changes, auto_tracking_enabled: value.auto_tracking_enabled, cron_enabled: value.cron_enabled, cron_schedule: value.cron_schedule, debounce_seconds: value.debounce_seconds, evaluator_mode: value.evaluator_mode, event_triggers_enabled: value.event_triggers_enabled, min_interval_seconds: value.min_interval_seconds };
}

function settingsRequest(value: SettingsForm) {
  return {
    agent_instance_id: value.agent_instance_id || undefined,
    auto_task_changes: value.auto_task_changes,
    auto_tracking_enabled: value.auto_tracking_enabled,
    cron_enabled: value.cron_enabled,
    cron_schedule: value.cron_schedule,
    debounce_seconds: value.debounce_seconds,
    event_triggers_enabled: value.event_triggers_enabled,
    min_interval_seconds: value.min_interval_seconds,
  };
}

function progressPath(projectId: string) { return `/projects/${encodeURIComponent(projectId)}/progress`; }
function statusLabel(value: ProgressEvaluation["status"]) { return { failed: "失败", queued: "排队中", running: "评估中", succeeded: "已完成" }[value]; }
function SummaryItem({ label, value }: Readonly<{ label: string; value: string }>) { return <div className="rounded-lg border border-border p-3"><dt className="text-xs text-muted-foreground">{label}</dt><dd className="mt-1 font-medium">{value}</dd></div>; }
function ListSummary({ icon: Icon, items, title }: Readonly<{ icon: typeof History; items: string[]; title: string }>) { return <div className="rounded-lg border border-border p-3"><p className="flex items-center gap-2 text-xs font-medium"><Icon aria-hidden="true" className="size-3.5" />{title}<Badge className="ml-auto">{items.length}</Badge></p><p className="mt-2 line-clamp-2 text-xs text-muted-foreground">{items.slice(0, 3).join("；") || "暂无"}</p></div>; }
function DetailList({ title, values }: Readonly<{ title: string; values: string[] }>) { return <div><p className="text-xs font-medium">{title}</p>{values.length ? <ul className="mt-2 list-disc space-y-1 pl-5 text-xs text-muted-foreground">{values.map((value, index) => <li key={`${value}-${index}`}>{value}</li>)}</ul> : <p className="mt-2 text-xs text-muted-foreground">暂无</p>}</div>; }
function Toggle({ checked, label, onChange }: Readonly<{ checked: boolean; label: string; onChange: (value: boolean) => void }>) { return <label className="flex items-center justify-between gap-3 text-sm"><span>{label}</span><input aria-label={label} checked={checked} onChange={(event) => onChange(event.target.checked)} type="checkbox" /></label>; }
function NumberField({ label, max, onChange, value }: Readonly<{ label: string; max: number; onChange: (value: number) => void; value: number }>) { return <label className="block space-y-2 text-sm"><span>{label}</span><Input aria-label={label} max={max} min={0} onChange={(event) => onChange(Number(event.target.value))} type="number" value={value} /></label>; }

const selectClass = "flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50";
