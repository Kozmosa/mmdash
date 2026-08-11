"use client";

import { ChevronLeft, ChevronRight, Crosshair } from "lucide-react";
import { type PointerEvent as ReactPointerEvent, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/cn";

import { addLocalDays, assignOverlapLanes, dateAtMinutes, formatShortDay, formatTime, localDayKey, minutesFromDayStart, snapDate, startOfLocalDay } from "./calendar-time";
import { beginNativeProgressDrag, endNativeProgressDrag } from "./progress-drag-preview";
import { ProgressItemCard } from "./progress-item-card";
import type { ProgressMilestone, ProgressProposal, ProgressTask } from "./types";

const PX_PER_MINUTE = 1.2;
const GRID_HEIGHT = 24 * 60 * PX_PER_MINUTE;
const DAY_WIDTH = 230;

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

type DragState = {
  dayWidth: number;
  id: string;
  kind: "task" | "milestone";
  mode: "move" | "start" | "end";
  originX: number;
  originY: number;
  startAt: Date;
  title: string;
  endAt?: Date;
};

export function ProgressCalendar(props: Readonly<Props>) {
  const [anchor, setAnchor] = useState(() => startOfLocalDay(new Date()));
  const [dayCount, setDayCount] = useState(2);
  const viewportRef = useRef<HTMLDivElement>(null);
  const daysRef = useRef<HTMLDivElement>(null);
  const [drag, setDrag] = useState<DragState | null>(null);
  const [dragPosition, setDragPosition] = useState<{ x: number; y: number } | null>(null);
  const days = useMemo(() => Array.from({ length: dayCount }, (_, index) => addLocalDays(anchor, index)), [anchor, dayCount]);
  const now = new Date();

  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const target = minutesFromDayStart(new Date()) * PX_PER_MINUTE - viewport.clientHeight / 2;
    viewport.scrollTop = Math.max(0, target);
  }, []);

  useEffect(() => {
    if (!drag) return;
    let latestX = drag.originX;
    let latestY = drag.originY;
    const move = (event: PointerEvent) => { latestX = event.clientX; latestY = event.clientY; setDragPosition({ x: latestX, y: latestY }); };
    const up = () => {
      const deltaMinutes = Math.round(((latestY - drag.originY) / PX_PER_MINUTE) / 15) * 15;
      const deltaDays = drag.mode === "move" ? Math.round((latestX - drag.originX) / Math.max(drag.dayWidth, 1)) : 0;
      if (drag.kind === "milestone") {
        props.onMoveMilestone(drag.id, snapDate(addLocalDays(new Date(drag.startAt.getTime() + deltaMinutes * 60_000), deltaDays)));
      } else if (drag.endAt) {
        let startAt = addLocalDays(drag.startAt, deltaDays);
        let dueAt = addLocalDays(drag.endAt, deltaDays);
        if (drag.mode === "move") {
          startAt = new Date(startAt.getTime() + deltaMinutes * 60_000);
          dueAt = new Date(dueAt.getTime() + deltaMinutes * 60_000);
        } else if (drag.mode === "start") {
          startAt = new Date(startAt.getTime() + deltaMinutes * 60_000);
          if (startAt >= dueAt) startAt = new Date(dueAt.getTime() - 15 * 60_000);
        } else {
          dueAt = new Date(dueAt.getTime() + deltaMinutes * 60_000);
          if (dueAt <= startAt) dueAt = new Date(startAt.getTime() + 15 * 60_000);
        }
        props.onMoveTask(drag.id, snapDate(startAt), snapDate(dueAt));
      }
      setDrag(null);
      setDragPosition(null);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up, { once: true });
    return () => { window.removeEventListener("pointermove", move); window.removeEventListener("pointerup", up); };
  }, [drag, props]);

  function cycleDays() { setDayCount((value) => value === 4 ? 2 : value + 1); }
  function beginDrag(event: ReactPointerEvent, input: Omit<DragState, "dayWidth" | "originX" | "originY">) {
    if (!props.canManage || event.button > 0) return;
    event.preventDefault();
    event.stopPropagation();
    setDrag({ ...input, dayWidth: daysRef.current ? daysRef.current.scrollWidth / dayCount : DAY_WIDTH, originX: event.clientX, originY: event.clientY });
    setDragPosition({ x: event.clientX, y: event.clientY });
  }

  return (
    <section aria-label="进度日历" className="overflow-hidden rounded-xl border border-border bg-card shadow-sm">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3">
        <div className="flex items-center gap-2">
          <Button aria-label="上一段日期" onClick={() => setAnchor((value) => addLocalDays(value, -dayCount))} size="icon" variant="ghost"><ChevronLeft aria-hidden="true" className="size-4" /></Button>
          <Button onClick={() => setAnchor(startOfLocalDay(new Date()))} size="sm" variant="outline"><Crosshair aria-hidden="true" className="size-4" />今天</Button>
          <Button aria-label="下一段日期" onClick={() => setAnchor((value) => addLocalDays(value, dayCount))} size="icon" variant="ghost"><ChevronRight aria-hidden="true" className="size-4" /></Button>
          <p className="ml-2 text-sm font-semibold">{new Intl.DateTimeFormat("zh-CN", { month: "long", year: "numeric" }).format(anchor)}</p>
        </div>
        <div className="flex rounded-lg bg-muted p-1"><Button aria-pressed={dayCount === 1} className="h-8" onClick={() => setDayCount(1)} size="sm" variant={dayCount === 1 ? "secondary" : "ghost"}>日</Button><Button aria-label="重复点击切换双日、三日、四日" aria-pressed={dayCount > 1} className="h-8 min-w-16" onClick={cycleDays} size="sm" variant={dayCount > 1 ? "secondary" : "ghost"}>{dayCount === 2 ? "双日" : dayCount === 3 ? "三日" : "四日"}</Button></div>
      </div>

      <div className="h-[42rem] overflow-auto" ref={viewportRef}>
        <div className="sticky top-0 z-30 bg-card/95 backdrop-blur" style={{ minWidth: 64 + DAY_WIDTH * dayCount }}>
          <div className="ml-16 grid border-b border-border" style={{ gridTemplateColumns: `repeat(${dayCount}, minmax(${DAY_WIDTH}px, 1fr))` }}>
            {days.map((day) => <div className={cn("border-r border-border px-3 py-2 text-center text-sm", localDayKey(day) === localDayKey(now) && "font-semibold text-red-500")} key={localDayKey(day)}>{formatShortDay(day)}</div>)}
          </div>
          <div className="flex border-b border-border bg-muted/20">
            <div className="w-16 shrink-0 px-2 py-2 text-right text-[10px] text-muted-foreground">节点</div>
            <div className="grid flex-1" style={{ gridTemplateColumns: `repeat(${dayCount}, minmax(${DAY_WIDTH}px, 1fr))` }}>
              {days.map((day) => <MilestoneStrip {...props} day={day} key={localDayKey(day)} />)}
            </div>
          </div>
        </div>

        <div className="flex" style={{ minWidth: 64 + DAY_WIDTH * dayCount }}>
          <TimeLabels />
          <div className="grid flex-1" ref={daysRef} style={{ gridTemplateColumns: `repeat(${dayCount}, minmax(${DAY_WIDTH}px, 1fr))` }}>
            {days.map((day) => <DayColumn {...props} activeDrag={drag} beginDrag={beginDrag} day={day} dragPosition={dragPosition} key={localDayKey(day)} now={now} />)}
          </div>
        </div>
      </div>
      {drag && dragPosition && drag.mode === "move" ? createPortal(<PointerDragGhost kind={drag.kind} position={dragPosition} title={drag.title} />, document.body) : null}
    </section>
  );
}

