"use client";

import { CalendarRange, ChevronLeft, ChevronRight, Rows3 } from "lucide-react";
import { useMemo, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/cn";

import { addLocalDays, dateAtMinutes, formatShortDay, localDayKey, minutesFromDayStart, PERIODS, periodForDate, startOfLocalDay } from "./calendar-time";
import { beginNativeProgressDrag, endNativeProgressDrag } from "./progress-drag-preview";
import { ProgressItemCard } from "./progress-item-card";
import type { ProgressMilestone, ProgressProposal, ProgressTask } from "./types";

type Props = {
  canManage: boolean;
  completionByTarget: Map<string, ProgressProposal>;
  milestones: ProgressMilestone[];
  tasks: ProgressTask[];
  onCreate: (at: Date, kind?: "task" | "milestone", targetHasTime?: boolean) => void;
  onMoveMilestone: (id: string, at: Date) => void;
  onMoveTask: (id: string, startAt: Date, dueAt: Date) => void;
  onOpenMilestone: (milestone: ProgressMilestone) => void;
  onOpenTask: (task: ProgressTask) => void;
  onReview: (proposal: ProgressProposal, decision: "accepted" | "rejected") => void;
  onToggleMilestone: (milestone: ProgressMilestone) => void;
  onToggleTask: (task: ProgressTask) => void;
};

export function ProgressTodoStream(props: Readonly<Props>) {
  const [periodMode, setPeriodMode] = useState(false);
  const [anchor, setAnchor] = useState(todoAnchor);
  const viewportRef = useRef<HTMLDivElement>(null);
  const days = useMemo(() => Array.from({ length: 14 }, (_, index) => addLocalDays(anchor, index)), [anchor]);

  return (
    <section aria-label="TODO 时间流" className="min-w-0 overflow-hidden rounded-xl border border-border bg-card shadow-sm">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3">
        <div className="flex items-center gap-2"><Button aria-label="前两周" onClick={() => setAnchor((value) => addLocalDays(value, -14))} size="icon" variant="ghost"><ChevronLeft aria-hidden="true" className="size-4" /></Button><Button onClick={() => setAnchor(todoAnchor())} size="sm" variant="outline">今天</Button><Button aria-label="后两周" onClick={() => setAnchor((value) => addLocalDays(value, 14))} size="icon" variant="ghost"><ChevronRight aria-hidden="true" className="size-4" /></Button></div>
        <div className="flex rounded-lg bg-muted p-1"><Button aria-pressed={!periodMode} className="h-8" onClick={() => setPeriodMode(false)} size="sm" variant={!periodMode ? "secondary" : "ghost"}><CalendarRange aria-hidden="true" className="size-4" />日</Button><Button aria-pressed={periodMode} className="h-8" onClick={() => setPeriodMode(true)} size="sm" variant={periodMode ? "secondary" : "ghost"}><Rows3 aria-hidden="true" className="size-4" />上午/下午/夜晚/半夜</Button></div>
      </div>
      <div className="h-[42rem] overflow-y-auto" ref={viewportRef}>
        <Unscheduled {...props} />
        {days.map((day) => <TodoDay {...props} day={day} key={localDayKey(day)} periodMode={periodMode} />)}
      </div>
    </section>
  );
}

function Unscheduled({ canManage, completionByTarget, milestones, onCreate, onOpenMilestone, onOpenTask, onReview, onToggleMilestone, onToggleTask, tasks }: Readonly<Props>) {
  const unscheduledTasks = tasks.filter((task) => !task.start_at || !task.due_at);
  const unscheduledMilestones = milestones.filter((milestone) => !milestone.target_at);
  if (!unscheduledTasks.length && !unscheduledMilestones.length) return null;
  return <section className="border-b border-border bg-muted/20 p-4" onDoubleClick={(event) => { if (event.currentTarget === event.target) onCreate(dateAtMinutes(new Date(), 9 * 60)); }}><h3 className="mb-2 text-sm font-semibold">未安排</h3><div className="space-y-2">{unscheduledMilestones.map((milestone) => <ProgressItemCard canManage={canManage} item={milestone} key={milestone.milestone_id} kind="milestone" onOpen={() => onOpenMilestone(milestone)} onReview={onReview} onToggle={() => onToggleMilestone(milestone)} pendingCompletion={completionByTarget.get(milestone.milestone_id)} />)}{unscheduledTasks.map((task) => <ProgressItemCard canManage={canManage} item={task} key={task.task_id} kind="task" onOpen={() => onOpenTask(task)} onReview={onReview} onToggle={() => onToggleTask(task)} pendingCompletion={completionByTarget.get(task.task_id)} />)}</div></section>;
}

function TodoDay(props: Readonly<Props & { day: Date; periodMode: boolean }>) {
  const { canManage, completionByTarget, day, milestones, onCreate, onMoveMilestone, onOpenMilestone, onReview, onToggleMilestone, periodMode, tasks } = props;
  const dayKey = localDayKey(day);
  const nodeValues = milestones.filter((item) => item.target_at && localDayKey(new Date(item.target_at)) === dayKey);
  const taskValues = tasks.filter((task) => task.start_at && effectiveDayKey(new Date(task.start_at)) === dayKey).sort((left, right) => new Date(left.start_at!).getTime() - new Date(right.start_at!).getTime());
  const today = effectiveDayKey(new Date()) === dayKey;
  return <section className="border-b border-border last:border-b-0">
    <div className={cn("sticky top-0 z-10 flex items-center justify-between border-b border-border bg-card/95 px-4 py-2 backdrop-blur", today && "text-red-500")}><h3 className="text-sm font-semibold">{formatShortDay(day)}</h3><span className="text-xs opacity-60">{taskValues.filter((task) => task.status !== "done").length} 项待完成</span></div>
    <div className="border-b border-border bg-muted/20 px-4 py-2" onDoubleClick={(event) => { if (event.currentTarget === event.target) onCreate(dateAtMinutes(day, 9 * 60), "milestone", false); }} onDragOver={(event) => event.preventDefault()} onDrop={(event) => {
      const id = event.dataTransfer.getData("application/x-mmdash-milestone");
      const milestone = milestones.find((item) => item.milestone_id === id);
      if (!milestone?.target_at || !canManage) return;
      const at = new Date(milestone.target_at);
      at.setFullYear(day.getFullYear(), day.getMonth(), day.getDate());
      onMoveMilestone(id, at);
    }}>
      <p className="mb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">节点</p>
      <div className="flex flex-wrap gap-2">{nodeValues.length ? nodeValues.map((milestone) => <div className="min-w-44 max-w-72 flex-1" draggable={canManage} key={milestone.milestone_id} onDragEnd={endNativeProgressDrag} onDragStart={(event) => beginNativeProgressDrag(event, "application/x-mmdash-milestone", milestone.milestone_id)}><ProgressItemCard canManage={canManage} compact item={milestone} kind="milestone" onOpen={() => onOpenMilestone(milestone)} onReview={onReview} onToggle={() => onToggleMilestone(milestone)} pendingCompletion={completionByTarget.get(milestone.milestone_id)} /></div>) : <p className="py-1 text-xs text-muted-foreground">双击此处添加关键节点</p>}</div>
    </div>
    {periodMode ? PERIODS.map((period) => <TodoSection {...props} allTasks={tasks} day={day} key={period.key} label={period.label} periodStart={period.start % (24 * 60)} showCurrentTime={today && periodForDate(new Date()).key === period.key} tasks={taskValues.filter((task) => periodForDate(new Date(task.start_at!)).key === period.key)} />) : <TodoSection {...props} allTasks={tasks} day={day} label="全天安排" showCurrentTime={today} tasks={taskValues} />}
  </section>;
}

function TodoSection({ allTasks, canManage, completionByTarget, day, label, onCreate, onMoveTask, onOpenTask, onReview, onToggleTask, periodStart, showCurrentTime, tasks }: Readonly<Omit<Props, "tasks"> & { allTasks: ProgressTask[]; day: Date; label: string; periodStart?: number; showCurrentTime: boolean; tasks: ProgressTask[] }>) {
  const now = new Date();
  let lineRendered = false;
  function moveHere(id: string) {
    if (!canManage) return;
    const task = allTasks.find((item) => item.task_id === id);
    if (!task?.start_at || !task.due_at) return;
    const oldStart = new Date(task.start_at);
    const duration = new Date(task.due_at).getTime() - oldStart.getTime();
    const startAt = dateAtMinutes(day, periodStart ?? minutesFromDayStart(oldStart));
    onMoveTask(id, startAt, new Date(startAt.getTime() + duration));
  }
  return <div className="min-h-20 px-4 py-3" onDoubleClick={(event) => { if (event.currentTarget === event.target) onCreate(dateAtMinutes(day, periodStart ?? 9 * 60), "task", true); }} onDragOver={(event) => event.preventDefault()} onDrop={(event) => moveHere(event.dataTransfer.getData("application/x-mmdash-task"))}>
    <h4 className="mb-2 text-xs font-medium text-muted-foreground">{label}</h4>
    <div className="space-y-2">
      {tasks.map((task) => {
        const before = showCurrentTime && !lineRendered && new Date(task.start_at!).getTime() > now.getTime();
        if (before) lineRendered = true;
        return <div key={task.task_id}>{before ? <NowLine /> : null}<div draggable={canManage} onDragEnd={endNativeProgressDrag} onDragStart={(event) => beginNativeProgressDrag(event, "application/x-mmdash-task", task.task_id)}><ProgressItemCard canManage={canManage} item={task} kind="task" onOpen={() => onOpenTask(task)} onReview={onReview} onToggle={() => onToggleTask(task)} pendingCompletion={completionByTarget.get(task.task_id)} /></div></div>;
      })}
      {!tasks.length ? <p className="rounded-lg border border-dashed border-border px-3 py-3 text-xs text-muted-foreground">双击此区域插入任务</p> : null}
      {showCurrentTime && !lineRendered ? <NowLine /> : null}
    </div>
  </div>;
}

function NowLine() { return <div className="relative my-2 border-t border-red-500"><span className="absolute left-0 -translate-y-1/2 rounded bg-red-500 px-1.5 py-0.5 text-[10px] text-white">现在</span></div>; }

function effectiveDayKey(value: Date): string {
  return localDayKey(minutesFromDayStart(value) < 8 * 60 ? addLocalDays(value, -1) : value);
}

function todoAnchor(): Date {
  const now = new Date();
  return startOfLocalDay(minutesFromDayStart(now) < 8 * 60 ? addLocalDays(now, -1) : now);
}
