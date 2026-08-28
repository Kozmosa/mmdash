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
import { cn } from "@/lib/cn";

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
    <Card className="border-primary/20 bg-gradient-to-br from-background to-muted/20">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-lg">
          <Download className="size-5 text-primary animate-pulse" />
          快速安装 Box Gateway
        </CardTitle>
        <CardDescription>
          Box Gateway（mbox）是部署在您的沙箱或物理服务器上的轻量化网关，通过安全的底层通道将本地执行能力接入 mmdash。
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <div className="grid gap-6 md:grid-cols-3">
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <span className="flex size-5 items-center justify-center rounded-full bg-primary/10 text-xs font-semibold text-primary">1</span>
              <span className="text-sm font-medium">下载 mbox 执行文件</span>
            </div>
            <div className="flex flex-wrap gap-2 pt-1">
              <a
                className="inline-flex h-8 items-center justify-center gap-1.5 rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground shadow-sm hover:bg-primary/90 transition-colors"
                download={developmentDownloads.box.windows.filename}
                href={developmentDownloads.box.windows.href}
              >
                <Download className="size-3.5" />
                Windows (.exe)
              </a>
              <a
                className="inline-flex h-8 items-center justify-center gap-1.5 rounded-md border border-border bg-background px-3 text-xs font-medium shadow-xs hover:bg-accent hover:text-accent-foreground transition-colors"
                download={developmentDownloads.box.linux.filename}
                href={developmentDownloads.box.linux.href}
              >
                <Download className="size-3.5" />
                Linux (x64)
              </a>
            </div>
          </div>

          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <span className="flex size-5 items-center justify-center rounded-full bg-primary/10 text-xs font-semibold text-primary">2</span>
              <span className="text-sm font-medium">初始化配置环境</span>
            </div>
            <p className="text-xs text-muted-foreground leading-relaxed">
              在目标机器的命令行终端，解压并运行初始化配置命令：
              <code className="block mt-1.5 rounded bg-muted px-2 py-1 font-mono text-[11px] border border-border/50 text-foreground">mbox setup</code>
            </p>
          </div>

          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <span className="flex size-5 items-center justify-center rounded-full bg-primary/10 text-xs font-semibold text-primary">3</span>
              <span className="text-sm font-medium">浏览器登录授权</span>
            </div>
            <p className="text-xs text-muted-foreground leading-relaxed">
              执行以下登录指令，系统会拉起浏览器完成安全的账号授权：
              <code className="block mt-1.5 rounded bg-muted px-2 py-1 font-mono text-[11px] border border-border/50 text-foreground">mbox account login</code>
            </p>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

const statusColors: Record<string, string> = {
  online: "bg-emerald-500/10 text-emerald-700 border-emerald-500/20 dark:bg-emerald-500/20 dark:text-emerald-300 border",
  offline: "bg-rose-500/10 text-rose-700 border-rose-500/20 dark:bg-rose-500/20 dark:text-rose-300 border",
  draining: "bg-amber-500/10 text-amber-700 border-amber-500/20 dark:bg-amber-500/20 dark:text-amber-300 border",
  revoked: "bg-zinc-500/10 text-zinc-700 border-zinc-500/20 dark:bg-zinc-500/20 dark:text-zinc-300 border",
};

const statusLabels: Record<string, string> = {
  online: "在线",
  offline: "离线",
  draining: "等待任务完成",
  revoked: "已撤销",
};

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
    <Card className="flex flex-col h-full hover:shadow-md transition-shadow">
      <CardHeader className="pb-3 border-b border-border/50">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <BoxIcon className="size-4 shrink-0 text-muted-foreground" />
              <span className="truncate">{box.name}</span>
            </CardTitle>
            <CardDescription className="mt-1 font-mono text-[10px] select-all tracking-wider text-muted-foreground">
              {box.box_id}
            </CardDescription>
          </div>
          <Badge className={cn("font-medium shrink-0 shadow-none px-2 py-0.5 text-xs", statusColors[box.status] ?? "bg-zinc-500/10 text-zinc-700")}>
            {statusLabels[box.status] ?? box.status}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-4 flex-1 flex flex-col justify-between">
        <div className="space-y-4">
          {box.legacy_reauthorization_required ? (
            <p className="rounded-md bg-amber-500/10 p-2.5 text-[11px] text-amber-700">
              旧版 Project-scoped Box 必须重新完成账号级设备授权。
            </p>
          ) : null}
          <div className="grid grid-cols-2 gap-2">
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
        </div>
        <div className="space-y-3 pt-3 border-t border-border/50">
          <div className="flex gap-2">
            <Input
              aria-label="Box 名称"
              maxLength={200}
              onChange={(event) => setName(event.target.value)}
              value={name}
              className="h-8 text-xs bg-muted/40"
            />
            <Button
              disabled={pending || !name.trim() || name.trim() === box.name}
              onClick={() => onRename(name.trim())}
              size="sm"
              className="h-8 text-xs shrink-0"
            >
              <Pencil className="size-3 mr-1" />
              保存
            </Button>
          </div>
          <div className="flex flex-wrap gap-2">
            {box.status !== "revoked" ? (
              <Button
                disabled={pending}
                onClick={() => onRevoke("drain")}
                size="sm"
                variant="outline"
                className="h-8 text-xs text-muted-foreground hover:text-foreground"
              >
                <Unplug className="size-3 mr-1" />
                排空任务后撤销
              </Button>
            ) : null}
            {box.status !== "revoked" ? (
              <Button
                className="bg-destructive/10 text-destructive border border-destructive/20 hover:bg-destructive hover:text-destructive-foreground h-8 text-xs transition-colors"
                disabled={pending}
                onClick={forceRevoke}
                size="sm"
              >
                <ShieldAlert className="size-3 mr-1" />
                强制撤销
              </Button>
            ) : null}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function Metric({ label, value }: Readonly<{ label: string; value: string }>) {
  return (
    <div className="bg-muted/30 p-2.5 rounded-lg border border-border/40 flex flex-col justify-center min-h-[56px] transition-colors hover:bg-muted/50">
      <p className="text-[10px] uppercase tracking-wider text-muted-foreground font-semibold">{label}</p>
      <p className="mt-0.5 break-all text-xs font-semibold text-foreground leading-tight">{value}</p>
    </div>
  );
}
