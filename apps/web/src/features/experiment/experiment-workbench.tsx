"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FlaskConical, GitCompareArrows, Plus, X } from "lucide-react";
import { useState } from "react";

import { useCurrentProject } from "@/components/providers/project-provider";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";

import { experimentApi } from "./api";
import {
  ComparisonPanel,
  ExperimentCard,
  LabeledSelect,
} from "./experiment-ui";
import type { RuntimePolicy } from "./types";

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
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [compareIds, setCompareIds] = useState<string[]>([]);
  const [compareMode, setCompareMode] = useState(false);
  const [name, setName] = useState("");
  const [commit, setCommit] = useState("");
  const [entrypoint, setEntrypoint] = useState("python:run.py");
  const [experimentType, setExperimentType] = useState<"box" | "self">("box");
  const [runtimePolicy, setRuntimePolicy] = useState<RuntimePolicy>("auto");
  const [requestedBoxId, setRequestedBoxId] = useState("");
  const [error, setError] = useState<string>();

  const refresh = async () => {
    await client.invalidateQueries({ queryKey: ["experiments", project.id] });
  };
  const create = useMutation({
    mutationFn: () =>
      experimentApi.create(project.id, {
        entrypoint: entrypoint.trim(),
        environment: {},
        experiment_type: experimentType,
        idempotency_key: crypto.randomUUID(),
        inputs: {},
        name: name.trim(),
        parameters: {},
        requested_box_id:
          experimentType === "box" && requestedBoxId
            ? requestedBoxId
            : undefined,
        runtime_policy: experimentType === "box" ? runtimePolicy : undefined,
        source_commit: commit.trim(),
      }),
    onError: (value) => setError(value.message),
    onSuccess: async () => {
      setError(undefined);
      setName("");
      setIsCreateOpen(false);
      await refresh();
    },
  });
  const action = useMutation({
    mutationFn: (input: { id: string; kind: "run" | "cancel" | "rerun" }) => {
      if (input.kind === "run") return experimentApi.run(project.id, input.id);
      if (input.kind === "rerun")
        return experimentApi.rerun(project.id, input.id, {});
      return experimentApi.cancel(project.id, input.id);
    },
    onError: (value) => setError(value.message),
    onSuccess: async () => {
      setError(undefined);
      await refresh();
    },
  });
  const comparison = useQuery({
    enabled: compareMode && compareIds.length >= 2,
    queryFn: () => experimentApi.compare(project.id, compareIds),
    queryKey: ["experiment-compare", project.id, ...compareIds],
  });

  function toggleCompare(id: string) {
    setCompareIds((ids) =>
      ids.includes(id) ? ids.filter((value) => value !== id) : [...ids, id],
    );
  }

  return (
    <section aria-labelledby="experiments-title" className="space-y-6">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="mb-3 flex size-10 items-center justify-center rounded-lg border border-border bg-card shadow-xs">
            <FlaskConical className="size-5" />
          </div>
          <h1 className="text-2xl font-semibold" id="experiments-title">
            实验
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            实验列表以卡片展示状态和进度；点击卡片进入独立详情页查看 Terminal
            与结果文件树。
          </p>
        </div>
        <Button onClick={() => setIsCreateOpen(true)}>
          <Plus className="size-4" />
          新建实验
        </Button>
      </header>

      {isCreateOpen && (
        <div
          aria-labelledby="create-experiment-title"
          aria-modal="true"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
          role="dialog"
        >
          <Card className="w-full max-w-md shadow-xl animate-in fade-in-50 zoom-in-95 duration-150">
            <CardHeader className="relative pr-10">
              <CardTitle id="create-experiment-title">新建实验</CardTitle>
              <CardDescription>
                Runtime 策略和 Box 只在托管运行时生效，确认运行后配置不可修改。
              </CardDescription>
              <Button
                aria-label="关闭弹窗"
                className="absolute right-4 top-4 size-8 p-0"
                onClick={() => setIsCreateOpen(false)}
                variant="ghost"
              >
                <X className="size-4" />
              </Button>
            </CardHeader>
            <CardContent className="space-y-4">
              <label className="grid gap-1.5 text-sm font-medium">
                实验名称
                <Input
                  aria-label="实验名称"
                  placeholder="实验名称"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                />
              </label>
              <label className="grid gap-1.5 text-sm font-medium">
                完整 Commit SHA
                <Input
                  aria-label="完整 Commit SHA"
                  placeholder="完整 Commit SHA"
                  value={commit}
                  onChange={(event) => setCommit(event.target.value)}
                />
              </label>
              <label className="grid gap-1.5 text-sm font-medium">
                固定入口
                <Input
                  aria-label="固定入口"
                  placeholder="python:run.py"
                  value={entrypoint}
                  onChange={(event) => setEntrypoint(event.target.value)}
                />
              </label>
              <LabeledSelect
                label="运行方式"
                value={experimentType}
                onChange={(value) => setExperimentType(value as "box" | "self")}
              >
                <option value="box">Box 托管运行</option>
                <option value="self">Coding Agent 自行运行</option>
              </LabeledSelect>
              <LabeledSelect
                disabled={experimentType === "self"}
                label="Runtime 策略"
                value={runtimePolicy}
                onChange={(value) => setRuntimePolicy(value as RuntimePolicy)}
              >
                <option value="auto">自动（E2B 优先）</option>
                <option value="e2b">仅 E2B</option>
                <option value="local-docker">仅 Local Docker</option>
                <option value="local-process">仅 Local Process（裸机）</option>
              </LabeledSelect>
              {experimentType === "box" && runtimePolicy === "local-process" ? (
                <p
                  className="rounded-md bg-amber-500/10 p-3 text-xs text-amber-700"
                  role="note"
                >
                  Local Process 直接在 Box
                  宿主机上以裸机进程运行任务：没有容器隔离，任务可访问宿主机文件与网络，仅适合完全信任的
                  Box 与代码（trusted-host）。该 Runtime
                  只接受网络策略为「允许」的实验，其他网络策略会在准备阶段被拒绝（LIMITS_NOT_ENFORCEABLE）。
                </p>
              ) : null}
              <LabeledSelect
                disabled={experimentType === "self"}
                label="指定 Box（可选）"
                value={requestedBoxId}
                onChange={setRequestedBoxId}
              >
                <option value="">最低负载自动选择</option>
                {boxes.data?.items.map((box) => (
                  <option key={box.box_id} value={box.box_id}>
                    {box.name} · {box.status}
                  </option>
                ))}
              </LabeledSelect>
            </CardContent>
            <CardFooter className="flex justify-end gap-2 pt-0">
              <Button onClick={() => setIsCreateOpen(false)} variant="outline">
                取消
              </Button>
              <Button
                disabled={
                  !name.trim() ||
                  !/^[0-9a-f]{40}([0-9a-f]{24})?$/.test(commit) ||
                  create.isPending
                }
                onClick={() => create.mutate()}
              >
                {create.isPending ? "创建中…" : "创建冻结实验"}
              </Button>
            </CardFooter>
          </Card>
        </div>
      )}

      {error ? (
        <p className="text-sm text-destructive" role="alert">
          {error}
        </p>
      ) : null}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">
          {experiments.data?.items.length ?? 0} 条实验记录
        </p>
        <Button
          onClick={() => {
            setCompareMode((value) => !value);
            setCompareIds([]);
          }}
          size="sm"
          variant={compareMode ? "default" : "outline"}
        >
          <GitCompareArrows className="size-4" />
          {compareMode ? "退出比较" : "比较实验"}
        </Button>
      </div>
      {compareMode ? (
        <ComparisonPanel
          items={comparison.data?.items ?? []}
          selectedCount={compareIds.length}
        />
      ) : null}

      <div className="space-y-3">
        {experiments.data?.items.map((item) => (
          <ExperimentCard
            checked={compareIds.includes(item.experiment_id)}
            compareMode={compareMode}
            item={item}
            key={item.experiment_id}
            onCancel={() =>
              action.mutate({ id: item.experiment_id, kind: "cancel" })
            }
            onCompare={() => toggleCompare(item.experiment_id)}
            onRerun={() =>
              action.mutate({ id: item.experiment_id, kind: "rerun" })
            }
            onRun={() => action.mutate({ id: item.experiment_id, kind: "run" })}
          />
        ))}
        {!experiments.isLoading && !experiments.data?.items.length ? (
          <p className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">
            尚无实验记录。
          </p>
        ) : null}
      </div>
    </section>
  );
}
