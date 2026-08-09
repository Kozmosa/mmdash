"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CalendarDays, GanttChart, KanbanSquare, ListChecks, Plus, RotateCcw } from "lucide-react";
import { type FormEvent, useRef, useState } from "react";

import { EmptyState } from "@/components/states/empty-state";
import { useCurrentProject } from "@/components/providers/project-provider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { ProgressTrackingPanel } from "@/features/progress/progress-tracking-panel";
import type { ProgressAggregate as Progress, ProgressMilestone as Milestone, ProgressProposal as Proposal, ProgressTask as Task } from "@/features/progress/types";
import { apiClient } from "@/lib/api-client";

type View = "board" | "list" | "gantt" | "today" | "proposals";
type CreateReminderRequest = {
  milestone_id?: string;
  note?: string;
  remind_at: string;
  task_id?: string;
};
type ReminderTargetType = "task" | "milestone";

const progressManageRoles = new Set(["owner", "maintainer", "editor"]);
const progressEvaluateRoles = new Set(["owner", "maintainer", "editor", "viewer"]);

export default function ProgressPage() {
  const project = useCurrentProject();
  const queryClient = useQueryClient();
  const [view, setView] = useState<View>("board");
  const [milestoneTitle, setMilestoneTitle] = useState("");
  const [taskTitle, setTaskTitle] = useState("");
  const canManageProgress = progressManageRoles.has(project.role ?? "");
  const canEvaluateProgress = progressEvaluateRoles.has(project.role ?? "");
  const progress = useQuery({
    queryKey: ["progress", project.id],
    queryFn: () => apiClient.request<Progress>(`/projects/${encodeURIComponent(project.id)}/progress`),
  });
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["progress", project.id] });
  const createMilestone = useMutation({
    mutationFn: () => apiClient.request(`/projects/${encodeURIComponent(project.id)}/progress/milestones`, { body: { title: milestoneTitle }, method: "POST" }),
    onSuccess: () => { setMilestoneTitle(""); refresh(); },
  });
  const createTask = useMutation({
    mutationFn: () => apiClient.request(`/projects/${encodeURIComponent(project.id)}/progress/tasks`, { body: { title: taskTitle }, method: "POST" }),
    onSuccess: () => { setTaskTitle(""); refresh(); },
  });
  const reviewProposal = useMutation({
    mutationFn: (input: { id: string; decision: "accepted" | "rejected" }) => apiClient.request(`/projects/${encodeURIComponent(project.id)}/progress/proposals/${encodeURIComponent(input.id)}/review`, { body: { decision: input.decision }, method: "POST" }),
    onSuccess: refresh,
  });
  const triggerReminder = useMutation({
    mutationFn: (id: string) => apiClient.request(`/projects/${encodeURIComponent(project.id)}/progress/reminders/${encodeURIComponent(id)}/trigger`, { method: "POST" }),
    onSuccess: refresh,
  });
  const data = progress.data;

  return (
    <section className="space-y-6" aria-labelledby="progress-title">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="mb-3 flex size-10 items-center justify-center rounded-lg border border-border bg-card shadow-xs"><ListChecks aria-hidden="true" className="size-5" /></div>
          <h1 id="progress-title" className="text-2xl font-semibold tracking-tight">Progress</h1>
          <p className="mt-1 text-sm text-muted-foreground">同一份 Core 权威数据驱动看板、列表、甘特、今日视图和 Proposal 审阅。</p>
        </div>
        <Button onClick={refresh} variant="outline"><RotateCcw aria-hidden="true" className="size-4" />刷新</Button>
      </header>

      {data ? <ProgressTrackingPanel canEvaluate={canEvaluateProgress} canManage={canManageProgress} progress={data} projectId={project.id} /> : null}

      {canManageProgress ? <div className="grid gap-3 md:grid-cols-2">
        <Card>
          <CardHeader><CardTitle className="text-base">建立关键节点</CardTitle></CardHeader>
          <CardContent className="flex gap-2"><Input aria-label="关键节点标题" onChange={(event) => setMilestoneTitle(event.target.value)} placeholder="例如：完成模型假设" value={milestoneTitle} /><Button disabled={!milestoneTitle.trim() || createMilestone.isPending} onClick={() => createMilestone.mutate()}><Plus className="size-4" />创建</Button></CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle className="text-base">建立任务</CardTitle></CardHeader>
          <CardContent className="flex gap-2"><Input aria-label="任务标题" onChange={(event) => setTaskTitle(event.target.value)} placeholder="例如：整理实验输入" value={taskTitle} /><Button disabled={!taskTitle.trim() || createTask.isPending} onClick={() => createTask.mutate()}><Plus className="size-4" />创建</Button></CardContent>
        </Card>
      </div> : null}

      <nav aria-label="Progress 视图" className="flex flex-wrap gap-2 border-b border-border pb-3">
        <ViewButton active={view === "board"} icon={KanbanSquare} label="看板" onClick={() => setView("board")} />
        <ViewButton active={view === "list"} icon={ListChecks} label="列表" onClick={() => setView("list")} />
        <ViewButton active={view === "gantt"} icon={GanttChart} label="甘特" onClick={() => setView("gantt")} />
        <ViewButton active={view === "today"} icon={CalendarDays} label="今日" onClick={() => setView("today")} />
        <ViewButton active={view === "proposals"} icon={RotateCcw} label="Proposal 审阅" onClick={() => setView("proposals")} />
      </nav>

      {progress.isLoading ? <p className="text-sm text-muted-foreground">正在读取 Progress…</p> : null}
      {progress.error ? <p className="text-sm text-destructive">{progress.error.message}</p> : null}
      {data && view === "board" ? <BoardView board={data.board} /> : null}
      {data && view === "list" ? <ListView milestones={data.milestones} tasks={data.tasks} /> : null}
      {data && view === "gantt" ? <GanttView items={data.gantt} /> : null}
      {data && view === "today" ? <TodayView blocked={data.blocked} overdue={data.overdue} tasks={data.today} /> : null}
      {data && view === "proposals" ? <ProposalView canReview={canManageProgress} proposals={data.proposals} onReview={(id, decision) => reviewProposal.mutate({ id, decision })} /> : null}

      {data ? <Card><CardHeader><CardTitle className="text-base">提醒</CardTitle></CardHeader><CardContent className="space-y-5">{canManageProgress ? <ReminderCreationForm milestones={data.milestones} tasks={data.tasks} /> : null}{data.reminders.length ? <div className="space-y-2">{data.reminders.map((reminder) => <div className="flex items-center justify-between gap-3 rounded-lg border border-border p-3" key={reminder.reminder_id}><div><p className="text-sm">{reminder.note || "Progress 提醒"}</p><p className="text-xs text-muted-foreground">{new Date(reminder.remind_at).toLocaleString()} · {reminder.status}</p></div>{canManageProgress ? <Button disabled={reminder.status !== "pending" || triggerReminder.isPending} onClick={() => triggerReminder.mutate(reminder.reminder_id)} size="sm" variant="outline">触发事件</Button> : null}</div>)}</div> : <EmptyState description="创建提醒后，Core 会按 remind_at 自动发布稳定 reminder 事件；外部通知由 Notification 接管。" title="暂无提醒" />}</CardContent></Card> : null}
    </section>
  );
}

