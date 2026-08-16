"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CheckSquare2,
  FileArchive,
  FlaskConical,
  GitCompareArrows,
  Play,
  RotateCcw,
  Square,
  Terminal,
} from "lucide-react";
import { type ReactNode, useMemo, useState } from "react";

import { useCurrentProject } from "@/components/providers/project-provider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";

import { experimentApi } from "./api";
import type { Experiment, ExperimentStatus, ResultFile, RuntimePolicy } from "./types";

const activeStatuses: ExperimentStatus[] = [
  "queued",
  "preparing",
  "running",
  "uploading",
  "processing_result",
  "verifying_result",
];
const terminalStatuses: ExperimentStatus[] = ["succeeded", "failed", "canceled", "timed_out", "archived"];
const rerunnableStatuses: ExperimentStatus[] = ["failed", "canceled", "timed_out"];

export function ExperimentWorkbench() {
  const project = useCurrentProject();
  const client = useQueryClient();
  const experiments = useQuery({
    queryFn: () => experimentApi.list(project.id),
    queryKey: ["experiments", project.id],
    refetchInterval: 2_000,
  });
  const boxes = useQuery({
    queryFn: () => experimentApi.projectBoxes(project.id),
    queryKey: ["project-boxes", project.id],
    refetchInterval: 5_000,
  });
  const [selectedId, setSelectedId] = useState<string>();
  const [compareIds, setCompareIds] = useState<string[]>([]);
  const [compareMode, setCompareMode] = useState(false);
  const [name, setName] = useState("");
  const [commit, setCommit] = useState("");
  const [entrypoint, setEntrypoint] = useState("python:run.py");
  const [experimentType, setExperimentType] = useState<"box" | "self">("box");
  const [runtimePolicy, setRuntimePolicy] = useState<RuntimePolicy>("auto");
  const [requestedBoxId, setRequestedBoxId] = useState("");
  const [error, setError] = useState<string>();

  const current = experiments.data?.items.find((item) => item.experiment_id === selectedId)
    ?? experiments.data?.items[0];
  const refresh = async () => {
    await client.invalidateQueries({ queryKey: ["experiments", project.id] });
  };
  const create = useMutation({
    mutationFn: () => experimentApi.create(project.id, {
      entrypoint: entrypoint.trim(),
      environment: {},
      experiment_type: experimentType,
      idempotency_key: crypto.randomUUID(),
      inputs: {},
      name: name.trim(),
      parameters: {},
      requested_box_id: experimentType === "box" && requestedBoxId ? requestedBoxId : undefined,
      runtime_policy: experimentType === "box" ? runtimePolicy : undefined,
      source_commit: commit.trim(),
    }),
    onError: (value) => setError(value.message),
    onSuccess: async (item) => {
      setError(undefined);
      setName("");
      setSelectedId(item.experiment_id);
      await refresh();
    },
  });
  const action = useMutation({
    mutationFn: (input: { id: string; kind: "run" | "cancel" | "rerun" }) => {
      if (input.kind === "run") return experimentApi.run(project.id, input.id);
      if (input.kind === "rerun") return experimentApi.rerun(project.id, input.id, {});
      return experimentApi.cancel(project.id, input.id);
    },
    onError: (value) => setError(value.message),
    onSuccess: async (item) => {
      setError(undefined);
      setSelectedId(item.experiment_id);
      await refresh();
    },
  });
  const logs = useQuery({
    enabled: Boolean(current?.experiment_id && current.experiment_type !== "self"),
    queryFn: () => experimentApi.logs(project.id, current!.experiment_id),
    queryKey: ["experiment-logs", project.id, current?.experiment_id],
    refetchInterval: current && activeStatuses.includes(current.execution_status) ? 2_000 : false,
  });
  const result = useQuery({
    enabled: Boolean(current?.experiment_id && ["processing_result", "succeeded", "archived"].includes(current.execution_status)),
    queryFn: () => experimentApi.result(project.id, current!.experiment_id),
    queryKey: ["experiment-result", project.id, current?.experiment_id],
    retry: false,
  });
  const comparison = useQuery({
    enabled: compareMode && compareIds.length >= 2,
    queryFn: () => experimentApi.compare(project.id, compareIds),
    queryKey: ["experiment-compare", project.id, ...compareIds],
  });

  function toggleCompare(id: string) {
    setCompareIds((ids) => ids.includes(id) ? ids.filter((value) => value !== id) : [...ids, id]);
  }

  return (
    <section aria-labelledby="experiments-title" className="space-y-6">
      <header>
        <div className="mb-3 flex size-10 items-center justify-center rounded-lg border border-border bg-card shadow-xs">
          <FlaskConical className="size-5" />
        </div>
        <h1 className="text-2xl font-semibold" id="experiments-title">实验</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          冻结 Commit 和运行配置后交给 Box，或登记 Coding Agent 自行运行并 push 的结果。
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>新建实验</CardTitle>
          <CardDescription>Runtime 策略和 Box 只在托管运行时生效，确认运行后配置不可修改。</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          <Input aria-label="实验名称" placeholder="实验名称" value={name} onChange={(event) => setName(event.target.value)} />
          <Input aria-label="完整 Commit SHA" placeholder="完整 Commit SHA" value={commit} onChange={(event) => setCommit(event.target.value)} />
          <Input aria-label="固定入口" placeholder="python:run.py" value={entrypoint} onChange={(event) => setEntrypoint(event.target.value)} />
          <LabeledSelect label="运行方式" value={experimentType} onChange={(value) => setExperimentType(value as "box" | "self")}>
            <option value="box">Box 托管运行</option>
            <option value="self">Coding Agent 自行运行</option>
          </LabeledSelect>
          <LabeledSelect disabled={experimentType === "self"} label="Runtime 策略" value={runtimePolicy} onChange={(value) => setRuntimePolicy(value as RuntimePolicy)}>
            <option value="auto">自动（E2B 优先）</option>
            <option value="e2b">仅 E2B</option>
            <option value="local-docker">仅 Local Docker</option>
          </LabeledSelect>
          <LabeledSelect disabled={experimentType === "self"} label="指定 Box（可选）" value={requestedBoxId} onChange={setRequestedBoxId}>
            <option value="">最低负载自动选择</option>
            {boxes.data?.items.map((box) => <option key={box.box_id} value={box.box_id}>{box.name} · {box.status}</option>)}
          </LabeledSelect>
          <Button
            className="md:col-span-2 xl:col-span-3"
            disabled={!name.trim() || !/^[0-9a-f]{40}([0-9a-f]{24})?$/.test(commit) || create.isPending}
            onClick={() => create.mutate()}
          >
            <FlaskConical className="size-4" />{create.isPending ? "创建中…" : "创建冻结实验"}
          </Button>
        </CardContent>
      </Card>

      {error ? <p className="text-sm text-destructive" role="alert">{error}</p> : null}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">{experiments.data?.items.length ?? 0} 条实验记录</p>
        <Button onClick={() => { setCompareMode((value) => !value); setCompareIds([]); }} size="sm" variant={compareMode ? "default" : "outline"}>
          <GitCompareArrows className="size-4" />{compareMode ? "退出比较" : "比较实验"}
        </Button>
      </div>

      {compareMode ? (
        <ComparisonPanel items={comparison.data?.items ?? []} selectedCount={compareIds.length} />
      ) : null}

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_28rem]">
        <div className="space-y-3">
          {experiments.data?.items.map((item) => (
            <ExperimentCard
              checked={compareIds.includes(item.experiment_id)}
              compareMode={compareMode}
              item={item}
              key={item.experiment_id}
              onCancel={() => action.mutate({ id: item.experiment_id, kind: "cancel" })}
              onCompare={() => toggleCompare(item.experiment_id)}
              onRerun={() => action.mutate({ id: item.experiment_id, kind: "rerun" })}
              onRun={() => action.mutate({ id: item.experiment_id, kind: "run" })}
              onSelect={() => setSelectedId(item.experiment_id)}
              selected={current?.experiment_id === item.experiment_id}
            />
          ))}
          {!experiments.isLoading && !experiments.data?.items.length ? (
            <p className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">尚无实验记录。</p>
          ) : null}
        </div>

        <aside className="space-y-4">
          {current ? <ExperimentDetail item={current} /> : null}
          <Card>
            <CardHeader><CardTitle className="flex items-center gap-2"><Terminal className="size-4" />只读 Terminal</CardTitle></CardHeader>
            <CardContent>
              {current?.experiment_type === "self" ? (
                <p className="text-sm text-muted-foreground">自行运行类型不采集托管日志。Coding Agent push 后通过 MCP 绑定 Commit。</p>
              ) : (
                <>
                  {current?.logs_truncated ? <p className="mb-3 text-xs text-amber-600">Box 磁盘不足，新增日志已停止保存；实验仍会继续运行。</p> : null}
                  <pre className="max-h-80 overflow-auto rounded-lg bg-zinc-950 p-4 font-mono text-xs leading-5 text-zinc-200">
                    {logs.data?.items.slice().sort((a, b) => a.sequence - b.sequence).map((log) => `[${log.sequence}] ${log.stream}> ${log.message}`).join("\n") || "选择一个 Box 实验后显示完整历史日志"}
                  </pre>
                </>
              )}
            </CardContent>
          </Card>
          <ResultPanel current={current} files={result.data?.files ?? []} />
        </aside>
      </div>
    </section>
  );
}

