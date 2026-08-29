"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link2, Server, ShieldAlert, Unlink } from "lucide-react";
import { toast } from "sonner";

import { useCurrentProject } from "@/components/providers/project-provider";
import { useCurrentUser } from "@/components/providers/user-provider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

import { experimentApi } from "./api";
import type { Box } from "./types";

const assignRoles = new Set(["owner", "maintainer"]);

export function ProjectBoxSettingsPanel() {
  const project = useCurrentProject();
  const user = useCurrentUser();
  const client = useQueryClient();
  const assigned = useQuery({
    queryFn: () => experimentApi.projectBoxes(project.id),
    queryKey: ["project-boxes", project.id],
  });
  const personal = useQuery({
    queryFn: () => experimentApi.personalBoxes(),
    queryKey: ["personal-boxes"],
  });
  const refresh = async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: ["project-boxes", project.id] }),
      client.invalidateQueries({ queryKey: ["personal-boxes"] }),
    ]);
  };
  const assign = useMutation({
    mutationFn: (boxId: string) => experimentApi.assignBox(project.id, boxId),
    onError: (error) => toast.error(error.message),
    onSuccess: async () => {
      toast.success("Box 已分配到 Project");
      await refresh();
    },
  });
  const remove = useMutation({
    mutationFn: ({ boxId, force }: { boxId: string; force: boolean }) =>
      experimentApi.removeBox(project.id, boxId, force),
    onError: (error) => toast.error(error.message),
    onSuccess: async (_, input) => {
      toast.success(
        input.force ? "Box 已强制解除" : "Box 将在相关实验结束后解除",
      );
      await refresh();
    },
  });
  const assignedIds = new Set(assigned.data?.items.map((box) => box.box_id));
  const eligible =
    personal.data?.items.filter(
      (box) => !assignedIds.has(box.box_id) && box.status !== "revoked",
    ) ?? [];
  const canAssign = assignRoles.has(project.role ?? "");
  return (
    <section aria-labelledby="project-box-settings-title" className="space-y-4">
      <div>
        <h2
          className="flex items-center gap-2 text-lg font-semibold"
          id="project-box-settings-title"
        >
          <Server className="size-5" />
          Project Box
        </h2>
        <p className="mt-1 text-sm text-muted-foreground">
          一个 Project 可以使用多个账号级 Box；一个 Box 也可以服务多个
          Project，任务统一进入队列。
        </p>
      </div>
      <Card>
        <CardHeader>
          <CardTitle className="text-base">已分配 Box</CardTitle>
          <CardDescription>
            owner、maintainer、editor 都可使用；只有 owner/maintainer 或 Box
            所有者可以解除。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {assigned.data?.items.map((box) => (
            <AssignedBox
              box={box}
              canRemove={canAssign || box.owner_user_id === user?.id}
              key={box.box_id}
              onForce={() => {
                if (
                  window.confirm(
                    "强制解除会立即令相关运行中实验失败。确定继续吗？",
                  )
                )
                  remove.mutate({ boxId: box.box_id, force: true });
              }}
              onRemove={() =>
                remove.mutate({ boxId: box.box_id, force: false })
              }
            />
          ))}
          {!assigned.isLoading && !assigned.data?.items.length ? (
            <p className="text-sm text-muted-foreground">
              尚未向 Project 分配 Box。
            </p>
          ) : null}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle className="text-base">可分配的个人 Box</CardTitle>
          <CardDescription>
            {canAssign
              ? "从你账号下尚未分配到当前 Project 的 Box 中选择。"
              : "当前角色可使用已分配 Box，但不能修改分配关系。"}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {eligible.map((box) => (
            <div
              className="flex flex-wrap items-center gap-3 rounded-lg border p-3"
              key={box.box_id}
            >
              <div className="min-w-0 flex-1">
                <p className="font-medium">{box.name}</p>
                <p className="text-xs text-muted-foreground">
                  {box.runtimes.map((runtime) => runtime.name).join(", ") ||
                    "无可用 Runtime"}{" "}
                  · {box.load.running_tasks}/{box.load.capacity} tasks
                </p>
              </div>
              <Badge>{box.status}</Badge>
              {canAssign ? (
                <Button
                  disabled={assign.isPending}
                  onClick={() => assign.mutate(box.box_id)}
                  size="sm"
                >
                  <Link2 className="size-3.5" />
                  分配
                </Button>
              ) : null}
            </div>
          ))}
          {!personal.isLoading && !eligible.length ? (
            <p className="text-sm text-muted-foreground">
              没有可分配的个人 Box。
            </p>
          ) : null}
        </CardContent>
      </Card>
    </section>
  );
}

function AssignedBox({
  box,
  canRemove,
  onForce,
  onRemove,
}: Readonly<{
  box: Box;
  canRemove: boolean;
  onForce: () => void;
  onRemove: () => void;
}>) {
  return (
    <div className="flex flex-wrap items-center gap-3 rounded-lg border p-3">
      <div className="min-w-0 flex-1">
        <p className="font-medium">{box.name}</p>
        <p className="text-xs text-muted-foreground">
          {box.version} ·{" "}
          {box.runtimes.map((runtime) => runtime.name).join(", ")} ·{" "}
          {box.load.running_tasks}/{box.load.capacity} tasks
        </p>
      </div>
      <Badge>{box.status}</Badge>
      {canRemove ? (
        <>
          <Button onClick={onRemove} size="sm" variant="outline">
            <Unlink className="size-3.5" />
            等待实验完成后解除
          </Button>
          <Button
            aria-label={`强制解除 ${box.name}`}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            onClick={onForce}
            size="icon"
          >
            <ShieldAlert className="size-4" />
          </Button>
        </>
      ) : null}
    </div>
  );
}
