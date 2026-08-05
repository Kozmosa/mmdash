"use client";

import { useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  Bot,
  FileQuestion,
  Gauge,
  ListChecks,
} from "lucide-react";
import Link from "next/link";

import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/states/empty-state";
import { useCurrentProject } from "@/components/providers/project-provider";
import { apiClient } from "@/lib/api-client";
import type {
  ArtifactDetail,
  ArtifactPreview,
} from "@/features/artifact/types";
import { formatBytes } from "@/features/artifact/artifact-uploader";
import type { AgentInstance } from "@/features/agent/types";

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
      apiClient.request<{ fragments: { home: HomeAggregate; project: Project } }>(
        `/projects/${encodeURIComponent(project.id)}/pages/home`,
      ),
    queryKey: ["project-home", project.id],
  });
  const aggregate = home.data?.fragments.home;
  const projectDetail = home.data?.fragments.project;

  return (
    <div className="grid gap-6">
      <header className="flex items-start gap-4">
        <span className="flex size-11 items-center justify-center rounded-xl border border-border bg-card shadow-sm">
          <Gauge aria-hidden="true" className="size-5" />
        </span>
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">项目首页</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {projectDetail?.problem_title || "项目问题与协作进度"}
          </p>
        </div>
      </header>

      {projectDetail?.project_constraints?.length ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">关键约束</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
              {projectDetail.project_constraints.map((constraint) => (
                <li key={constraint}>{constraint}</li>
              ))}
            </ul>
          </CardContent>
        </Card>
      ) : null}

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
        <ProgressHomeCard aggregate={aggregate} projectId={project.id} />
        <EmptyModule label="模型、实验与论文" />
        <AgentHomeCard projectId={project.id} />
      </section>
    </div>
  );
}

function AgentHomeCard({ projectId }: Readonly<{ projectId: string }>) {
  const instances = useQuery({
    queryFn: () =>
      apiClient.request<{ items: AgentInstance[] }>(
        `/projects/${encodeURIComponent(projectId)}/agent-instances`,
      ),
    queryKey: ["agent-instances", projectId],
  });
  const instance =
    instances.data?.items?.find((item) => item.status === "active") ??
    instances.data?.items?.find((item) => item.status !== "disabled") ??
    instances.data?.items?.[0];

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <Bot aria-hidden="true" className="size-4" />
          Agent
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        {instances.isLoading ? (
          <p className="text-muted-foreground">正在读取 Hermes 状态…</p>
        ) : null}
        {instances.isError ? (
          <p className="text-destructive">Agent 状态暂时不可用。</p>
        ) : null}
        {!instances.isLoading && !instance ? (
          <>
            <p className="text-muted-foreground">尚未配置 Hermes Agent。</p>
            <Link
              className="inline-flex items-center gap-1 text-primary hover:underline"
              href={`/projects/${encodeURIComponent(projectId)}/settings#agent-settings`}
            >
              配置连接 <ArrowRight aria-hidden="true" className="size-3" />
            </Link>
          </>
        ) : null}
        {instance ? (
          <>
            <div className="flex items-center justify-between gap-2">
              <span className="truncate font-medium">{instance.display_name}</span>
              <Badge>{instance.status}</Badge>
            </div>
            <div className="flex items-center justify-between gap-2">
              <span className="text-muted-foreground">管理模式</span>
              <span>{instance.management_mode}</span>
            </div>
            <div className="flex items-center justify-between gap-2">
              <span className="text-muted-foreground">MCP 访问</span>
              <span>{instance.grant.project_access_status ?? "pending"}</span>
            </div>
            <Link
              className="inline-flex items-center gap-1 text-primary hover:underline"
              href={`/projects/${encodeURIComponent(projectId)}/agent`}
            >
              打开 Agent <ArrowRight aria-hidden="true" className="size-3" />
            </Link>
          </>
        ) : null}
      </CardContent>
    </Card>
  );
}

type Project = {
  problem_title: string;
  project_constraints: string[];
};

function ProgressHomeCard({
  aggregate,
  projectId,
}: Readonly<{ aggregate?: HomeAggregate; projectId: string }>) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          <ListChecks aria-hidden="true" className="size-4" />
          Progress
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        <div className="flex items-center justify-between">
          <span className="text-muted-foreground">关键节点</span>
          <Badge>{aggregate?.milestones.total ?? 0}</Badge>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-muted-foreground">任务</span>
          <Badge>{aggregate?.todos.total ?? 0}</Badge>
        </div>
        <Link
          className="inline-flex items-center gap-1 text-primary hover:underline"
          href={`/projects/${encodeURIComponent(projectId)}/progress`}
        >
          打开 Progress <ArrowRight aria-hidden="true" className="size-3" />
        </Link>
      </CardContent>
    </Card>
  );
}

function EmptyModule({
  label,
}: Readonly<{ label: string }>) {
  return <EmptyState description="该首页区域属于后续产品阶段，当前没有临时或模拟数据。" title={`${label}尚未接入`} />;
}
