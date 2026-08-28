"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FlaskConical, Save } from "lucide-react";
import { type Dispatch, type FormEvent, type ReactNode, type SetStateAction, useState } from "react";
import { toast } from "sonner";

import { useCurrentProject } from "@/components/providers/project-provider";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";

import { experimentApi } from "./api";
import type { ExperimentSettings, ResourceLimits, RuntimePolicy } from "./types";

const manageRoles = new Set(["owner", "maintainer"]);

export function ExperimentSettingsPanel() {
  const project = useCurrentProject();
  const settings = useQuery({ queryFn: () => experimentApi.settings(project.id), queryKey: ["experiment-settings", project.id] });
  return (
    <section aria-labelledby="experiment-settings-title" className="space-y-4">
      <div><h2 className="flex items-center gap-2 text-lg font-semibold" id="experiment-settings-title"><FlaskConical className="size-5" />Experiment 设置</h2><p className="mt-1 text-sm text-muted-foreground">配置新实验的项目时区、默认 Runtime、资源限制和 Git 大文件阈值；单次实验可以覆盖默认值。</p></div>
      {settings.isPending ? <Card className="min-h-52 animate-pulse bg-muted/20" /> : null}
      {settings.isError ? <Card><CardHeader><CardTitle className="text-base">无法读取 Experiment 设置</CardTitle></CardHeader></Card> : null}
      {settings.data ? <ExperimentSettingsForm canManage={manageRoles.has(project.role ?? "")} initial={settings.data} projectId={project.id} /> : null}
    </section>
  );
}

function ExperimentSettingsForm({ canManage, initial, projectId }: Readonly<{ canManage: boolean; initial: ExperimentSettings; projectId: string }>) {
  const client = useQueryClient();
  const [timezone, setTimezone] = useState(initial.timezone);
  const [runtime, setRuntime] = useState<RuntimePolicy>(initial.default_runtime_policy);
  const [threshold, setThreshold] = useState(initial.git_large_file_threshold_bytes);
  const [limits, setLimits] = useState(initial.default_limits);
  const save = useMutation({
    mutationFn: () => experimentApi.updateSettings(projectId, { default_limits: limits, default_runtime_policy: runtime, git_large_file_threshold_bytes: threshold, timezone: timezone.trim() }),
    onError: (error) => toast.error(error.message),
    onSuccess: (value) => { client.setQueryData(["experiment-settings", projectId], value); toast.success("Experiment 设置已保存"); },
  });
  function submit(event: FormEvent) { event.preventDefault(); save.mutate(); }
  return (
    <Card><CardHeader><CardTitle className="text-base">默认运行策略</CardTitle><CardDescription>{canManage ? "只影响新建实验；已有实验保留创建时冻结的时区和目录。" : "当前角色可以查看，但不能修改项目级默认值。"}</CardDescription></CardHeader><CardContent><form className="space-y-5" onSubmit={submit}>
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <Field label="IANA 时区"><Input disabled={!canManage} onChange={(event) => setTimezone(event.target.value)} placeholder="Asia/Shanghai" required value={timezone} /></Field>
        <Field label="默认 Runtime"><select className={selectClass} disabled={!canManage} onChange={(event) => setRuntime(event.target.value as RuntimePolicy)} value={runtime}><option value="auto">自动（E2B 优先）</option><option value="e2b">仅 E2B</option><option value="local-docker">仅 Local Docker</option><option value="local-process">仅 Local Process（裸机）</option></select></Field>
        <NumberField disabled={!canManage} label="Git 大文件阈值（bytes）" min={1} onChange={setThreshold} value={threshold} />
      </div>
      {runtime === "local-process" ? (
        <p className="rounded-md bg-amber-500/10 p-3 text-xs text-amber-700" role="note">
          默认 Runtime 设为 Local Process 后，新建实验将直接在 Box 宿主机上以裸机进程运行：没有容器隔离，仅适合完全信任的 Box 与代码（trusted-host）。
        </p>
      ) : null}
      <div><p className="mb-3 text-sm font-medium">默认资源限制</p><div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3"><LimitField disabled={!canManage} field="cpu_millis" label="CPU（millicores）" limits={limits} setLimits={setLimits} /><LimitField disabled={!canManage} field="memory_bytes" label="内存（bytes）" limits={limits} setLimits={setLimits} /><LimitField disabled={!canManage} field="timeout_seconds" label="超时（秒）" limits={limits} setLimits={setLimits} /><LimitField disabled={!canManage} field="disk_bytes" label="磁盘（bytes）" limits={limits} setLimits={setLimits} /><LimitField disabled={!canManage} field="pids" label="进程数" limits={limits} setLimits={setLimits} /><Field label="网络策略"><select className={selectClass} disabled={!canManage} onChange={(event) => setLimits((value) => ({ ...value, network: event.target.value as ResourceLimits["network"] }))} value={limits.network}><option value="disabled">禁用</option><option value="restricted">受限</option><option value="enabled">允许</option></select></Field></div></div>
      {canManage ? <Button disabled={save.isPending} type="submit"><Save className="size-4" />{save.isPending ? "保存中…" : "保存 Experiment 设置"}</Button> : null}
    </form></CardContent></Card>
  );
}

function Field({ children, label }: Readonly<{ children: ReactNode; label: string }>) { return <label className="grid gap-1.5 text-sm font-medium"><span>{label}</span>{children}</label>; }
function NumberField({ disabled, label, min, onChange, value }: Readonly<{ disabled: boolean; label: string; min: number; onChange: (value: number) => void; value: number }>) { return <Field label={label}><Input disabled={disabled} min={min} onChange={(event) => onChange(Number(event.target.value))} required type="number" value={value} /></Field>; }
function LimitField({ disabled, field, label, limits, setLimits }: Readonly<{ disabled: boolean; field: Exclude<keyof ResourceLimits, "network">; label: string; limits: ResourceLimits; setLimits: Dispatch<SetStateAction<ResourceLimits>> }>) { return <NumberField disabled={disabled} label={label} min={1} onChange={(number) => setLimits((value) => ({ ...value, [field]: number }))} value={limits[field]} />; }
const selectClass = "flex h-9 w-full rounded-md border border-input bg-background px-3 text-sm disabled:opacity-50";
