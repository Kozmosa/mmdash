"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, FlaskConical, RotateCcw, Search, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import { EmptyState } from "@/components/states/empty-state";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { UserMenu } from "@/components/user-menu";
import { ApiError, apiClient } from "@/lib/api-client";

type TrashedProject = {
  deleted_at: string;
  id: string;
  name: string;
  problem_summary: string;
  problem_title: string;
  purge_at: string;
  role: "owner";
  updated_at: string;
};

export default function ProjectTrashPage() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const trash = useQuery({
    queryFn: () =>
      apiClient.request<{ items: TrashedProject[] }>("/projects/trash"),
    queryKey: ["project-trash"],
    retry: false,
  });

  useEffect(() => {
    if (trash.error instanceof ApiError && trash.error.status === 401) {
      router.replace("/login");
    }
  }, [router, trash.error]);

  const restoreProject = useMutation({
    mutationFn: (projectId: string) =>
      apiClient.request<unknown>(
        `/projects/${encodeURIComponent(projectId)}/restore`,
        { method: "POST" },
      ),
    onSuccess() {
      void Promise.all([
        queryClient.invalidateQueries({ queryKey: ["projects"] }),
        queryClient.invalidateQueries({ queryKey: ["project-trash"] }),
      ]);
      toast.success("项目已恢复");
    },
    onError(error) {
      toast.error(error instanceof Error ? error.message : "恢复项目失败");
    },
  });

  const rawItems = trash.data?.items ?? [];
  const items = rawItems.filter((project) => {
    if (!search.trim()) return true;
    const term = search.toLowerCase();
    return (
      project.name.toLowerCase().includes(term) ||
      (project.problem_title || "").toLowerCase().includes(term) ||
      (project.problem_summary || "").toLowerCase().includes(term)
    );
  });

  return (
    <div className="min-h-screen bg-muted/20">
      <header className="border-b border-border bg-background">
        <div className="mx-auto flex h-16 w-full max-w-6xl items-center gap-4 px-6 lg:px-10">
          <Link
            aria-label="mmdash 项目首页"
            className="flex items-center gap-2.5 font-semibold tracking-tight"
            href="/projects"
          >
            <span className="flex size-9 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-sm">
              <FlaskConical aria-hidden="true" className="size-4" />
            </span>
            <span>mmdash</span>
          </Link>
          <div className="ml-auto">
            <UserMenu />
          </div>
        </div>
      </header>

      <main className="mx-auto w-full max-w-5xl p-6 lg:p-10">
        <Button
          className="mb-6"
          onClick={() => router.push("/projects")}
          variant="ghost"
        >
          <ArrowLeft aria-hidden="true" className="size-4" />
          返回项目列表
        </Button>

        <section className="mb-8" aria-labelledby="trash-title">
          <div className="mb-3 flex size-10 items-center justify-center rounded-lg border border-border bg-card shadow-xs">
            <Trash2 aria-hidden="true" className="size-5" />
          </div>
          <h1
            className="text-2xl font-semibold tracking-tight"
            id="trash-title"
          >
            项目回收站
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            只有 owner 能看到和恢复项目。项目保留 30 天，过期后将永久删除。
          </p>
        </section>

        {!trash.isLoading && !trash.error && rawItems.length > 0 && (
          <div className="mb-6 max-w-md relative">
            <Search className="pointer-events-none absolute left-3 top-2.5 size-4 text-muted-foreground" />
            <Input
              className="pl-9 h-9 bg-card border border-border"
              onChange={(e) => setSearch(e.target.value)}
              placeholder="搜索被删除的项目..."
              value={search}
            />
          </div>
        )}

        {trash.isLoading ? (
          <p className="text-sm text-muted-foreground">正在加载回收站…</p>
        ) : null}
        {trash.error &&
        !(trash.error instanceof ApiError && trash.error.status === 401) ? (
          <p className="text-sm text-destructive">{trash.error.message}</p>
        ) : null}
        {!trash.isLoading && !trash.error && rawItems.length === 0 ? (
          <EmptyState
            description="移入回收站的项目会在这里保留 30 天。"
            title="回收站为空"
          />
        ) : null}
        {!trash.isLoading && !trash.error && rawItems.length > 0 && items.length === 0 ? (
          <EmptyState
            description="尝试调整你的搜索词。"
            title="没有找到匹配的项目"
          />
        ) : null}

        <div className="grid gap-4 md:grid-cols-2">
          {items.map((project) => (
            <Card key={project.id}>
              <CardHeader>
                <CardTitle>{project.name}</CardTitle>
                <CardDescription>
                  {project.problem_title || "尚未填写题目"}
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <p className="line-clamp-2 text-sm text-muted-foreground">
                  {project.problem_summary || "尚未填写题目摘要。"}
                </p>
                <div className="rounded-md bg-muted px-3 py-2 text-xs text-muted-foreground">
                  剩余 {remainingDays(project.purge_at)} 天，将于{" "}
                  {formatDate(project.purge_at)} 永久删除
                </div>
                <Button
                  disabled={restoreProject.isPending}
                  onClick={() => restoreProject.mutate(project.id)}
                  variant="outline"
                >
                  <RotateCcw aria-hidden="true" className="size-4" />
                  恢复项目
                </Button>
              </CardContent>
            </Card>
          ))}
        </div>
      </main>
    </div>
  );
}

function remainingDays(purgeAt: string): number {
  return Math.max(
    1,
    Math.ceil((Date.parse(purgeAt) - Date.now()) / (24 * 60 * 60 * 1000)),
  );
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