function MilestoneStrip({ canManage, completionByTarget, day, milestones, onCreate, onMoveMilestone, onOpenMilestone, onReview, onToggleMilestone }: Readonly<Props & { day: Date }>) {
  const values = milestones.filter((item) => item.target_at && localDayKey(new Date(item.target_at)) === localDayKey(day));
  return <div className="min-h-16 space-y-1 border-r border-border p-1" onDoubleClick={(event) => { if (event.currentTarget === event.target) onCreate(dateAtMinutes(day, 9 * 60), "milestone", false); }} onDragOver={(event) => event.preventDefault()} onDrop={(event) => {
    const id = event.dataTransfer.getData("application/x-mmdash-milestone");
    const current = milestones.find((item) => item.milestone_id === id);
    if (!current?.target_at || !canManage) return;
    const target = new Date(current.target_at);
    target.setFullYear(day.getFullYear(), day.getMonth(), day.getDate());
    onMoveMilestone(id, target);
  }}>
    {values.map((milestone) => <div draggable={canManage} key={milestone.milestone_id} onDragEnd={endNativeProgressDrag} onDragStart={(event) => beginNativeProgressDrag(event, "application/x-mmdash-milestone", milestone.milestone_id)}><ProgressItemCard canManage={canManage} compact item={milestone} kind="milestone" onOpen={() => onOpenMilestone(milestone)} onReview={onReview} onToggle={() => onToggleMilestone(milestone)} pendingCompletion={completionByTarget.get(milestone.milestone_id)} /></div>)}
  </div>;
}

