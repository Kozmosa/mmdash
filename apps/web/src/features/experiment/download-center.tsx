import { Box, Download, TerminalSquare } from "lucide-react";

import { developmentDownloads } from "./development-downloads";

const downloadLinkClass =
  "inline-flex h-10 items-center justify-center gap-2 rounded-md border border-border bg-background px-4 text-sm font-medium shadow-xs transition-colors hover:bg-muted";

export function DownloadCenter() {
  return (
    <div className="space-y-12">
      <section className="max-w-3xl">
        <p className="text-sm font-medium text-primary">mmdash</p>
        <h1 className="mt-4 text-4xl font-semibold tracking-tight sm:text-6xl">
          下载 mmdash 工具
        </h1>
        <p className="mt-6 text-lg text-muted-foreground">
          这里提供本地开发环境最近一次构建的静态 Box Gateway 和 CLI。无需登录，
          点击对应平台即可下载。
        </p>
      </section>

      <section className="grid gap-5 md:grid-cols-2" aria-label="开发版下载">
        <article className="rounded-2xl border border-border bg-card p-6 shadow-sm">
          <div className="flex size-11 items-center justify-center rounded-xl bg-primary text-primary-foreground">
            <Box aria-hidden="true" className="size-5" />
          </div>
          <h2 className="mt-5 text-xl font-semibold">mmdash Box Gateway</h2>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">
            在你的 Windows 或 Linux 机器上运行 Box Gateway，连接本地 Runtime，
            并接收 mmdash 实验任务。
          </p>
          <div className="mt-6 flex flex-wrap gap-2">
            <a
              className={downloadLinkClass}
              download={developmentDownloads.box.windows.filename}
              href={developmentDownloads.box.windows.href}
            >
              <Download className="size-4" />
              Windows
            </a>
            <a
              className={downloadLinkClass}
              download={developmentDownloads.box.linux.filename}
              href={developmentDownloads.box.linux.href}
            >
              <Download className="size-4" />
              Linux
            </a>
          </div>
          <p className="mt-4 text-xs text-muted-foreground">
            下载后运行 `mbox setup`，再执行 `mbox account login`。
          </p>
        </article>

        <article className="rounded-2xl border border-border bg-card p-6 shadow-sm">
          <div className="flex size-11 items-center justify-center rounded-xl bg-secondary text-secondary-foreground">
            <TerminalSquare aria-hidden="true" className="size-5" />
          </div>
          <h2 className="mt-5 text-xl font-semibold">mmdash CLI</h2>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">
            用于本地项目、认证和 MCP
            工作流的命令行工具。当前提供开发环境构建版本。
          </p>
          <div className="mt-6 flex flex-wrap gap-2">
            <a
              className={downloadLinkClass}
              download={developmentDownloads.cli.windows.filename}
              href={developmentDownloads.cli.windows.href}
            >
              <Download className="size-4" />
              Windows
            </a>
            <a
              className={downloadLinkClass}
              download={developmentDownloads.cli.linux.filename}
              href={developmentDownloads.cli.linux.href}
            >
              <Download className="size-4" />
              Linux
            </a>
          </div>
          <p className="mt-4 text-xs text-muted-foreground">
            文件由每次启动 `dev.mjs` 时的 Go 构建步骤生成。
          </p>
        </article>
      </section>

      <p className="text-sm text-muted-foreground">
        这是开发下载页；正式版本发布后会在相同位置提供版本化下载。
      </p>
    </div>
  );
}
