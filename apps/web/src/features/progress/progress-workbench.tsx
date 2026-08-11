"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CalendarDays, ListTodo } from "lucide-react";
import { useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import { agentApi } from "@/features/agent/agent-api";
import { apiClient } from "@/lib/api-client";

import { ProgressCalendar } from "./progress-calendar";
import { ProgressInfoRail } from "./progress-info-rail";
import { ProgressItemDrawer, type DrawerSelection, type ProgressItemDraft } from "./progress-item-drawer";
import { ProgressTodoStream } from "./progress-todo-stream";
import type { ProgressAggregate, ProgressMilestone, ProgressProposal, ProgressTask } from "./types";

type View = "calendar" | "todo";
type ReviewDecision = "accepted" | "rejected";

export function ProgressWorkbench({ canEvaluate, canManage, progress, projectId }: Readonly<{
  canEvaluate: boolean;
  canManage: boolean;
  progress: ProgressAggregate;
  projectId: string;
}>) {
  const queryClient = useQueryClient();
  const [view, setView] = useState<View>("calendar");
  const [selection, setSelection] = useState<DrawerSelection | null>(null);
  const [feedback, setFeedback] = useState<string | null>(null);
  const root = `/projects/${encodeURIComponent(projectId)}/progress`;
  const progressQueryKey = ["progress", projectId] as const;
  const agents = useQuery({
    enabled: canManage,
    queryFn: () => agentApi.listInstances(projectId),
    queryKey: ["agent-instances", projectId],
  });

  async function refresh() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["progress", projectId] }),
      queryClient.invalidateQueries({ queryKey: ["project-home", projectId] }),
      queryClient.invalidateQueries({ queryKey: ["progress-evaluations", projectId] }),
    ]);
  }

  async function optimisticallyUpdate(update: (current: ProgressAggregate) => ProgressAggregate) {
    await queryClient.cancelQueries({ queryKey: progressQueryKey });
    const previous = queryClient.getQueryData<ProgressAggregate>(progressQueryKey);
    if (previous) queryClient.setQueryData(progressQueryKey, update(previous));
    return { previous };
  }

  function restoreOptimisticUpdate(context: unknown) {
    const previous = context && typeof context === "object" && "previous" in context
      ? (context as { previous?: ProgressAggregate }).previous
      : undefined;
    if (previous) queryClient.setQueryData(progressQueryKey, previous);
  }

  const save = useMutation({
    mutationFn: async (draft: ProgressItemDraft) => {
      const isTask = draft.kind === "task";
      const body = isTask
        ? { description: draft.description, due_at: draft.endAt?.toISOString(), start_at: draft.startAt?.toISOString(), title: draft.title }
        : { description: draft.description, target_at: draft.startAt?.toISOString(), target_has_time: draft.targetHasTime, title: draft.title };
      if (draft.id) {
        return apiClient.request(`${root}/${isTask ? "tasks" : "milestones"}/${encodeURIComponent(draft.id)}`, { body, method: "PATCH" });
      }
      return apiClient.request(`${root}/${isTask ? "tasks" : "milestones"}`, { body, method: "POST" });
    },
    onError: () => setFeedback("保存失败，当前安排没有被修改。"),
    onSuccess: async () => {
      setSelection(null);
      setFeedback("安排已保存。" );
      await refresh();
    },
  });

  const updateTask = useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Record<string, unknown> }) => apiClient.request(`${root}/tasks/${encodeURIComponent(id)}`, { body: patch, method: "PATCH" }),
    onError: (_error, _input, context) => { restoreOptimisticUpdate(context); setFeedback("任务调整失败，已恢复原来的状态。" ); },
    onMutate: ({ id, patch }) => optimisticallyUpdate((current) => ({
      ...current,
      tasks: current.tasks.map((task) => task.task_id === id ? { ...task, ...patch } as ProgressTask : task),
    })),
    onSettled: () => { void refresh(); },
  });
  const updateMilestone = useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: Record<string, unknown> }) => apiClient.request(`${root}/milestones/${encodeURIComponent(id)}`, { body: patch, method: "PATCH" }),
    onError: (_error, _input, context) => { restoreOptimisticUpdate(context); setFeedback("关键节点调整失败，已恢复原来的状态。" ); },
    onMutate: ({ id, patch }) => optimisticallyUpdate((current) => ({
      ...current,
      milestones: current.milestones.map((milestone) => milestone.milestone_id === id ? { ...milestone, ...patch } as ProgressMilestone : milestone),
    })),
    onSettled: () => { void refresh(); },
  });
  const review = useMutation({
    mutationFn: ({ decision, id }: { decision: ReviewDecision; id: string }) => apiClient.request(`${root}/proposals/${encodeURIComponent(id)}/review`, { body: { decision }, method: "POST" }),
    onError: (_error, _input, context) => { restoreOptimisticUpdate(context); setFeedback("无法处理这条 AI 建议，已恢复待确认状态。" ); },
    onMutate: ({ decision, id }) => optimisticallyUpdate((current) => applyOptimisticReviews(current, [id], decision)),
    onSettled: () => { void refresh(); },
  });
  const batchReview = useMutation({
    mutationFn: ({ decision, ids }: { decision: ReviewDecision; ids: string[] }) => apiClient.request(`${root}/proposals/batch-review`, { body: { decision, proposal_ids: ids }, method: "POST" }),
    onError: (_error, _input, context) => { restoreOptimisticUpdate(context); setFeedback("批量处理未完成，已恢复全部待确认建议。" ); },
    onMutate: ({ decision, ids }) => optimisticallyUpdate((current) => applyOptimisticReviews(current, ids, decision)),
    onSettled: () => { void refresh(); },
  });
  const recalculate = useMutation({
    mutationFn: () => apiClient.request(`${root}/recalculate`, { body: { force: false, trigger_kind: "manual" }, method: "POST" }),
    onError: () => setFeedback("无法触发进度评估，请稍后重试。"),
    onSuccess: async () => {
      setFeedback("进度评估已进入队列。" );
      await refresh();
    },
  });
  const updateAgent = useMutation({
    mutationFn: (agentInstanceId: string) => apiClient.request(`${root}/settings`, {
      body: {
        agent_instance_id: agentInstanceId || undefined,
        auto_task_changes: progress.settings.auto_task_changes,
        auto_tracking_enabled: progress.settings.auto_tracking_enabled,
        cron_enabled: progress.settings.cron_enabled,
        cron_schedule: progress.settings.cron_schedule,
        debounce_seconds: progress.settings.debounce_seconds,
        event_triggers_enabled: progress.settings.event_triggers_enabled,
        min_interval_seconds: progress.settings.min_interval_seconds,
      },
      method: "PATCH",
    }),
    onError: () => setFeedback("无法保存 Progress Agent，请确认该 Agent 仍处于可用状态。"),
    onSuccess: async () => {
      setFeedback("Progress Agent 已更新。" );
      await refresh();
    },
  });

  const pending = useMemo(() => progress.proposals.filter((proposal) => proposal.status === "pending"), [progress.proposals]);
  const activeAgents = useMemo(() => (agents.data?.items ?? [])
    .filter((item) => item.status === "active" && item.grant.status === "active")
    .map((item) => ({ id: item.agent_instance_id, name: item.display_name })), [agents.data?.items]);
  const completionByTarget = useMemo(() => {
    const values = new Map<string, ProgressProposal>();
    for (const proposal of pending) {
      if (proposal.target_id && proposal.proposal_type.endsWith(".complete")) values.set(proposal.target_id, proposal);
    }
    return values;
  }, [pending]);

  function openTask(task: ProgressTask) { setSelection({ kind: "task", task }); }
  function openMilestone(milestone: ProgressMilestone) { setSelection({ kind: "milestone", milestone }); }
  function createAt(at: Date, kind: "task" | "milestone" = "task", targetHasTime = true) {
    if (!canManage) return;
    setSelection({ at, kind, targetHasTime });
  }
  function toggleTask(task: ProgressTask) {
    if (!canManage) return;
    updateTask.mutate({ id: task.task_id, patch: { status: task.status === "done" ? task.work_state : "done" } });
  }
  function toggleMilestone(milestone: ProgressMilestone) {
    if (!canManage) return;
    updateMilestone.mutate({ id: milestone.milestone_id, patch: { status: milestone.status === "completed" ? "planned" : "completed" } });
  }

  const shared = {
    canManage,
    completionByTarget,
    milestones: progress.milestones,
    onCreate: createAt,
    onOpenMilestone: openMilestone,
    onOpenTask: openTask,
    onReview: (proposal: ProgressProposal, decision: ReviewDecision) => review.mutate({ decision, id: proposal.proposal_id }),
    onToggleMilestone: toggleMilestone,
    onToggleTask: toggleTask,
    tasks: progress.tasks,
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">双击时间位置新建；拖动调整，任务上下边缘可拉伸，所有操作吸附到 15 分钟。</p>
        <div aria-label="Progress 视图" className="flex rounded-lg border border-border bg-muted/40 p-1" role="group">
          <Button aria-pressed={view === "calendar"} className="h-8" onClick={() => setView("calendar")} size="sm" variant={view === "calendar" ? "secondary" : "ghost"}><CalendarDays aria-hidden="true" className="size-4" />日历</Button>
          <Button aria-pressed={view === "todo"} className="h-8" onClick={() => setView("todo")} size="sm" variant={view === "todo" ? "secondary" : "ghost"}><ListTodo aria-hidden="true" className="size-4" />TODO</Button>
        </div>
      </div>

      {feedback ? <p aria-live="polite" className="rounded-lg border border-border bg-muted/30 px-3 py-2 text-sm" role="status">{feedback}</p> : null}

      {view === "calendar" ? (
        <div className="space-y-4">
          <ProgressCalendar
            {...shared}
            onMoveMilestone={(id, at) => updateMilestone.mutate({ id, patch: { target_at: at.toISOString(), target_has_time: true } })}
            onMoveTask={(id, startAt, dueAt) => updateTask.mutate({ id, patch: { due_at: dueAt.toISOString(), start_at: startAt.toISOString() } })}
          />
          <ProgressInfoRail
            activeAgents={activeAgents}
            canEvaluate={canEvaluate}
            canManage={canManage}
            layout="horizontal"
            onAgentChange={(id) => updateAgent.mutate(id)}
            onBatchReview={(ids, decision) => batchReview.mutate({ decision, ids })}
            onRecalculate={() => recalculate.mutate()}
            pending={pending}
            progress={progress}
            updatingAgent={updateAgent.isPending}
          />
        </div>
      ) : (
        <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]">
          <ProgressTodoStream
            {...shared}
            onMoveMilestone={(id, at) => updateMilestone.mutate({ id, patch: { target_at: at.toISOString() } })}
            onMoveTask={(id, startAt, dueAt) => updateTask.mutate({ id, patch: { due_at: dueAt.toISOString(), start_at: startAt.toISOString() } })}
          />
          <ProgressInfoRail
            activeAgents={activeAgents}
            canEvaluate={canEvaluate}
            canManage={canManage}
            layout="vertical"
            onAgentChange={(id) => updateAgent.mutate(id)}
            onBatchReview={(ids, decision) => batchReview.mutate({ decision, ids })}
            onRecalculate={() => recalculate.mutate()}
            pending={pending}
            progress={progress}
            updatingAgent={updateAgent.isPending}
          />
        </div>
      )}

      <ProgressItemDrawer
        busy={save.isPending}
        onClose={() => setSelection(null)}
        onSave={(draft) => save.mutate(draft)}
        selection={selection}
      />
    </div>
  );
}

function applyOptimisticReviews(current: ProgressAggregate, ids: string[], decision: ReviewDecision): ProgressAggregate {
  const selected = current.proposals.filter((proposal) => ids.includes(proposal.proposal_id));
  const completedTasks = new Set(selected.filter((proposal) => decision === "accepted" && proposal.proposal_type === "task.complete" && proposal.target_id).map((proposal) => proposal.target_id!));
  const completedMilestones = new Set(selected.filter((proposal) => decision === "accepted" && proposal.proposal_type === "milestone.complete" && proposal.target_id).map((proposal) => proposal.target_id!));
  return {
    ...current,
    milestones: current.milestones.map((milestone) => completedMilestones.has(milestone.milestone_id) ? { ...milestone, status: "completed" } : milestone),
    proposals: current.proposals.filter((proposal) => !ids.includes(proposal.proposal_id)),
    tasks: current.tasks.map((task) => completedTasks.has(task.task_id) ? { ...task, status: "done" } : task),
  };
}