function DayColumn(props: Readonly<Props & {
  activeDrag: DragState | null;
  beginDrag: (event: ReactPointerEvent, input: Omit<DragState, "dayWidth" | "originX" | "originY">) => void;
  day: Date;
  dragPosition: { x: number; y: number } | null;
  now: Date;
}>) {
  const { activeDrag, beginDrag, canManage, completionByTarget, day, dragPosition, milestones, now, onCreate, onOpenMilestone, onOpenTask, onReview, onToggleMilestone, onToggleTask, tasks } = props;
  const scheduled = tasks.filter((task) => task.start_at && task.due_at && localDayKey(new Date(task.start_at)) === localDayKey(day));
  const lanes = assignOverlapLanes(scheduled.map((task) => ({ end: new Date(task.due_at!).getTime(), id: task.task_id, start: new Date(task.start_at!).getTime() })));
  const timedMilestones = milestones.filter((item) => item.target_has_time && item.target_at && localDayKey(new Date(item.target_at)) === localDayKey(day));
  const isToday = localDayKey(day) === localDayKey(now);
  return <div className="relative border-r border-border" data-testid={`calendar-day-${localDayKey(day)}`} onDoubleClick={(event) => {
    if (event.currentTarget !== event.target) return;
    const rect = event.currentTarget.getBoundingClientRect();
    const minutes = Math.round(((event.clientY - rect.top) / PX_PER_MINUTE) / 15) * 15;
    onCreate(dateAtMinutes(day, Math.max(0, Math.min(23 * 60 + 45, minutes))), "task", true);
  }} style={{ height: GRID_HEIGHT }}>
    <GridLines />
    {isToday ? <div className="pointer-events-none absolute left-0 right-0 z-20 border-t border-red-500" style={{ top: minutesFromDayStart(now) * PX_PER_MINUTE }}><span className="absolute -left-1 -translate-x-full -translate-y-1/2 rounded bg-red-500 px-1 text-[9px] text-white">{formatTime(now)}</span></div> : null}
    {scheduled.map((task) => {
      const startAt = new Date(task.start_at!);
      const dueAt = new Date(task.due_at!);
      let visualStartAt = startAt;
      let visualDueAt = dueAt;
      const isActiveResize = activeDrag?.kind === "task" && activeDrag.id === task.task_id && activeDrag.mode !== "move" && dragPosition;
      if (isActiveResize) {
        const deltaMinutes = Math.round(((dragPosition.y - activeDrag.originY) / PX_PER_MINUTE) / 15) * 15;
        if (activeDrag.mode === "start") {
          visualStartAt = new Date(Math.min(startAt.getTime() + deltaMinutes * 60_000, dueAt.getTime() - 15 * 60_000));
        } else {
          visualDueAt = new Date(Math.max(dueAt.getTime() + deltaMinutes * 60_000, startAt.getTime() + 15 * 60_000));
        }
      }
      const lane = lanes.get(task.task_id) ?? { lane: 0, lanes: 1 };
      const top = (visualStartAt.getTime() - startOfLocalDay(day).getTime()) / 60_000 * PX_PER_MINUTE;
      const height = Math.max(24, (visualDueAt.getTime() - visualStartAt.getTime()) / 60_000 * PX_PER_MINUTE);
      return <div className={cn("absolute z-10 px-0.5 transition-opacity", activeDrag?.id === task.task_id && activeDrag.mode === "move" && "opacity-30")} data-testid={`calendar-task-${task.task_id}`} key={task.task_id} style={{ height, left: `${lane.lane / lane.lanes * 100}%`, top, width: `${100 / lane.lanes}%` }}>
        <button aria-label={`调整 ${task.title} 开始时间`} className="absolute inset-x-2 top-0 z-20 h-1 cursor-ns-resize" onPointerDown={(event) => beginDrag(event, { endAt: dueAt, id: task.task_id, kind: "task", mode: "start", startAt, title: task.title })} type="button" />
        <div className="h-full cursor-grab" onPointerDown={(event) => beginDrag(event, { endAt: dueAt, id: task.task_id, kind: "task", mode: "move", startAt, title: task.title })}><ProgressItemCard canManage={canManage} className="h-full" compact item={task} kind="task" onOpen={() => onOpenTask(task)} onReview={onReview} onToggle={() => onToggleTask(task)} pendingCompletion={completionByTarget.get(task.task_id)} /></div>
        <button aria-label={`调整 ${task.title} 结束时间`} className="absolute inset-x-2 bottom-0 z-20 h-1 cursor-ns-resize" onPointerDown={(event) => beginDrag(event, { endAt: dueAt, id: task.task_id, kind: "task", mode: "end", startAt, title: task.title })} type="button" />
        {isActiveResize ? <div aria-hidden="true" className="pointer-events-none absolute bottom-1 right-1 z-30 rounded bg-foreground/80 px-1.5 py-0.5 text-[9px] font-medium text-background shadow" data-testid="calendar-resize-preview">{formatTime(visualStartAt)}–{formatTime(visualDueAt)}</div> : null}
      </div>;
    })}
    {timedMilestones.map((milestone) => {
      const at = new Date(milestone.target_at!);
      return <div className={cn("absolute left-2 right-2 z-20 cursor-grab transition-opacity", activeDrag?.id === milestone.milestone_id && "opacity-30")} key={milestone.milestone_id} onPointerDown={(event) => beginDrag(event, { id: milestone.milestone_id, kind: "milestone", mode: "move", startAt: at, title: milestone.title })} style={{ top: minutesFromDayStart(at) * PX_PER_MINUTE - 10 }}>
        <ProgressItemCard canManage={canManage} className="border-violet-400 bg-violet-100 text-violet-950 dark:bg-violet-950 dark:text-violet-100" compact item={milestone} kind="milestone" onOpen={() => onOpenMilestone(milestone)} onReview={onReview} onToggle={() => onToggleMilestone(milestone)} pendingCompletion={completionByTarget.get(milestone.milestone_id)} />
      </div>;
    })}
  </div>;
}