function ViewButton({ active, icon: Icon, label, onClick }: { active: boolean; icon: typeof ListChecks; label: string; onClick: () => void }) {
  return <Button aria-pressed={active} onClick={onClick} variant={active ? "secondary" : "outline"}><Icon aria-hidden="true" className="size-4" />{label}</Button>;
}

function BoardView({ board }: { board: Progress["board"] }) {
  const columns = [["待办", board.todo ?? []], ["进行中", board.in_progress ?? []], ["阻塞", board.blocked ?? []], ["完成", board.done ?? []]] as const;
  return <div className="grid gap-3 lg:grid-cols-4">{columns.map(([title, tasks]) => <Card key={title}><CardHeader><CardTitle className="flex items-center justify-between text-sm">{title}<Badge>{tasks.length}</Badge></CardTitle></CardHeader><CardContent className="space-y-2">{tasks.length ? tasks.map((task) => <TaskCard key={task.task_id} task={task} />) : <EmptyState className="min-h-28 p-4" description="没有符合条件的任务。" title="空" />}</CardContent></Card>)}</div>;
}

function ListView({ milestones, tasks }: { milestones: Milestone[]; tasks: Task[] }) {
  if (!milestones.length && !tasks.length) return <EmptyState description="人工创建 Milestone 或 Task 后，所有 Progress 视图会同步显示。" title="还没有进度记录" />;
  return <div className="grid gap-4"><Card><CardHeader><CardTitle className="text-base">关键节点</CardTitle></CardHeader><CardContent className="space-y-2">{milestones.map((item) => <div className="flex items-center justify-between rounded-lg border border-border p-3" key={item.milestone_id}><span className="text-sm">{item.title}</span><Badge>{item.status}</Badge></div>)}</CardContent></Card><Card><CardHeader><CardTitle className="text-base">任务</CardTitle></CardHeader><CardContent className="space-y-2">{tasks.map((item) => <TaskCard key={item.task_id} task={item} />)}</CardContent></Card></div>;
}