function LabeledSelect({ children, disabled, label, onChange, value }: Readonly<{ children: ReactNode; disabled?: boolean; label: string; onChange: (value: string) => void; value: string }>) {
  return <label className="grid gap-1.5 text-xs font-medium text-muted-foreground">{label}<select className="h-9 rounded-md border border-input bg-background px-3 text-sm text-foreground disabled:opacity-50" disabled={disabled} onChange={(event) => onChange(event.target.value)} value={value}>{children}</select></label>;
}

function ExperimentCard({ checked, compareMode, item, onCancel, onCompare, onRerun, onRun, onSelect, selected }: Readonly<{ checked: boolean; compareMode: boolean; item: Experiment; onCancel: () => void; onCompare: () => void; onRerun: () => void; onRun: () => void; onSelect: () => void; selected: boolean }>) {
  const terminal = terminalStatuses.includes(item.execution_status);
  return (
    <article className={`rounded-lg border p-4 transition-colors ${selected ? "border-foreground/50" : "border-border hover:border-foreground/25"}`}>
      <button className="block w-full text-left" onClick={onSelect} type="button">
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
        {item.retry.warning_code ? <p className="mt-2 text-xs text-amber-600">已有更新的重跑记录：{item.retry.latest_experiment_id}</p> : null}
      </button>
      <div className="mt-3 flex flex-wrap gap-2">
        {item.execution_status === "created" ? <Button onClick={onRun} size="sm"><Play className="size-3.5" />确认运行</Button> : null}
        {!terminal && item.execution_status !== "created" && item.execution_status !== "awaiting_result" ? <Button onClick={onCancel} size="sm" variant="outline"><Square className="size-3.5" />取消</Button> : null}
        {rerunnableStatuses.includes(item.execution_status) && item.experiment_type !== "self" ? <Button onClick={onRerun} size="sm" variant="outline"><RotateCcw className="size-3.5" />创建重跑</Button> : null}
        {compareMode ? <Button onClick={onCompare} size="sm" variant={checked ? "default" : "outline"}><CheckSquare2 className="size-3.5" />{checked ? "已选择" : "加入比较"}</Button> : null}
      </div>
    </article>
  );
}