function TimeLabels() {
  return <div className="relative w-16 shrink-0 border-r border-border" style={{ height: GRID_HEIGHT }}>{Array.from({ length: 24 }, (_, hour) => <span className="absolute right-2 -translate-y-1/2 text-[10px] text-muted-foreground" key={hour} style={{ top: hour * 60 * PX_PER_MINUTE }}>{String(hour).padStart(2, "0")}:00</span>)}</div>;
}

function GridLines() {
  return <>{Array.from({ length: 24 }, (_, hour) => <div className="pointer-events-none absolute left-0 right-0 border-t border-border/70" key={hour} style={{ top: hour * 60 * PX_PER_MINUTE }} />)}{Array.from({ length: 96 }, (_, slot) => slot % 4 ? <div className="pointer-events-none absolute left-0 right-0 border-t border-border/20" key={slot} style={{ top: slot * 15 * PX_PER_MINUTE }} /> : null)}</>;
}

function PointerDragGhost({ kind, position, title }: Readonly<{ kind: "task" | "milestone"; position: { x: number; y: number }; title: string }>) {
  return <div aria-hidden="true" className={cn("pointer-events-none fixed z-[100] w-56 rotate-1 rounded-lg border px-3 py-2 text-xs font-medium opacity-70 shadow-2xl backdrop-blur-sm", kind === "task" ? "border-sky-400 bg-sky-100 text-sky-950" : "border-violet-400 bg-violet-100 text-violet-950")} style={{ left: position.x + 12, top: position.y + 12 }}>{title}<p className="mt-1 text-[10px] opacity-60">松开以放置 · 15 分钟吸附</p></div>;
}
