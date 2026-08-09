"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FolderKanban, Plus, Trash2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { type FormEvent, useEffect, useState } from "react";
import { toast } from "sonner";

import { EmptyState } from "@/components/states/empty-state";
import { GlobalPageShell } from "@/components/layout/global-page-shell";
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
          <Card key={project.id}>
            <CardHeader>
              <CardTitle>{project.name}</CardTitle>
              <CardDescription>
                {project.problem_title || "尚未填写题目"} · {project.role}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <p className="line-clamp-3 text-sm text-muted-foreground">
                {project.problem_summary || "尚未填写题目摘要。"}
              </p>
            </CardContent>
            <CardFooter className="justify-between">
              <Button
                onClick={() =>
                  router.push(`/projects/${encodeURIComponent(project.id)}`)
                }
              >
                进入工作区
              </Button>
              {project.role === "owner" ? (
                <Button
                  aria-label={`将 ${project.name} 移入回收站`}
                  disabled={trashProject.isPending}
                  onClick={() => {
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
                >
                  <Trash2 aria-hidden="true" className="size-4" />
                </Button>
              ) : null}
            </CardFooter>
          </Card>
        ))}
      </div>
    </GlobalPageShell>
  );
}