function StatusBadge({ status }: Readonly<{ status: ExperimentStatus }>) {
  const label: Record<ExperimentStatus, string> = {
    archived: "已归档", awaiting_result: "等待结果", canceled: "已取消", created: "待确认", failed: "失败",
    preparing: "准备中", processing_result: "处理结果", queued: "排队中", running: "运行中", succeeded: "已完成",
    timed_out: "超时", uploading: "上传结果", verifying_result: "验证结果",
  };
  const failure = status === "failed" || status === "timed_out";
  return <Badge className={failure ? "border-destructive/40 bg-destructive/10 text-destructive" : undefined}>{label[status]}</Badge>;
}

function ExperimentDetail({ item }: Readonly<{ item: Experiment }>) {
  return <Card><CardHeader><CardTitle>{item.name}</CardTitle><CardDescription>{item.experiment_id}</CardDescription></CardHeader><CardContent className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-2 text-xs"><span className="text-muted-foreground">结果目录</span><code className="break-all">{item.result_directory}</code><span className="text-muted-foreground">项目时区</span><span>{item.project_timezone}</span><span className="text-muted-foreground">Runtime</span><span>{item.actual_runtime ? `${item.actual_runtime} ${item.runtime_version ?? ""}` : item.requested_runtime_policy}</span><span className="text-muted-foreground">结果 Commit</span><code className="break-all">{item.result_commit_sha ?? "尚未绑定"}</code>{item.result_contract ? <><span className="text-muted-foreground">自行运行说明</span><span className="whitespace-pre-wrap">{item.result_contract.instructions}</span></> : null}</CardContent></Card>;
}

