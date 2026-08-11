"use client";

import { Flag, X } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

import { snapDate } from "./calendar-time";
import type { ProgressMilestone, ProgressTask } from "./types";

export type DrawerSelection =
  | { kind: "task"; task: ProgressTask }
  | { kind: "milestone"; milestone: ProgressMilestone }
  | { at: Date; kind: "task" | "milestone"; targetHasTime: boolean };

export type ProgressItemDraft = {
  id?: string;
  kind: "task" | "milestone";
  title: string;
  description: string;
  startAt?: Date;
  endAt?: Date;
  targetHasTime: boolean;
};

export function ProgressItemDrawer({ busy, onClose, onSave, selection }: Readonly<{
  busy: boolean;
  onClose: () => void;
  onSave: (draft: ProgressItemDraft) => void;
  selection: DrawerSelection | null;
}>) {
  const [kind, setKind] = useState<"task" | "milestone">("task");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [targetHasTime, setTargetHasTime] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!selection) return;
    setKind(selection.kind);
    setError(null);
    if ("task" in selection) {
      setTitle(selection.task.title);
      setDescription(selection.task.description);
      setStart(toLocalInput(selection.task.start_at));
      setEnd(toLocalInput(selection.task.due_at));
      setTargetHasTime(true);
    } else if ("milestone" in selection) {
      setTitle(selection.milestone.title);
      setDescription(selection.milestone.description);
      setStart(toLocalInput(selection.milestone.target_at));
      setEnd("");
      setTargetHasTime(selection.milestone.target_has_time);
    } else {
      const at = snapDate(selection.at);
      setTitle("");
      setDescription("");
      setStart(toLocalInput(at.toISOString()));
      setEnd(selection.kind === "task" ? toLocalInput(new Date(at.getTime() + 60 * 60_000).toISOString()) : "");
      setTargetHasTime(selection.targetHasTime);
    }
  }, [selection]);

  if (!selection) return null;
  const current = selection;
  const editing = "task" in current || "milestone" in current;

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const startAt = start ? snapDate(new Date(start)) : undefined;
    const endAt = end ? snapDate(new Date(end)) : undefined;
    if (!title.trim()) return setError("请输入标题。" );
    if (kind === "task" && (!startAt || !endAt || endAt <= startAt)) return setError("任务需要有效的开始和结束时间。" );
    if (kind === "milestone" && !startAt) return setError("请选择关键节点所在日期。" );
    onSave({
      description: description.trim(),
      endAt,
      id: "task" in current ? current.task.task_id : "milestone" in current ? current.milestone.milestone_id : undefined,
      kind,
      startAt,
      targetHasTime,
      title: title.trim(),
    });
  }

  return (
    <div aria-modal="true" className="fixed inset-0 z-50 flex justify-end bg-black/20" onMouseDown={(event) => { if (event.currentTarget === event.target) onClose(); }} role="dialog">
      <aside className="h-full w-full max-w-md overflow-y-auto border-l border-border bg-background p-6 shadow-2xl">
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="flex items-center gap-2 text-sm font-semibold"><Flag aria-hidden="true" className="size-4" />{editing ? "安排详情" : "新建安排"}</p>
            <p className="mt-1 text-xs text-muted-foreground">时间会自动吸附到最近的 15 分钟。</p>
          </div>
          <Button aria-label="关闭详情" onClick={onClose} size="icon" variant="ghost"><X aria-hidden="true" className="size-4" /></Button>
        </div>

        <form className="mt-6 space-y-5" onSubmit={submit}>
          {!editing ? (
            <fieldset>
              <legend className="mb-2 text-sm font-medium">类型</legend>
              <div className="grid grid-cols-2 gap-2">
                <Button aria-pressed={kind === "task"} onClick={() => setKind("task")} variant={kind === "task" ? "secondary" : "outline"}>任务安排</Button>
                <Button aria-pressed={kind === "milestone"} onClick={() => setKind("milestone")} variant={kind === "milestone" ? "secondary" : "outline"}>关键节点</Button>
              </div>
            </fieldset>
          ) : null}
          <label className="block space-y-2 text-sm"><span>标题</span><Input autoFocus maxLength={255} onChange={(event) => setTitle(event.target.value)} value={title} /></label>
          <label className="block space-y-2 text-sm"><span>说明</span><textarea className="min-h-36 w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring" maxLength={10_000} onChange={(event) => setDescription(event.target.value)} placeholder="记录交付标准、背景或下一步…" value={description} /></label>
          <label className="block space-y-2 text-sm"><span>{kind === "task" ? "开始" : "日期与时刻"}</span><Input onChange={(event) => setStart(event.target.value)} required type="datetime-local" value={start} /></label>
          {kind === "task" ? <label className="block space-y-2 text-sm"><span>结束</span><Input onChange={(event) => setEnd(event.target.value)} required type="datetime-local" value={end} /></label> : (
            <label className="flex items-center justify-between gap-3 rounded-lg border border-border p-3 text-sm"><span><span className="block font-medium">包含具体时刻</span><span className="text-xs text-muted-foreground">关闭后只显示在节点区</span></span><input checked={targetHasTime} onChange={(event) => setTargetHasTime(event.target.checked)} type="checkbox" /></label>
          )}
          {error ? <p className="text-sm text-destructive" role="alert">{error}</p> : null}
          <div className="flex justify-end gap-2 border-t border-border pt-4"><Button onClick={onClose} variant="outline">关闭</Button><Button disabled={busy || !title.trim()} type="submit">{busy ? "保存中…" : "保存"}</Button></div>
        </form>
      </aside>
    </div>
  );
}

function toLocalInput(value?: string): string {
  if (!value) return "";
  const date = new Date(value);
  const shifted = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return shifted.toISOString().slice(0, 16);
}
