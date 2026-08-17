"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Box as BoxIcon,
  Download,
  LoaderCircle,
  Pencil,
  ShieldAlert,
  Unplug,
} from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";

import { experimentApi } from "./api";
import { developmentDownloads } from "./development-downloads";
import type { Box } from "./types";

export function BoxManagement() {
  const client = useQueryClient();
  const boxes = useQuery({
    queryFn: () => experimentApi.personalBoxes(),
    queryKey: ["personal-boxes"],
    refetchInterval: 5_000,
  });
  const rename = useMutation({
    mutationFn: ({ boxId, name }: { boxId: string; name: string }) =>
      experimentApi.renameBox(boxId, name),
    onError: (error) => toast.error(error.message),
    onSuccess: async () => {
      toast.success("Box 名称已更新");
      await client.invalidateQueries({ queryKey: ["personal-boxes"] });
    },
  });
  const revoke = useMutation({
    mutationFn: ({ boxId, mode }: { boxId: string; mode: "drain" | "force" }) =>
      experimentApi.revokeBox(boxId, mode),
    onError: (error) => toast.error(error.message),
    onSuccess: async (result) => {
      toast.success(
        result.mode === "drain" ? "Box 已进入等待撤销" : "Box 已强制撤销",
      );
      await client.invalidateQueries({ queryKey: ["personal-boxes"] });
    },
  });

  return (
    <section aria-labelledby="box-management-title" className="space-y-5">
      <header>
        <h1 className="text-2xl font-semibold" id="box-management-title">
          Box 管理
        </h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Box 属于你的账号，可分配给多个 Project。下载后使用 `mbox setup`、
          `mbox account login` 完成初始化和账号绑定。
        </p>
      </header>
      <BoxInstallerCard />
      {boxes.isLoading ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <LoaderCircle className="size-4 animate-spin" />
          正在读取 Box…
        </p>
      ) : null}
      <div className="grid gap-4 lg:grid-cols-2">
        {boxes.data?.items.map((box) => (
          <BoxCard
            box={box}
            key={box.box_id}
            onRename={(name) => rename.mutate({ boxId: box.box_id, name })}
            onRevoke={(mode) => revoke.mutate({ boxId: box.box_id, mode })}
            pending={rename.isPending || revoke.isPending}
          />
        ))}
      </div>
      {!boxes.isLoading && !boxes.data?.items.length ? (
        <Card>
          <CardContent className="py-10 text-center">
            <BoxIcon className="mx-auto mb-3 size-8 text-muted-foreground" />
            <p className="font-medium">尚未绑定 Box</p>
            <p className="mt-1 text-sm text-muted-foreground">
              在要作为 Box 的机器上运行 `mbox setup`，然后执行 `mbox account
              login` 完成浏览器授权。
            </p>
          </CardContent>
        </Card>
      ) : null}
    </section>
  );
}

export function BoxInstallerCard() {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Download className="size-4" />
          安装 Box Gateway
        </CardTitle>
        <CardDescription>
          下载 dev.mjs 最近一次构建的静态开发版可执行文件。正式发行版发布后会
          使用独立的版本目录。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div
          className="flex flex-wrap gap-2"
          role="group"
          aria-label="下载 Box Gateway"
        >
          <a
            className="inline-flex h-9 items-center justify-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground shadow-sm hover:bg-primary/90"
            download={developmentDownloads.box.windows.filename}
            href={developmentDownloads.box.windows.href}
          >
            <Download className="size-4" />
            Windows
          </a>
          <a
            className="inline-flex h-9 items-center justify-center gap-2 rounded-md border border-border bg-background px-4 py-2 text-sm font-medium shadow-xs hover:bg-accent hover:text-accent-foreground"
            download={developmentDownloads.box.linux.filename}
            href={developmentDownloads.box.linux.href}
          >
            <Download className="size-4" />
            Linux
          </a>
        </div>
        <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
          <p>
            Windows 和 Linux 按钮分别对应 `apps/web/public/downloads/dev/`
            下的静态文件。每次启动 `dev.mjs` 都会重新构建并覆盖对应文件。
          </p>
          <p className="mt-2">
            下载后运行 `mbox setup`，再执行 `mbox account login`
            完成初始化和账号绑定。
          </p>
        </div>
      </CardContent>
    </Card>
  );
}

function BoxCard({
  box,
  onRename,
  onRevoke,
  pending,
}: Readonly<{
  box: Box;
  onRename: (name: string) => void;
  onRevoke: (mode: "drain" | "force") => void;
  pending: boolean;
}>) {
  const [name, setName] = useState(box.name);
  const forceRevoke = () => {
    if (
      window.confirm(
        "强制撤销会立即使 Box Token 失效，并将相关运行中实验标记为失败。确定继续吗？",
      )
    )
      onRevoke("force");
  };
  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <div>
            <CardTitle className="flex items-center gap-2">
              <BoxIcon className="size-4" />
              {box.name}
            </CardTitle>
            <CardDescription className="mt-1 font-mono">
              {box.box_id}
            </CardDescription>
          </div>
          <Badge>{box.status}</Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {box.legacy_reauthorization_required ? (
          <p className="rounded-md bg-amber-500/10 p-3 text-xs text-amber-700">
            旧版 Project-scoped Box 必须重新完成账号级设备授权。
          </p>
        ) : null}
        <div className="grid grid-cols-2 gap-3 text-xs">
          <Metric label="版本" value={box.version} />
          <Metric
            label="任务负载"
            value={`${box.load.running_tasks}/${box.load.capacity}`}
          />
          <Metric
            label="Runtime"
            value={
              box.runtimes.map((runtime) => runtime.name).join(", ") ||
              "未探测到"
            }
          />
          <Metric
            label="Project 分配"
            value={String(box.project_assignments.length)}
          />
          <Metric
            label="最近心跳"
            value={
              box.last_heartbeat_at
                ? new Date(box.last_heartbeat_at).toLocaleString()
                : "尚无"
            }
          />
          <Metric
            label="离线时间"
            value={
              box.offline_since
                ? new Date(box.offline_since).toLocaleString()
                : "—"
            }
          />
        </div>
        <div className="flex gap-2">
          <Input
            aria-label="Box 名称"
            maxLength={200}
            onChange={(event) => setName(event.target.value)}
            value={name}
          />
          <Button
            disabled={pending || !name.trim() || name.trim() === box.name}
            onClick={() => onRename(name.trim())}
            size="sm"
          >
            <Pencil className="size-3.5" />
            保存
          </Button>
        </div>
        <div className="flex flex-wrap gap-2 border-t pt-4">
          {box.status !== "revoked" ? (
            <Button
              disabled={pending}
              onClick={() => onRevoke("drain")}
              size="sm"
              variant="outline"
            >
              <Unplug className="size-3.5" />
              等待任务完成后撤销
            </Button>
          ) : null}
          {box.status !== "revoked" ? (
            <Button
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              disabled={pending}
              onClick={forceRevoke}
              size="sm"
            >
              <ShieldAlert className="size-3.5" />
              强制撤销
            </Button>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}

function Metric({ label, value }: Readonly<{ label: string; value: string }>) {
  return (
    <div>
      <p className="text-muted-foreground">{label}</p>
      <p className="mt-0.5 break-all font-medium">{value}</p>
    </div>
  );
}