function GanttView({ items }: { items: Progress["gantt"] }) {
  return items.length ? <Card><CardHeader><CardTitle className="text-base">时间轴</CardTitle></CardHeader><CardContent className="space-y-3">{items.map((item) => <div className="grid gap-2 md:grid-cols-[12rem_1fr] md:items-center" key={`${item.kind}-${item.id}`}><div><p className="truncate text-sm">{item.title}</p><p className="text-xs text-muted-foreground">{item.kind} · {item.status}</p></div><div className="h-3 rounded-full bg-muted"><div className="h-3 w-1/3 rounded-full bg-primary/70" /></div></div>)}</CardContent></Card> : <EmptyState description="设置时间后，Milestone 和 Task 会出现在同一时间轴。" title="暂无时间安排" />;
}

function TodayView({ blocked, overdue, tasks }: { blocked: Task[]; overdue: Task[]; tasks: Task[] }) {
  const groups = [["今日", tasks ?? []], ["逾期", overdue ?? []], ["阻塞", blocked ?? []]] as const;
  return <div className="grid gap-3 md:grid-cols-3">{groups.map(([title, values]) => <Card key={title}><CardHeader><CardTitle className="flex items-center justify-between text-sm">{title}<Badge>{values.length}</Badge></CardTitle></CardHeader><CardContent className="space-y-2">{values.length ? values.map((task) => <TaskCard key={task.task_id} task={task} />) : <EmptyState className="min-h-28 p-4" description="暂无任务。" title="空" />}</CardContent></Card>)}</div>;
}

function ProposalView({ canReview, proposals, onReview }: { canReview: boolean; proposals: Proposal[]; onReview: (id: string, decision: "accepted" | "rejected") => void }) {
  const pending = proposals.filter((proposal) => proposal.status === "pending");
  return pending.length ? <div className="space-y-3">{pending.map((proposal) => <Card key={proposal.proposal_id}><CardHeader><CardTitle className="text-base">{proposal.title}</CardTitle><p className="text-xs text-muted-foreground">{proposal.proposal_type} · 来源：{proposal.source_run_id || proposal.source}</p></CardHeader><CardContent className="space-y-3"><p className="text-sm text-muted-foreground">{proposal.rationale || "未提供理由"}</p><pre className="overflow-x-auto rounded-lg bg-muted p-3 text-xs">{JSON.stringify(proposal.changes, null, 2)}</pre>{canReview ? <div className="flex gap-2"><Button onClick={() => onReview(proposal.proposal_id, "accepted")} size="sm">接受</Button><Button onClick={() => onReview(proposal.proposal_id, "rejected")} size="sm" variant="outline">拒绝</Button></div> : null}</CardContent></Card>)}</div> : <EmptyState description="Agent 或其他非人类来源的关键节点变更会进入这里，接受后才由 Progress 服务应用。" title="暂无待审 Proposal" />;
}

