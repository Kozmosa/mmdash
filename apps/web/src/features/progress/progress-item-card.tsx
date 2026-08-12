"use client";

import { Check, Circle, CircleCheckBig, CircleDotDashed, LoaderCircle, OctagonAlert, Trash2, X } from "lucide-react";

import { cn } from "@/lib/cn";

import { formatTime } from "./calendar-time";
import type { ProgressMilestone, ProgressProposal, ProgressTask } from "./types";

type Props = {
  canManage: boolean;
  className?: string;
  compact?: boolean;
  item: ProgressTask | ProgressMilestone;
  kind: "task" | "milestone";
  onDelete: () => void;
  onOpen: () => void;
  onReview: (proposal: ProgressProposal, decision: "accepted" | "rejected") => void;
  onToggle: () => void;
  pendingCompletion?: ProgressProposal;
};

export function ProgressItemCard({ canManage, className, compact = false, item, kind, onDelete, onOpen, onReview, onToggle, pendingCompletion }: Readonly<Props>) {
  const completed = kind === "task" ? (item as ProgressTask).status === "done" : (item as ProgressMilestone).status === "completed";
  const task = kind === "task" ? item as ProgressTask : null;
  const start = task?.start_at ? new Date(task.start_at) : (item as ProgressMilestone).target_at ? new Date((item as ProgressMilestone).target_at!) : null;
  const end = task?.due_at ? new Date(task.due_at) : null;

  return (
    <article
      className={cn(
        "group relative select-text overflow-hidden rounded-lg border border-sky-300/70 bg-sky-100/90 text-sky-950 shadow-sm transition hover:shadow-md dark:border-sky-800 dark:bg-sky-950/70 dark:text-sky-100",
        pendingCompletion && "border-amber-400 bg-amber-100 text-amber-950 dark:border-amber-700 dark:bg-amber-950/70 dark:text-amber-100",
        completed && "opacity-50 saturate-50",
        compact ? "px-2 py-1" : "p-3",
        className,
      )}
      data-progress-kind={kind}
      data-progress-status={pendingCompletion ? "ai-complete" : completed ? "completed" : task?.status ?? item.status}
      onClick={onOpen}
      onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); onOpen(); } }}
      role="button"
      tabIndex={0}
    >
      <div className="flex min-w-0 items-start gap-2">
        <CompletionControl canManage={canManage} completed={completed} label={item.title} onReview={onReview} onToggle={onToggle} proposal={pendingCompletion} />
        <div className="min-w-0 flex-1">
          <p className={cn("truncate font-medium", compact ? "text-xs" : "text-sm", completed && "line-through")}>{item.title}</p>
          {!compact && item.description ? <p className="mt-1 line-clamp-2 text-xs opacity-70">{item.description}</p> : null}
          {start ? <p className={cn("truncate opacity-70", compact ? "text-[10px]" : "mt-1 text-xs")}>{formatTime(start)}{end ? `–${formatTime(end)}` : ""}</p> : null}
        </div>
        {task ? <WorkState status={task.work_state ?? (task.status === "done" ? "todo" : task.status)} compact={compact} /> : null}
        {canManage ? <button aria-label={`删除${kind === "task" ? "任务" : "关键节点"} ${item.title}`} className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition hover:bg-destructive/10 hover:text-destructive focus-visible:opacity-100 group-hover:opacity-100" onClick={(event) => { event.stopPropagation(); onDelete(); }} onPointerDown={(event) => event.stopPropagation()} type="button"><Trash2 aria-hidden="true" className="size-3.5" /></button> : null}
      </div>
      {pendingCompletion && !compact ? <p className="mt-2 text-[11px] font-medium text-amber-800 dark:text-amber-200">AI 判断已完成，等待你的确认</p> : null}
    </article>
  );
}

function CompletionControl({ canManage, completed, label, onReview, onToggle, proposal }: Readonly<{
  canManage: boolean;
  completed: boolean;
  label: string;
  onReview: Props["onReview"];
  onToggle: () => void;
  proposal?: ProgressProposal;
}>) {
  if (proposal) {
    return <span className="flex shrink-0 gap-0.5" onClick={(event) => event.stopPropagation()} onPointerDown={(event) => event.stopPropagation()}>
      <button aria-label={`认可 ${label} 已完成`} className="rounded-full bg-amber-500 p-0.5 text-white hover:bg-amber-600 disabled:opacity-50" disabled={!canManage} onClick={() => onReview(proposal, "accepted")} type="button"><Check aria-hidden="true" className="size-3.5" /></button>
      <button aria-label={`不认可 ${label} 已完成`} className="rounded-full border border-amber-500 bg-background/60 p-0.5 text-amber-700 hover:bg-background disabled:opacity-50" disabled={!canManage} onClick={() => onReview(proposal, "rejected")} type="button"><X aria-hidden="true" className="size-3.5" /></button>
    </span>;
  }
  return <button aria-label={completed ? `将 ${label} 标为未完成` : `将 ${label} 标为完成`} className="shrink-0 rounded-full disabled:opacity-60" disabled={!canManage} onClick={(event) => { event.stopPropagation(); onToggle(); }} onPointerDown={(event) => event.stopPropagation()} type="button">
    {completed ? <CircleCheckBig aria-hidden="true" className="size-4 fill-current" /> : <Circle aria-hidden="true" className="size-4" />}
  </button>;
}

function WorkState({ status }: Readonly<{ compact: boolean; status: ProgressTask["status"] }>) {
  const values = {
    todo: { icon: CircleDotDashed, label: "待办", style: "text-slate-500" },
    in_progress: { icon: LoaderCircle, label: "进行中", style: "text-blue-600 dark:text-blue-300" },
    blocked: { icon: OctagonAlert, label: "阻塞", style: "text-red-600 dark:text-red-300" },
    done: { icon: Check, label: "完成", style: "text-emerald-600" },
  } as const;
  const value = values[status];
  const Icon = value.icon;
  return <span className={cn("flex shrink-0 items-center gap-1 text-[10px] font-medium", value.style)} title={value.label}><Icon aria-hidden="true" className="size-3" />{value.label}</span>;
}
