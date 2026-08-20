"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Play, RotateCcw, Square } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { useCurrentProject } from "@/components/providers/project-provider";
import { Button } from "@/components/ui/button";

import { experimentApi } from "./api";
import {
  activeStatuses,
  ExecutionProgress,
  ExperimentDetail,
  ExperimentTerminal,
  rerunnableStatuses,
  ResultPanel,
  StatusBadge,
  terminalStatuses,
} from "./experiment-ui";

export function ExperimentDetailWorkbench({
  experimentId,
}: Readonly<{ experimentId: string }>) {
  const project = useCurrentProject();
  const client = useQueryClient();
  const [error, setError] = useState<string>();
  const experiment = useQuery({
    enabled: Boolean(experimentId),
    queryFn: () => experimentApi.get(project.id, experimentId),
    queryKey: ["experiment", project.id, experimentId],
    refetchInterval: 2_000,
  });
  const item = experiment.data;
  const action = useMutation({
    mutationFn: (kind: "run" | "cancel" | "rerun") => {
      if (kind === "run") return experimentApi.run(project.id, experimentId);
      if (kind === "rerun")
        return experimentApi.rerun(project.id, experimentId, {});
      return experimentApi.cancel(project.id, experimentId);
    },
    onError: (value) => setError(value.message),
    onSuccess: async () => {
      setError(undefined);
      await client.invalidateQueries({
        queryKey: ["experiment", project.id, experimentId],
      });
      await client.invalidateQueries({ queryKey: ["experiments", project.id] });
    },
  });
  const logs = useQuery({
    enabled: Boolean(item?.experiment_id && item.experiment_type !== "self"),
    queryFn: () => experimentApi.logs(project.id, item!.experiment_id),
    queryKey: ["experiment-logs", project.id, item?.experiment_id],
    refetchInterval:
      item && activeStatuses.includes(item.execution_status) ? 2_000 : false,
  });
  const result = useQuery({
    enabled: Boolean(
      item?.experiment_id &&
      ["processing_result", "succeeded", "archived"].includes(
        item.execution_status,
      ),
    ),
    queryFn: () => experimentApi.result(project.id, item!.experiment_id),
    queryKey: ["experiment-result", project.id, item?.experiment_id],
    retry: false,
  });

  if (experiment.isLoading) {
    return <p className="text-sm text-muted-foreground">正在读取实验…</p>;
  }
  if (!item) {
    return <p className="text-sm text-destructive">实验不存在或无权查看。</p>;
  }

  const terminal = terminalStatuses.includes(item.execution_status);
  return (
    <section aria-labelledby="experiment-detail-title" className="space-y-6">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <Link
            className="mb-4 inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
            href={`/projects/${encodeURIComponent(project.id)}/experiments`}
          >
            <ArrowLeft className="size-4" />
            返回实验列表
          </Link>
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-2xl font-semibold" id="experiment-detail-title">
              {item.name}
            </h1>
            <StatusBadge status={item.execution_status} />
            {item.connectivity_status === "box_offline" ? (
              <span className="text-sm text-amber-700">
                Box 离线（实验继续执行）
              </span>
            ) : null}
          </div>
          <p className="mt-1 break-all font-mono text-xs text-muted-foreground">
            {item.experiment_id}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {item.execution_status === "created" ? (
            <Button
              disabled={action.isPending}
              onClick={() => action.mutate("run")}
              size="sm"
            >
              <Play className="size-3.5" />
              确认运行
            </Button>
          ) : null}
          {!terminal &&
          item.execution_status !== "created" &&
          item.execution_status !== "awaiting_result" ? (
            <Button
              disabled={action.isPending}
              onClick={() => action.mutate("cancel")}
              size="sm"
              variant="outline"
            >
              <Square className="size-3.5" />
              取消
            </Button>
          ) : null}
          {rerunnableStatuses.includes(item.execution_status) &&
          item.experiment_type !== "self" ? (
            <Button
              disabled={action.isPending}
              onClick={() => action.mutate("rerun")}
              size="sm"
              variant="outline"
            >
              <RotateCcw className="size-3.5" />
              创建重跑
            </Button>
          ) : null}
        </div>
      </header>
      {error ? (
        <p className="text-sm text-destructive" role="alert">
          {error}
        </p>
      ) : null}
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_28rem]">
        <div className="space-y-4">
          <ExperimentDetail item={item} />
          <ExperimentTerminal item={item} logs={logs.data?.items ?? []} />
        </div>
        <ExecutionProgress item={item} />
      </div>
      <ResultPanel
        current={item}
        files={result.data?.files ?? []}
        projectId={project.id}
      />
    </section>
  );
}
