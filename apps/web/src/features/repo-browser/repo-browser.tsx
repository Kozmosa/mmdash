"use client";

import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { FileCode2, GitCommitHorizontal, RefreshCw } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo } from "react";

import { useCurrentProject } from "@/components/providers/project-provider";
import { EmptyState } from "@/components/states/empty-state";
import { ErrorState } from "@/components/states/error-state";
import { LoadingState } from "@/components/states/loading-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { optionalRequest } from "@/features/repo/optional-request";
import type {
  RepoCommit,
  RepoCommitPage,
  RepoFileContent,
  RepoWorkspaceKind,
  Repository,
} from "@/features/repo/types";
import { apiClient } from "@/lib/api-client";
import { cn } from "@/lib/cn";

import { ContentPreview } from "./content-preview";
import {
  parseRepoLocation,
  repoLocationQuery,
  type RepoLocation,
} from "./location";
import { RepoTree } from "./repo-tree";

const workspaces: RepoWorkspaceKind[] = ["code", "article", "result"];

export function RepoBrowser() {
  const project = useCurrentProject();
  const pathname = usePathname();
  const router = useRouter();
  const search = useSearchParams();
  const queryClient = useQueryClient();
  const location = useMemo(
    () => parseRepoLocation(new URLSearchParams(search.toString())),
    [search],
  );
  const repoPath = `/projects/${encodeURIComponent(project.id)}/repository`;
  const repository = useQuery({
    queryFn: () => optionalRequest<Repository>(apiClient, repoPath),
    queryKey: ["repository", project.id],
    retry: false,
  });
  const workspace = repository.data?.workspaces.find(
    (item) => item.workspace === location.workspace,
  );
  const revision = location.revision ?? workspace?.head_commit_sha ?? null;

  const replaceLocation = (next: RepoLocation) => {
    router.replace(`${pathname}?${repoLocationQuery(next)}`, {
      scroll: false,
    });
  };

  useEffect(() => {
    if (!location.revision && revision) {
      replaceLocation({ ...location, path: "", revision });
    }
  }, [location, revision]);

  const commits = useInfiniteQuery({
    enabled: Boolean(repository.data && revision),
    getNextPageParam: (lastPage: RepoCommitPage) =>
      lastPage.has_more ? lastPage.next_cursor : undefined,
    initialPageParam: "",
    queryFn: ({ pageParam }) =>
      apiClient.request<RepoCommitPage>(`${repoPath}/commits`, {
        query: {
          cursor: pageParam || undefined,
          limit: 40,
          workspace: location.workspace,
        },
      }),
    queryKey: ["repo-commits", project.id, location.workspace],
  });
  const commit = useQuery({
    enabled: Boolean(revision),
    queryFn: () =>
      apiClient.request<RepoCommit>(
        `${repoPath}/commits/${encodeURIComponent(revision!)}`,
      ),
    queryKey: ["repo-commit", project.id, revision],
  });
  const content = useQuery({
    enabled: Boolean(revision && location.path),
    queryFn: () =>
      apiClient.request<RepoFileContent>(`${repoPath}/content`, {
        query: {
          path: location.path,
          revision: revision!,
          workspace: location.workspace,
        },
      }),
    queryKey: [
      "repo-content",
      project.id,
      location.workspace,
      revision,
      location.path,
    ],
  });

  if (repository.isPending) {
    return <LoadingState label="正在读取 Repository 状态…" />;
  }
  if (repository.isError) {
    return (
      <ErrorState
        description="请检查 BFF 与 Core 连接后重试。"
        onRetry={() => void repository.refetch()}
        title="无法读取 Repository"
      />
    );
  }
  if (!repository.data) {
    return (
      <EmptyState
        description="请先在设置页创建 mmdash 托管仓库，或绑定 GitHub / 已启用的服务器仓库。"
        title="尚未绑定 Repository"
      />
    );
  }
  if (!workspace || !revision) {
    return (
      <EmptyState
        description="该逻辑工作区尚未完成首次同步，暂时没有可固定读取的 commit。"
        title="工作区尚未就绪"
      />
    );
  }

  const managedRepository = repository.data;
  const allCommits = commits.data?.pages.flatMap((page) => page.items) ?? [];

  return (
    <section className="space-y-5" aria-labelledby="repo-browser-title">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="mb-3 flex size-10 items-center justify-center rounded-lg border bg-card shadow-xs">
            <FileCode2 aria-hidden="true" className="size-5" />
          </div>
          <h1
            className="text-2xl font-semibold tracking-tight"
            id="repo-browser-title"
          >
            Repository 浏览
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            只读浏览受管 Git 对象；目录和内容始终固定到完整 commit SHA。
          </p>
        </div>
        <Button
          onClick={async () => {
            const refreshed = await repository.refetch();
            const nextWorkspace = refreshed.data?.workspaces.find(
              (item) => item.workspace === location.workspace,
            );
            const nextRevision = nextWorkspace?.head_commit_sha;
            queryClient.removeQueries({
              queryKey: ["repo-tree", project.id],
            });
            queryClient.removeQueries({
              queryKey: ["repo-content", project.id],
            });
            await queryClient.invalidateQueries({
              queryKey: ["repo-commits", project.id, location.workspace],
            });
            if (nextRevision) {
              replaceLocation({
                path: "",
                revision: nextRevision,
                workspace: location.workspace,
              });
            }
          }}
          variant="outline"
        >
          <RefreshCw aria-hidden="true" className="size-4" />
          刷新到分支 HEAD
        </Button>
      </header>

      <div
        aria-label="逻辑工作区"
        className="flex flex-wrap gap-2"
        role="tablist"
      >
        {workspaces.map((kind) => {
          const mapping = managedRepository.workspaces.find(
            (item) => item.workspace === kind,
          );
          const active = kind === location.workspace;
          return (
            <button
              aria-selected={active}
              className={cn(
                "min-w-40 rounded-lg border px-4 py-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring",
                active ? "border-foreground bg-card shadow-sm" : "bg-muted/30",
              )}
              key={kind}
              onClick={() =>
                replaceLocation({
                  path: "",
                  revision: mapping?.head_commit_sha ?? null,
                  workspace: kind,
                })
              }
              role="tab"
              type="button"
            >
              <span className="block text-sm font-semibold">{kind}</span>
              <span className="mt-1 block text-xs text-muted-foreground">
                {mapping?.remote_branch ?? "未映射"} ·{" "}
                {shortSha(mapping?.head_commit_sha)}
              </span>
            </button>
          );
        })}
      </div>

      <div className="flex flex-wrap items-center gap-2 text-sm">
        <Badge>{workspace.remote_branch}</Badge>
        <code title={revision}>{revision}</code>
        <span className="text-muted-foreground">
          {workspace.status} · {managedRepository.status}
        </span>
      </div>

      <div className="grid gap-4 xl:grid-cols-[280px_minmax(0,1fr)]">
        <Card className="h-fit">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <GitCommitHorizontal aria-hidden="true" className="size-4" />
              Commits
            </CardTitle>
            <CardDescription>选择后重新固定 tree 与 content。</CardDescription>
          </CardHeader>
          <CardContent>
            {commits.isPending ? (
              <p className="text-sm text-muted-foreground">正在读取 commits…</p>
            ) : commits.isError ? (
              <p className="text-sm text-red-700" role="alert">
                Commit 列表读取失败
              </p>
            ) : (
              <ol className="space-y-1">
                {allCommits.map((item) => (
                  <li key={item.commit_sha}>
                    <button
                      aria-current={
                        item.commit_sha === revision ? "true" : undefined
                      }
                      className={cn(
                        "w-full rounded-md px-2 py-2 text-left text-xs outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring",
                        item.commit_sha === revision ? "bg-accent" : null,
                      )}
                      onClick={() =>
                        replaceLocation({
                          ...location,
                          path: "",
                          revision: item.commit_sha,
                        })
                      }
                      type="button"
                    >
                      <span className="block font-mono font-semibold">
                        {shortSha(item.commit_sha)}
                      </span>
                      <span className="mt-0.5 block truncate text-muted-foreground">
                        {firstLine(item.message) || "(no message)"}
                      </span>
                    </button>
                  </li>
                ))}
              </ol>
            )}
            {commits.hasNextPage ? (
              <Button
                className="mt-3 w-full"
                disabled={commits.isFetchingNextPage}
                onClick={() => void commits.fetchNextPage()}
                size="sm"
                variant="ghost"
              >
                {commits.isFetchingNextPage ? "正在载入…" : "载入更早 commits"}
              </Button>
            ) : null}
          </CardContent>
        </Card>

        <div className="min-w-0 space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">
                {firstLine(commit.data?.message ?? "") || shortSha(revision)}
              </CardTitle>
              <CardDescription>
                {commit.data
                  ? `${commit.data.author.name} · ${new Date(commit.data.author.time).toLocaleString()}`
                  : "正在读取 commit 元数据…"}
              </CardDescription>
            </CardHeader>
            {commit.data?.changes.length ? (
              <CardContent>
                <p className="text-xs text-muted-foreground">
                  {commit.data.changes.length} 个变更路径
                </p>
              </CardContent>
            ) : null}
          </Card>

          <div className="grid min-w-0 gap-4 lg:grid-cols-[300px_minmax(0,1fr)]">
            <RepoTree
              key={`${location.workspace}:${revision}`}
              onSelect={(path) =>
                replaceLocation({ ...location, path, revision })
              }
              projectId={project.id}
              revision={revision}
              selectedPath={location.path}
              workspace={location.workspace}
            />
            <div className="min-w-0">
              {!location.path ? (
                <div className="flex min-h-72 items-center justify-center rounded-xl border bg-muted/20 p-8 text-center text-sm text-muted-foreground">
                  从文件树选择一个对象。文本进入只读
                  Monaco，其余类型只显示安全元数据。
                </div>
              ) : content.isPending ? (
                <LoadingState label="正在读取固定 revision 的文件…" />
              ) : content.isError ? (
                <ErrorState
                  description="对象可能已被拒绝预览，或当前权限不足。"
                  onRetry={() => void content.refetch()}
                  title="无法读取文件"
                />
              ) : content.data ? (
                <ContentPreview content={content.data} projectId={project.id} />
              ) : null}
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}

function shortSha(value: string | null | undefined): string {
  return value ? value.slice(0, 8) : "未解析";
}

function firstLine(value: string): string {
  return value.trim().split("\n", 1)[0] ?? "";
}
