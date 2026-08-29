"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, FolderKanban, Plus, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { type FormEvent, useEffect, useState } from "react";
import { toast } from "sonner";

import { EmptyState } from "@/components/states/empty-state";
import { GlobalPageShell } from "@/components/layout/global-page-shell";
import { Badge } from "@/components/ui/badge";
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
import { ApiError, apiClient } from "@/lib/api-client";

type Project = {
  archived_at?: string;
  id: string;
  name: string;
  problem_summary: string;
  problem_title: string;
  role: "agent" | "box" | "editor" | "maintainer" | "owner" | "viewer";
  updated_at: string;
};

export default function ProjectsPage() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [creating, setCreating] = useState(false);
  const projects = useQuery({
    queryFn: () => apiClient.request<{ items: Project[] }>("/projects"),
    queryKey: ["projects"],
    retry: false,
  });

  useEffect(() => {
    if (projects.error instanceof ApiError && projects.error.status === 401) {
      router.replace("/login");
    }
  }, [projects.error, router]);

  const createProject = useMutation({
    mutationFn: (input: {
      name: string;
      problem_summary: string;
      problem_title: string;
    }) =>
      apiClient.request<Project>("/projects", {
        body: input,
        method: "POST",
      }),
    onSuccess(project) {
      void queryClient.invalidateQueries({ queryKey: ["projects"] });
      toast.success("项目已创建");
      router.push(
        `/projects/${encodeURIComponent(project.id)}/artifacts?setup=1`,
      );
    },
  });

  const trashProject = useMutation({
    mutationFn: (projectId: string) =>
      apiClient.request(`/projects/${encodeURIComponent(projectId)}`, {
        method: "DELETE",
      }),
    onSuccess() {
      void Promise.all([
        queryClient.invalidateQueries({ queryKey: ["projects"] }),
        queryClient.invalidateQueries({ queryKey: ["project-trash"] }),
      ]);
      toast.success("项目已移入回收站，可在 30 天内恢复");
    },
    onError(error) {
      toast.error(error instanceof Error ? error.message : "移入回收站失败");
    },
  });

  function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    createProject.mutate({
      name: String(form.get("name") ?? ""),
      problem_summary: String(form.get("problem_summary") ?? ""),
      problem_title: String(form.get("problem_title") ?? ""),
    });
  }

  const items = projects.data?.items ?? [];
  return (
    <GlobalPageShell
      headerActions={
        <Button onClick={() => setCreating((value) => !value)}>
          <Plus aria-hidden="true" className="size-4" />
          创建项目
        </Button>
      }
    >
      <section className="mb-8" aria-labelledby="projects-title">
        <div className="mb-3 flex size-10 items-center justify-center rounded-lg border border-border bg-card shadow-xs">
          <FolderKanban aria-hidden="true" className="size-5" />
        </div>
        <h1
          className="text-2xl font-semibold tracking-tight"
          id="projects-title"
        >
          团队项目
        </h1>
        <p className="mt-1 text-sm text-muted-foreground">
          创建项目、邀请成员并进入协作工作区
        </p>
      </section>

      {creating ? (
        <Card className="mb-6">
          <form onSubmit={handleCreate}>
            <CardHeader>
              <CardTitle>创建团队项目</CardTitle>
              <CardDescription>
                创建者自动成为 owner，之后可在项目设置中管理成员角色。
              </CardDescription>
            </CardHeader>
            <CardContent className="grid gap-4 md:grid-cols-2">
              <label className="grid gap-2 text-sm font-medium">
                项目名称
                <Input name="name" required />
              </label>
              <label className="grid gap-2 text-sm font-medium">
                题目标题
                <Input name="problem_title" />
              </label>
              <label className="grid gap-2 text-sm font-medium md:col-span-2">
                题目摘要
                <Input name="problem_summary" />
              </label>
            </CardContent>
            <CardFooter className="gap-2">
              <Button disabled={createProject.isPending} type="submit">
                {createProject.isPending ? "创建中…" : "确认创建"}
              </Button>
              <Button
                onClick={() => setCreating(false)}
                type="button"
                variant="ghost"
              >
                取消
              </Button>
            </CardFooter>
          </form>
        </Card>
      ) : null}

      {projects.isLoading ? (
        <p className="text-sm text-muted-foreground">正在加载项目…</p>
      ) : null}
      {projects.error &&
      !(projects.error instanceof ApiError && projects.error.status === 401) ? (
        <p className="text-sm text-destructive">{projects.error.message}</p>
      ) : null}
      {!projects.isLoading && items.length === 0 ? (
        <EmptyState
          description="创建第一个团队项目后，即可进入带侧边栏的协作工作区。"
          title="还没有可用项目"
        />
      ) : null}
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {items.map((project) => (
          <div
            key={project.id}
            className="group relative flex flex-col justify-between h-full rounded-xl border border-border bg-card shadow-card hover:shadow-md hover:border-primary/30 transition-all duration-200"
          >
            <Link
              href={`/projects/${encodeURIComponent(project.id)}`}
              className="flex-1 p-5 flex flex-col justify-between"
            >
              <div>
                <div className="flex items-start justify-between gap-3">
                  <div className="space-y-1 min-w-0">
                    <h3 className="font-semibold text-base text-foreground truncate group-hover:text-primary transition-colors">
                      {project.name}
                    </h3>
                    <p className="text-xs text-muted-foreground font-medium truncate">
                      {project.problem_title || "尚未填写题目名称"}
                    </p>
                  </div>
                  <Badge className="shrink-0 text-[10px] bg-primary/10 text-primary border-primary/20 hover:bg-primary/20 capitalize font-semibold shadow-none">
                    {project.role}
                  </Badge>
                </div>
                <p className="mt-4 line-clamp-3 text-xs text-muted-foreground leading-relaxed">
                  {project.problem_summary || "尚未填写题目摘要。"}
                </p>
              </div>
            </Link>
            <div className="px-5 pb-5 pt-3 border-t border-border/50 flex items-center justify-between">
              <Link
                href={`/projects/${encodeURIComponent(project.id)}`}
                className="inline-flex h-8 items-center justify-center rounded-md bg-primary px-3 text-xs font-semibold text-primary-foreground shadow-sm hover:bg-primary/90 transition-colors"
              >
                进入工作区
                <ArrowRight className="ml-1.5 size-3.5 group-hover:translate-x-0.5 transition-transform" />
              </Link>
              {project.role === "owner" ? (
                <Button
                  aria-label={`将 ${project.name} 移入回收站`}
                  disabled={trashProject.isPending}
                  onClick={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    if (
                      window.confirm(
                        `确定将“${project.name}”移入回收站吗？30 天内可以恢复。`,
                      )
                    ) {
                      trashProject.mutate(project.id);
                    }
                  }}
                  size="icon"
                  variant="ghost"
                  className="h-7 w-7 text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors rounded-md"
                >
                  <Trash2 aria-hidden="true" className="size-3.5" />
                </Button>
              ) : null}
            </div>
          </div>
        ))}
      </div>
    </GlobalPageShell>
  );
}
