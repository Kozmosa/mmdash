"use client";

import {
  CheckSquare2,
  FileArchive,
  Play,
  RotateCcw,
  Square,
  Terminal,
} from "lucide-react";
import Link from "next/link";
import { type ReactNode, useMemo } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

import type { Experiment, ExperimentLog, ExperimentStatus, ResultFile } from "./types";

export const activeStatuses: ExperimentStatus[] = [
  "queued",
  "preparing",
  "running",
  "uploading",
  "processing_result",
  "verifying_result",
];

export const terminalStatuses: ExperimentStatus[] = [
  "succeeded",
  "failed",
  "canceled",
  "timed_out",
  "archived",
];

export const rerunnableStatuses: ExperimentStatus[] = [
  "failed",
  "canceled",
  "timed_out",
];

export function ExperimentCard({
  checked,
  compareMode,
  item,
  onCancel,
  onCompare,
  onRerun,
  onRun,
  onSelect,
}: Readonly<{
  checked: boolean;
  compareMode: boolean;
  item: Experiment;
  onCancel: () => void;
  onCompare: () => void;
  onRerun: () => void;
  onRun: () => void;
  onSelect?: () => void;
}>) {
  const terminal = terminalStatuses.includes(item.execution_status);
  const href = `/projects/${encodeURIComponent(item.project_id)}/experiments/${encodeURIComponent(item.experiment_id)}`;
  return (
    <article className="rounded-lg border border-border p-4 transition-colors hover:border-foreground/25">
      <Link className="block text-left" href={href} onClick={onSelect}>
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-semibold">{item.name}</span>
          <Badge>{item.experiment_type}</Badge>
          <StatusBadge status={item.execution_status} />
          {item.connectivity_status === "box_offline" ? <Badge className="border-amber-400 bg-amber-500/10 text-amber-700">Box 离线</Badge> : null}
          <span className="ml-auto text-xs text-muted-foreground">{new Date(item.updated_at).toLocaleString()}</span>
        </div>
        <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-muted"><div className="h-full rounded-full bg-primary transition-all" style={{ width: `${Math.max(0, Math.min(100, item.progress))}%` }} /></div>
        <p className="mt-2 text-xs text-muted-foreground">
          {item.source_commit.slice(0, 12)} · {item.entrypoint} · {item.actual_runtime ?? item.requested_runtime_policy}
          {item.box_id ? ` · Box ${item.box_id.slice(0, 8)}` : ""}
        </p>
        {item.failure ? <p className="mt-2 text-sm text-destructive">{item.failure.stage} / {item.failure.code}: {item.failure.message}</p> : null}
        {item.summary ? <p className="mt-2 line-clamp-2 text-sm text-muted-foreground">{item.summary}</p> : null}
        {item.retry.warning_code ? <p className="mt-2 text-xs text-amber-600">已有更新的重跑记录：{item.retry.latest_experiment_id}</p> : null}
      </Link>
      <div className="mt-3 flex flex-wrap gap-2">
        {item.execution_status === "created" ? <Button onClick={onRun} size="sm"><Play className="size-3.5" />确认运行</Button> : null}
        {!terminal && item.execution_status !== "created" && item.execution_status !== "awaiting_result" ? <Button onClick={onCancel} size="sm" variant="outline"><Square className="size-3.5" />取消</Button> : null}
        {rerunnableStatuses.includes(item.execution_status) && item.experiment_type !== "self" ? <Button onClick={onRerun} size="sm" variant="outline"><RotateCcw className="size-3.5" />创建重跑</Button> : null}
        {compareMode ? <Button onClick={onCompare} size="sm" variant={checked ? "default" : "outline"}><CheckSquare2 className="size-3.5" />{checked ? "已选择" : "加入比较"}</Button> : null}
      </div>
    </article>
  );
}

export function StatusBadge({ status }: Readonly<{ status: ExperimentStatus }>) {
  const label: Record<ExperimentStatus, string> = {
    archived: "已归档", awaiting_result: "等待结果", canceled: "已取消", created: "待确认", failed: "失败",
    preparing: "准备中", processing_result: "处理结果", queued: "排队中", running: "运行中", succeeded: "已完成",
    timed_out: "超时", uploading: "上传结果", verifying_result: "验证结果",
  };
  const failure = status === "failed" || status === "timed_out";
  return <Badge className={failure ? "border-destructive/40 bg-destructive/10 text-destructive" : undefined}>{label[status]}</Badge>;
}

export function ExperimentDetail({ item }: Readonly<{ item: Experiment }>) {
  return <Card><CardHeader><CardTitle>{item.name}</CardTitle><CardDescription>{item.experiment_id}</CardDescription></CardHeader><CardContent className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-2 text-xs"><span className="text-muted-foreground">结果目录</span><code className="break-all">{item.result_directory}</code><span className="text-muted-foreground">项目时区</span><span>{item.project_timezone}</span><span className="text-muted-foreground">Runtime</span><span>{item.actual_runtime ? `${item.actual_runtime} ${item.runtime_version ?? ""}` : item.requested_runtime_policy}</span><span className="text-muted-foreground">结果 Commit</span><code className="break-all">{item.result_commit_sha ?? "尚未绑定"}</code>{item.result_contract ? <><span className="text-muted-foreground">自行运行说明</span><span className="whitespace-pre-wrap">{item.result_contract.instructions}</span></> : null}</CardContent></Card>;
}

