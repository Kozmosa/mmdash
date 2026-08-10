"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Box as BoxIcon, FlaskConical, Play, Square, Terminal } from "lucide-react";
import { useState } from "react";

import { useCurrentProject } from "@/components/providers/project-provider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";

import { experimentApi } from "./api";
import type { Experiment } from "./types";

export function ExperimentWorkbench() {
  const project = useCurrentProject();
  const client = useQueryClient();
  const experiments = useQuery({ queryKey: ["experiments", project.id], queryFn: () => experimentApi.list(project.id), refetchInterval: 2_000 });
  const boxes = useQuery({ queryKey: ["boxes", project.id], queryFn: () => experimentApi.boxes(project.id), refetchInterval: 5_000 });
  const [selected, setSelected] = useState<string>();
  const [name, setName] = useState("");
  const [commit, setCommit] = useState("");
  const [entrypoint, setEntrypoint] = useState("python:run.py");
  const [error, setError] = useState<string>();
  const refresh = () => client.invalidateQueries({ queryKey: ["experiments", project.id] });
  const create = useMutation({ mutationFn: () => experimentApi.create(project.id, { name: name.trim(), source_commit: commit.trim(), entrypoint: entrypoint.trim(), parameters: {}, environment: {}, inputs: {}, runtime: "local-docker", limits: { cpu_millis: 1000, memory_bytes: 1_073_741_824, timeout_seconds: 300, disk_bytes: 1_073_741_824, pids: 128, network: "disabled" }, idempotency_key: crypto.randomUUID() }), onSuccess: async (item) => { setName(""); setSelected(item.experiment_id); await refresh(); }, onError: (value) => setError(value.message) });
  const action = useMutation({ mutationFn: (input: { id: string; kind: "run" | "cancel" }) => input.kind === "run" ? experimentApi.run(project.id, input.id) : experimentApi.cancel(project.id, input.id), onSuccess: refresh, onError: (value) => setError(value.message) });
  const current = experiments.data?.items.find((item) => item.experiment_id === selected) ?? experiments.data?.items[0];
  const logs = useQuery({ queryKey: ["experiment-logs", project.id, current?.experiment_id], queryFn: () => experimentApi.logs(project.id, current!.experiment_id), enabled: Boolean(current?.experiment_id), refetchInterval: current && ["queued", "preparing", "running"].includes(current.status) ? 2_000 : false });

  return <section className="space-y-6" aria-labelledby="experiments-title">
    <header><div className="mb-3 flex size-10 items-center justify-center rounded-lg border border-border bg-card shadow-xs"><FlaskConical className="size-5" /></div><h1 className="text-2xl font-semibold" id="experiments-title">求解记录</h1><p className="mt-1 text-sm text-muted-foreground">冻结 Commit、入口和参数后，在受限 Box Sandbox 中运行并归档 artifact.zip。</p></header>
    <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
      <div className="space-y-4">
        <Card><CardHeader><CardTitle>新建实验</CardTitle></CardHeader><CardContent className="grid gap-3 sm:grid-cols-3"><Input aria-label="实验名称" placeholder="实验名称" value={name} onChange={(event) => setName(event.target.value)} /><Input aria-label="完整 Commit SHA" placeholder="完整 Commit SHA" value={commit} onChange={(event) => setCommit(event.target.value)} /><Input aria-label="固定入口" placeholder="python:run.py" value={entrypoint} onChange={(event) => setEntrypoint(event.target.value)} /><Button className="sm:col-span-3" disabled={!name.trim() || !/^[0-9a-f]{40}([0-9a-f]{24})?$/.test(commit) || create.isPending} onClick={() => create.mutate()}><FlaskConical className="size-4" />创建冻结实验</Button></CardContent></Card>
        {error ? <p className="text-sm text-destructive">{error}</p> : null}
        <div className="space-y-3">{experiments.data?.items.map((item) => <ExperimentCard key={item.experiment_id} item={item} selected={current?.experiment_id === item.experiment_id} onSelect={() => setSelected(item.experiment_id)} onRun={() => action.mutate({ id: item.experiment_id, kind: "run" })} onCancel={() => action.mutate({ id: item.experiment_id, kind: "cancel" })} />)}</div>
      </div>
      <aside className="space-y-4"><Card><CardHeader><CardTitle className="flex items-center gap-2"><BoxIcon className="size-4" />Box 状态</CardTitle></CardHeader><CardContent className="space-y-3">{boxes.data?.items.length ? boxes.data.items.map((box) => <div className="rounded-md border p-3" key={box.box_id}><div className="flex items-center justify-between gap-2"><span className="font-medium">{box.name}</span><Badge>{box.status}</Badge></div><p className="mt-1 text-xs text-muted-foreground">{box.version} · {box.load.running_tasks}/{box.load.capacity} tasks</p><p className="text-xs text-muted-foreground">{box.runtimes.map((runtime) => runtime.name).join(", ")}</p></div>) : <p className="text-sm text-muted-foreground">尚未绑定 Box。</p>}</CardContent></Card><Card><CardHeader><CardTitle className="flex items-center gap-2"><Terminal className="size-4" />实时日志</CardTitle></CardHeader><CardContent><pre className="max-h-80 overflow-auto whitespace-pre-wrap text-xs leading-5 text-muted-foreground">{logs.data?.items.map((log) => `[${log.level}] ${log.message}`).join("\n") || "选择一个实验后显示日志"}</pre></CardContent></Card></aside>
    </div>
  </section>;
}

function ExperimentCard({ item, selected, onSelect, onRun, onCancel }: Readonly<{ item: Experiment; selected: boolean; onSelect: () => void; onRun: () => void; onCancel: () => void }>) {
  const terminal = ["succeeded", "failed", "canceled", "archived"].includes(item.status);
  return <button className={`block w-full rounded-lg border text-left transition-colors ${selected ? "border-foreground/50" : "border-border hover:border-foreground/25"}`} onClick={onSelect} type="button"><div className="p-4"><div className="flex flex-wrap items-center gap-2"><span className="font-semibold">{item.name}</span><Badge>{item.status}</Badge><span className="ml-auto text-xs text-muted-foreground">{new Date(item.updated_at).toLocaleString()}</span></div><p className="mt-2 text-xs text-muted-foreground">{item.source_commit.slice(0, 12)} · {item.entrypoint} · {item.runtime}</p>{item.failure_message ? <p className="mt-2 text-sm text-destructive">{item.failure_code}: {item.failure_message}</p> : null}<div className="mt-3 flex gap-2">{item.status === "created" ? <Button onClick={(event) => { event.stopPropagation(); onRun(); }} size="sm" type="button"><Play className="size-3.5" />运行</Button> : null}{!terminal && item.status !== "created" ? <Button onClick={(event) => { event.stopPropagation(); onCancel(); }} size="sm" type="button" variant="outline"><Square className="size-3.5" />取消</Button> : null}</div></div></button>;
}