function ResultPanel({ current, files }: Readonly<{ current?: Experiment; files: ResultFile[] }>) {
  const grouped = useMemo(() => groupFiles(files), [files]);
  const complete = current?.execution_status === "succeeded" || current?.execution_status === "archived";
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2"><FileArchive className="size-4" />结果文件树</CardTitle>
        <CardDescription>result 分支只读视图；Artifact 文件以虚拟指针呈现。</CardDescription>
      </CardHeader>
      <CardContent>
        {complete ? (
          <div className="space-y-3">
            {grouped.map(([directory, entries]) => (
              <section key={directory}>
                <p className="mb-1 font-mono text-xs font-semibold">{directory || "/"}</p>
                <ul className="space-y-1">
                  {entries.map((file) => (
                    <li className="flex items-center justify-between gap-3 rounded-md border px-2 py-1.5 text-xs" key={file.path}>
                      <span className="truncate">{file.path.split("/").at(-1)}</span>
                      <Badge className="bg-background">{file.storage_kind}</Badge>
                    </li>
                  ))}
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

function ComparisonPanel({ items, selectedCount }: Readonly<{ items: Experiment[]; selectedCount: number }>) {
  return <Card><CardHeader><CardTitle>实验比较</CardTitle><CardDescription>{selectedCount < 2 ? "至少选择两条实验记录。" : `正在比较 ${selectedCount} 条实验记录。`}</CardDescription></CardHeader>{items.length >= 2 ? <CardContent className="overflow-x-auto"><table className="w-full min-w-2xl text-left text-sm"><thead><tr className="border-b"><th className="p-2">实验</th><th className="p-2">状态</th><th className="p-2">Runtime</th><th className="p-2">Commit</th><th className="p-2">结果</th></tr></thead><tbody>{items.map((item) => <tr className="border-b last:border-0" key={item.experiment_id}><td className="p-2 font-medium">{item.name}</td><td className="p-2">{item.execution_status}</td><td className="p-2">{item.actual_runtime ?? item.requested_runtime_policy}</td><td className="p-2 font-mono text-xs">{item.source_commit.slice(0, 12)}</td><td className="p-2">{item.summary ?? item.result_commit_sha?.slice(0, 12) ?? "—"}</td></tr>)}</tbody></table></CardContent> : null}</Card>;
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
