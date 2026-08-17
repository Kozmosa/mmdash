"use client";

import { useQuery } from "@tanstack/react-query";
import { Box as BoxIcon, TerminalSquare } from "lucide-react";

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

import { experimentApi } from "./api";
import { BoxInstallerCard } from "./box-management";

export function DownloadCenter() {
  const releases = useQuery({
    queryFn: () => experimentApi.boxReleases(),
    queryKey: ["box-releases"],
    staleTime: 60_000,
  });
  return (
    <div className="space-y-6">
      <div>
        <p className="text-sm font-medium text-primary">mmdash 下载中心</p>
        <h1 className="mt-2 text-3xl font-semibold tracking-tight">
          把计算能力带到你的机器
        </h1>
        <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
          这里预留 mmdash 产品介绍和发行版下载位置。首版先提供 Box 下载，CLI
          下载区域会沿用同一发行入口。
        </p>
      </div>
      <BoxInstallerCard
        centerLink={false}
        releases={releases.data?.items ?? []}
      />
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <BoxIcon className="size-4" />
            开发构建（dev.mjs）
          </CardTitle>
          <CardDescription>
            每次启动本地开发环境都会重新编译当前平台的 Box 和
            CLI，并放到本页的开发下载目录。
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-wrap gap-2">
          <a
            className="inline-flex h-9 items-center rounded-md border border-border px-4 text-sm font-medium hover:bg-muted"
            href="/downloads/dev/mmdash-box-win32-x64.exe"
            download
          >
            开发版 Box（Windows）
          </a>
          <a
            className="inline-flex h-9 items-center rounded-md border border-border px-4 text-sm font-medium hover:bg-muted"
            href="/downloads/dev/mmdash-box-linux-x64"
            download
          >
            开发版 Box（Linux）
          </a>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <TerminalSquare className="size-4" />
            mmdash CLI
          </CardTitle>
          <CardDescription>
            CLI 发布入口已预留，后续会与 Box 使用同一版本目录。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <p className="text-sm text-muted-foreground">
            CLI 二进制会由 dev.mjs 在开发环境构建，并在发布后从这里提供下载。
          </p>
          <div className="flex flex-wrap gap-2">
            <a
              className="inline-flex h-9 items-center rounded-md border border-border px-4 text-sm font-medium hover:bg-muted"
              href="/downloads/dev/mmdash-cli-win32-x64.exe"
              download
            >
              开发版 CLI（Windows）
            </a>
            <a
              className="inline-flex h-9 items-center rounded-md border border-border px-4 text-sm font-medium hover:bg-muted"
              href="/downloads/dev/mmdash-cli-linux-x64"
              download
            >
              开发版 CLI（Linux）
            </a>
          </div>
        </CardContent>
      </Card>
      <Card className="border-dashed">
        <CardContent className="flex items-center gap-3 py-5 text-sm text-muted-foreground">
          <BoxIcon className="size-4" />
          Box 管理页面的下载入口统一指向本页，避免暴露内部 Artifact 存储地址。
        </CardContent>
      </Card>
    </div>
  );
}