export function ExperimentTerminal({
  item,
  logs,
}: Readonly<{ item: Experiment; logs: ExperimentLog[] }>) {
  return (
    <Card>
      <CardHeader><CardTitle className="flex items-center gap-2"><Terminal className="size-4" />只读 Terminal</CardTitle><CardDescription>实时输出和完整历史日志；Terminal 不提供远程 Shell。</CardDescription></CardHeader>
      <CardContent>
        {item.experiment_type === "self" ? (
          <p className="text-sm text-muted-foreground">自行运行类型不采集托管日志。Coding Agent push 后通过 MCP 绑定 Commit。</p>
        ) : (
          <>
            {item.logs_truncated ? <p className="mb-3 text-xs text-amber-600">Box 磁盘不足，新增日志已停止保存；实验仍会继续运行。</p> : null}
            <pre className="max-h-[32rem] overflow-auto rounded-lg bg-zinc-950 p-4 font-mono text-xs leading-5 text-zinc-200">{logs.slice().sort((a, b) => a.sequence - b.sequence).map((log) => `[${log.sequence}] ${log.stream}> ${log.message}`).join("\n") || "暂无日志"}</pre>
          </>
        )}
      </CardContent>
    </Card>
  );
}

export function ResultPanel({ current, files }: Readonly<{ current: Experiment; files: ResultFile[] }>) {
  const grouped = useMemo(() => groupFiles(files), [files]);
  const complete = current.execution_status === "succeeded" || current.execution_status === "archived";
  return (
    <Card>
      <CardHeader><CardTitle className="flex items-center gap-2"><FileArchive className="size-4" />结果文件树</CardTitle><CardDescription>result 分支只读视图；Artifact 文件以虚拟指针呈现。</CardDescription></CardHeader>
      <CardContent>
        {complete ? (
          <div className="space-y-3">
            {grouped.map(([directory, entries]) => (
              <section key={directory}>
                <p className="mb-1 font-mono text-xs font-semibold">{directory || "/"}</p>
                <ul className="space-y-1">
                  {entries.map((file) => <li className="flex items-center justify-between gap-3 rounded-md border px-2 py-1.5 text-xs" key={file.path}><span className="truncate">{file.path.split("/").at(-1)}</span><Badge className="bg-background">{file.storage_kind}</Badge></li>)}
                </ul>
              </section>
            ))}
            {!files.length ? <p className="text-sm text-muted-foreground">结果已绑定，暂无可展示文件。</p> : null}
          </div>
        ) : <p className="text-sm text-muted-foreground">实验完成并绑定 result commit 后显示文件树。</p>}
      </CardContent>
    </Card>
  );
}

export function ComparisonPanel({ items, selectedCount }: Readonly<{ items: Experiment[]; selectedCount: number }>) {
  return <Card><CardHeader><CardTitle>实验比较</CardTitle><CardDescription>{selectedCount < 2 ? "至少选择两条实验记录。" : `正在比较 ${selectedCount} 条实验记录。`}</CardDescription></CardHeader>{items.length >= 2 ? <CardContent className="overflow-x-auto"><table className="w-full min-w-2xl text-left text-sm"><thead><tr className="border-b"><th className="p-2">实验</th><th className="p-2">状态</th><th className="p-2">Runtime</th><th className="p-2">Commit</th><th className="p-2">结果</th></tr></thead><tbody>{items.map((item) => <tr className="border-b last:border-0" key={item.experiment_id}><td className="p-2 font-medium"><Link className="hover:underline" href={`/projects/${encodeURIComponent(item.project_id)}/experiments/${encodeURIComponent(item.experiment_id)}`}>{item.name}</Link></td><td className="p-2">{item.execution_status}</td><td className="p-2">{item.actual_runtime ?? item.requested_runtime_policy}</td><td className="p-2 font-mono text-xs">{item.source_commit.slice(0, 12)}</td><td className="p-2">{item.summary ?? item.result_commit_sha?.slice(0, 12) ?? "—"}</td></tr>)}</tbody></table></CardContent> : null}</Card>;
}

function groupFiles(files: ResultFile[]): [string, ResultFile[]][] {
  const groups = new Map<string, ResultFile[]>();
  for (const file of files) {
    const parts = file.path.split("/");
    const directory = parts.slice(0, -1).join("/");
    groups.set(directory, [...(groups.get(directory) ?? []), file]);
  }
  return [...groups.entries()].sort(([left], [right]) => left.localeCompare(right));
}

export function LabeledSelect({ children, disabled, label, onChange, value }: Readonly<{ children: ReactNode; disabled?: boolean; label: string; onChange: (value: string) => void; value: string }>) {
  return <label className="grid gap-1.5 text-xs font-medium text-muted-foreground">{label}<select className="h-9 rounded-md border border-input bg-background px-3 text-sm text-foreground disabled:opacity-50" disabled={disabled} onChange={(event) => onChange(event.target.value)} value={value}>{children}</select></label>;
}
