"use client";

import { useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  FileQuestion,
  Gauge,
  ListChecks,
  Waypoints,
} from "lucide-react";
import Link from "next/link";

import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useCurrentProject } from "@/components/providers/project-provider";
import { apiClient } from "@/lib/api-client";
import type {
  ArtifactDetail,
  ArtifactPreview,
} from "@/features/artifact/types";
import { formatBytes } from "@/features/artifact/artifact-uploader";

type ProblemItem =
  | {
      detail: ArtifactDetail;
      previews: { items: ArtifactPreview[] };
    }
  | {
      artifact_id: string;
      status: "unavailable";
    };

type HomeAggregate = {
  project_id: string;
  generated_at: string;
  problem: {
    available: boolean;
    items: ProblemItem[];
    total: number;
  };
  milestones: HomeSection;
  todos: HomeSection;
  models: HomeSection;
  experiments: HomeSection;
  article: HomeSection;
  agent: HomeSection;
};

type HomeSection = {
  available: boolean;
  items: unknown[];
  total: number;
};

export default function ProjectHomePage() {
  const project = useCurrentProject();
  const home = useQuery({
    queryFn: () =>
      apiClient.request<{ fragments: { home: HomeAggregate } }>(
        `/projects/${encodeURIComponent(project.id)}/pages/home`,
      ),
    queryKey: ["project-home", project.id],
  });
  const aggregate = home.data?.fragments.home;

  return (
    <div className="grid gap-6">
      <header className="flex items-start gap-4">
        <span className="flex size-11 items-center justify-center rounded-xl border border-border bg-card shadow-sm">
          <Gauge aria-hidden="true" className="size-5" />
        </span>
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">项目首页</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            题目原始文件来自 Project 的
            source_artifact_ids[]，其余模块按阶段接入。
          </p>
        </div>
      </header>

      <section aria-labelledby="problem-files-title">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div>
            <h2 className="font-semibold" id="problem-files-title">
              题目原始文件
            </h2>
            <p className="mt-1 text-sm text-muted-foreground">
              已验证且与当前 Project 绑定的 Artifact。
            </p>
          </div>
          <Link
            className="inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-md border border-border bg-background px-4 py-2 text-sm font-medium shadow-xs transition-colors outline-none hover:bg-accent hover:text-accent-foreground focus-visible:ring-2 focus-visible:ring-ring"
            href={`/projects/${encodeURIComponent(project.id)}/artifacts?setup=1`}
          >
            管理题目文件
            <ArrowRight aria-hidden="true" className="size-4" />
          </Link>
        </div>
        {home.isLoading ? (
          <p className="text-sm text-muted-foreground">正在聚合项目首页…</p>
        ) : null}
        {home.error ? (
          <p className="text-sm text-destructive">{home.error.message}</p>
        ) : null}
        {aggregate && aggregate.problem.items.length === 0 ? (
          <Card className="border-dashed">
            <CardContent className="flex flex-col items-center py-10 text-center">
              <FileQuestion
                aria-hidden="true"
                className="size-8 text-muted-foreground"
              />
              <p className="mt-3 font-medium">尚未绑定题目原始文件</p>
              <p className="mt-1 text-sm text-muted-foreground">
                先在项目文件库上传，再选择可用 Artifact。
              </p>
            </CardContent>
          </Card>
        ) : null}
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {aggregate?.problem.items.map((item) => {
            if ("status" in item) {
              return (
                <Card key={item.artifact_id}>
                  <CardHeader>
                    <CardTitle className="text-base">文件当前不可用</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <p className="break-all text-xs text-muted-foreground">
                      {item.artifact_id}
                    </p>
                    <Badge className="mt-3">已移入回收站</Badge>
                  </CardContent>
                </Card>
              );
            }
            const detail = item.detail;
            return (
              <Link
                href={`/projects/${encodeURIComponent(project.id)}/artifacts?artifact=${encodeURIComponent(detail.artifact.artifact_id)}`}
                key={detail.artifact.artifact_id}
              >
                <Card className="h-full transition hover:border-primary/40 hover:shadow-md">
                  <CardHeader>
                    <CardTitle className="truncate text-base">
                      {detail.artifact.name}
                    </CardTitle>
                  </CardHeader>
                  <CardContent>
                    <p className="truncate text-sm text-muted-foreground">
                      {detail.current_version?.filename}
                    </p>
                    <div className="mt-3 flex items-center justify-between">
                      <Badge>{detail.artifact.kind}</Badge>
                      <span className="text-xs text-muted-foreground">
                        {detail.current_version
                          ? formatBytes(detail.current_version.size_bytes)
                          : "—"}
                      </span>
                    </div>
                  </CardContent>
                </Card>
              </Link>
            );
          })}
        </div>
      </section>

      <section
        aria-label="未来模块状态"
        className="grid gap-3 md:grid-cols-2 xl:grid-cols-3"
      >
        <FutureModule
          icon={ListChecks}
          label="里程碑与任务"
          section={aggregate?.milestones}
        />
        <FutureModule
          icon={Waypoints}
          label="模型、实验与论文"
          section={aggregate?.models}
        />
        <FutureModule
          icon={Gauge}
          label="Agent 状态"
          section={aggregate?.agent}
        />
      </section>
    </div>
  );
}

function FutureModule({
  icon: Icon,
  label,
  section,
}: Readonly<{
  icon: typeof Gauge;
  label: string;
  section?: HomeSection;
}>) {
  return (
    <Card>
      <CardContent className="flex items-center gap-3 p-4">
        <span className="flex size-9 items-center justify-center rounded-lg bg-muted">
          <Icon aria-hidden="true" className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium">{label}</p>
          <p className="text-xs text-muted-foreground">
            {section?.available ? `${section.total} 项` : "后续阶段接入"}
          </p>
        </div>
      </CardContent>
    </Card>
  );
}