function ReminderCreationForm({ milestones, tasks }: { milestones: Milestone[]; tasks: Task[] }) {
  const project = useCurrentProject();
  const queryClient = useQueryClient();
  const submissionLocked = useRef(false);
  const [targetType, setTargetType] = useState<ReminderTargetType>("task");
  const [targetID, setTargetID] = useState("");
  const [remindAt, setRemindAt] = useState("");
  const [note, setNote] = useState("");
  const [feedback, setFeedback] = useState<{ kind: "error" | "success"; message: string } | null>(null);
  const createReminder = useMutation({
    mutationFn: (input: CreateReminderRequest) => apiClient.request(`/projects/${encodeURIComponent(project.id)}/progress/reminders`, { body: input, method: "POST" }),
    onError: () => setFeedback({ kind: "error", message: "创建提醒失败，请稍后重试。" }),
    onSuccess: async () => {
      setTargetID("");
      setRemindAt("");
      setNote("");
      setFeedback({ kind: "success", message: "提醒已创建，列表已刷新。" });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["progress", project.id] }),
        queryClient.invalidateQueries({ queryKey: ["project-home", project.id] }),
      ]);
    },
    onSettled: () => {
      submissionLocked.current = false;
    },
  });
  const targets = targetType === "task" ? tasks : milestones;

  function chooseTargetType(next: ReminderTargetType) {
    setTargetType(next);
    setTargetID("");
    setFeedback(null);
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submissionLocked.current || createReminder.isPending) return;
    const targetExists = targetType === "task"
      ? tasks.some((task) => task.task_id === targetID)
      : milestones.some((milestone) => milestone.milestone_id === targetID);
    if (!targetID || !targetExists) {
      setFeedback({ kind: "error", message: "请选择当前项目中的 Task 或 Milestone。" });
      return;
    }
    const date = new Date(remindAt);
    if (!remindAt || Number.isNaN(date.getTime())) {
      setFeedback({ kind: "error", message: "请输入有效的本地提醒时间。" });
      return;
    }
    if (note.length > 2_000) {
      setFeedback({ kind: "error", message: "提醒备注不能超过 2000 个字符。" });
      return;
    }
    const input: CreateReminderRequest = { remind_at: date.toISOString() };
    if (targetType === "task") input.task_id = targetID;
    else input.milestone_id = targetID;
    if (note.trim()) input.note = note.trim();
    submissionLocked.current = true;
    setFeedback(null);
    createReminder.mutate(input);
  }

  return (
    <form className="space-y-4 rounded-lg border border-border p-4" onSubmit={submit}>
      <div>
        <p className="text-sm font-medium">创建提醒</p>
        <p className="mt-1 text-xs text-muted-foreground">时间按当前浏览器的本地时区填写，提交时转换为 ISO 时间。</p>
      </div>
      <fieldset className="space-y-2">
        <legend className="text-sm font-medium">目标类型</legend>
        <div className="flex flex-wrap gap-4 text-sm">
          <label className="flex items-center gap-2"><input aria-label="目标类型：Task" checked={targetType === "task"} name="reminder-target-type" onChange={() => chooseTargetType("task")} type="radio" />Task</label>
          <label className="flex items-center gap-2"><input aria-label="目标类型：Milestone" checked={targetType === "milestone"} name="reminder-target-type" onChange={() => chooseTargetType("milestone")} type="radio" />Milestone</label>
        </div>
      </fieldset>
      <label className="block space-y-2 text-sm">
        <span>提醒目标</span>
        <select aria-label="提醒目标" className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50" disabled={!targets.length || createReminder.isPending} onChange={(event) => { setTargetID(event.target.value); setFeedback(null); }} value={targetID}>
          <option value="">{targetType === "task" ? "选择 Task" : "选择 Milestone"}</option>
          {targetType === "task" ? tasks.map((task) => <option key={task.task_id} value={task.task_id}>{task.title}</option>) : milestones.map((milestone) => <option key={milestone.milestone_id} value={milestone.milestone_id}>{milestone.title}</option>)}
        </select>
      </label>
      {!targets.length ? <p className="text-xs text-muted-foreground">当前项目没有可选的 {targetType === "task" ? "Task" : "Milestone"}。</p> : null}
      <label className="block space-y-2 text-sm">
        <span>提醒时间（本地时间）</span>
        <Input aria-label="提醒时间" disabled={createReminder.isPending} onChange={(event) => { setRemindAt(event.target.value); setFeedback(null); }} required type="datetime-local" value={remindAt} />
      </label>
      <label className="block space-y-2 text-sm">
        <span>备注（可选）</span>
        <textarea aria-label="提醒备注" className="min-h-24 w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50" disabled={createReminder.isPending} maxLength={2_000} onChange={(event) => { setNote(event.target.value); setFeedback(null); }} value={note} />
        <span className="text-xs text-muted-foreground">{note.length}/2000</span>
      </label>
      {feedback ? <p aria-live="polite" className={feedback.kind === "error" ? "text-sm text-destructive" : "text-sm text-muted-foreground"} role={feedback.kind === "error" ? "alert" : "status"}>{feedback.message}</p> : null}
      <Button disabled={!targetID || !remindAt || note.length > 2_000 || createReminder.isPending} type="submit"><Plus aria-hidden="true" className="size-4" />{createReminder.isPending ? "正在创建…" : "创建提醒"}</Button>
    </form>
  );
}

function TaskCard({ task }: { task: Task }) {
  return <div className="rounded-lg border border-border p-3"><div className="flex items-start justify-between gap-2"><p className="text-sm font-medium">{task.title}</p><Badge>{task.status}</Badge></div><p className="mt-1 text-xs text-muted-foreground">来源：{task.source}{task.due_at ? ` · 截止 ${new Date(task.due_at).toLocaleDateString()}` : ""}</p></div>;
}
