"use client";

import { useQuery } from "@tanstack/react-query";
import { CalendarClock } from "lucide-react";

import { useCurrentProject } from "@/components/providers/project-provider";
import { ErrorState } from "@/components/states/error-state";
import { LoadingState } from "@/components/states/loading-state";
import { ProgressWorkbench } from "@/features/progress/progress-workbench";
import type { ProgressAggregate } from "@/features/progress/types";
import { apiClient } from "@/lib/api-client";

const manageRoles = new Set(["owner", "maintainer", "editor"]);
const evaluateRoles = new Set(["owner", "maintainer", "editor", "viewer"]);

export default function ProgressPage() {
  const project = useCurrentProject();
  const progress = useQuery({
    queryFn: () =>
      apiClient.request<ProgressAggregate>(
        `/projects/${encodeURIComponent(project.id)}/progress`,
        { cache: "no-store" },
      ),
    queryKey: ["progress", project.id],
    refetchInterval: (query) => {
      const latest = (query.state.data as ProgressAggregate | undefined)
        ?.latest_evaluation;
      return latest && ["queued", "running"].includes(latest.status)
        ? 1_000
        : 5_000;
    },
    refetchIntervalInBackground: true,
    refetchOnWindowFocus: "always",
  });

  if (progress.isLoading) return <LoadingState label="正在读取进度安排…" />;
  if (progress.isError || !progress.data) {
    return (
      <ErrorState
        description="Progress 暂时无法读取，请稍后重试。"
        onRetry={() => progress.refetch()}
        title="无法打开进度工作台"
      />
    );
  }

  return (
    <section aria-labelledby="progress-title" className="space-y-5">
      <header className="flex items-start gap-3">
        <span className="flex size-10 items-center justify-center rounded-xl border border-border bg-card shadow-xs">
          <CalendarClock aria-hidden="true" className="size-5" />
        </span>
        <div>
          <h1
            className="text-2xl font-semibold tracking-tight"
            id="progress-title"
          >
            Progress
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            把关键节点、时间安排和 AI 进度判断放在同一条可操作的时间线上。
          </p>
        </div>
      </header>
      <ProgressWorkbench
        canEvaluate={evaluateRoles.has(project.role ?? "")}
        canManage={manageRoles.has(project.role ?? "")}
        progress={progress.data}
        projectId={project.id}
      />
    </section>
  );
}
