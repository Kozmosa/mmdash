"use client";

import { useEffect, useState } from "react";
import {
  Box,
  Download,
  TerminalSquare,
  Copy,
  Check,
  FlaskConical,
} from "lucide-react";
import { toast } from "sonner";

import { cn } from "@/lib/cn";
import { developmentDownloads } from "./development-downloads";

const downloadLinkClass =
  "inline-flex h-10 items-center justify-center gap-2 rounded-md border border-border bg-background px-4 text-sm font-medium shadow-xs transition-colors hover:bg-muted cursor-pointer";

function CodeBlock({ code }: { code: string }) {
  const [copied, setCopied] = useState(false);

  async function copyToClipboard() {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
      toast.success("已复制到剪贴板");
    } catch {
      toast.error("复制失败，请手动选择复制");
    }
  }

  return (
    <div className="relative group/code mt-3">
      <pre className="overflow-x-auto rounded-lg bg-muted p-4 pr-12 text-xs font-mono leading-5 border border-border/40">
        <code>{code}</code>
      </pre>
      <button
        type="button"
        onClick={copyToClipboard}
        className="absolute right-3 top-3 inline-flex size-7 items-center justify-center rounded-md border border-border bg-background text-muted-foreground opacity-0 group-hover/code:opacity-100 focus:opacity-100 transition-opacity hover:bg-muted hover:text-foreground cursor-pointer shadow-xs"
        aria-label="复制代码"
      >
        {copied ? (
          <Check className="size-3.5 text-green-500" />
        ) : (
          <Copy className="size-3.5" />
        )}
      </button>
    </div>
  );
}

export function DownloadCenter() {
  const [activeSection, setActiveSection] = useState("download-center");

  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            setActiveSection(entry.target.id);
          }
        });
      },
      { rootMargin: "-20% 0px -60% 0px" },
    );

    const targets = [
      "download-center",
      "windows-install",
      "linux-install",
      "notes",
    ];
    targets.forEach((id) => {
      const el = document.getElementById(id);
      if (el) observer.observe(el);
    });

    return () => observer.disconnect();
  }, []);

  return (
    <div className="grid grid-cols-1 lg:grid-cols-[220px_1fr] gap-10 items-start">
      {/* Sticky Left Sidebar: Document Outline */}
      <aside className="hidden lg:block sticky top-24 space-y-5 border-r border-border/40 pr-6 h-fit">
        <div className="space-y-1">
          <p className="text-xs font-semibold uppercase tracking-wider text-muted-foreground px-3 mb-3">
            文档导航
          </p>
          <a
            href="#download-center"
            className={cn(
              "flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium transition-colors",
              activeSection === "download-center"
                ? "bg-secondary text-secondary-foreground"
                : "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
            )}
          >
            <Download className="size-4" />
            下载工具
          </a>
          <a
            href="#windows-install"
            className={cn(
              "flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium transition-colors",
              activeSection === "windows-install"
                ? "bg-secondary text-secondary-foreground"
                : "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
            )}
          >
            <TerminalSquare className="size-4" />
            Windows 安装
          </a>
          <a
            href="#linux-install"
            className={cn(
              "flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium transition-colors",
              activeSection === "linux-install"
                ? "bg-secondary text-secondary-foreground"
                : "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
            )}
          >
            <Box className="size-4" />
            Linux 安装
          </a>
          <a
            href="#notes"
            className={cn(
              "flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium transition-colors",
              activeSection === "notes"
                ? "bg-secondary text-secondary-foreground"
                : "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
            )}
          >
            <FlaskConical className="size-4" />
            说明事项
          </a>
        </div>
      </aside>

      {/* Main Content Pane */}
      <div className="space-y-16 min-w-0">
        {/* Section 1: Downloads */}
        <section id="download-center" className="scroll-mt-24 space-y-6">
          <div>
            <h1 className="text-3xl font-bold tracking-tight sm:text-4xl text-foreground">
              下载 mmdash 工具
            </h1>
            <p className="mt-2 text-base text-muted-foreground">
              这里提供 Box Gateway (网关守护服务) 与 CLI (命令行客户端)
              的官方预编译可执行文件，无需登录即可快速下载。
            </p>
          </div>

          <div className="grid gap-6 md:grid-cols-2">
            <article className="rounded-2xl border border-border bg-card p-6 shadow-card hover:shadow-md transition-shadow">
              <div className="flex size-11 items-center justify-center rounded-xl bg-primary text-primary-foreground">
                <Box aria-hidden="true" className="size-5" />
              </div>
              <h2 className="mt-5 text-lg font-semibold">mmdash Box Gateway</h2>
              <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                在你的 Windows 或 Linux 机器上运行 Box Gateway，连接本地
                Docker/e2b Sandbox 运行环境，并随时接收调度实验任务。
              </p>
              <div className="mt-6 flex flex-wrap gap-2">
                <a
                  className={downloadLinkClass}
                  download={developmentDownloads.box.windows.filename}
                  href={developmentDownloads.box.windows.href}
                >
                  <Download className="size-4" />
                  Windows (x64)
                </a>
                <a
                  className={downloadLinkClass}
                  download={developmentDownloads.box.linux.filename}
                  href={developmentDownloads.box.linux.href}
                >
                  <Download className="size-4" />
                  Linux (x64)
                </a>
              </div>
              <p className="mt-4 text-xs text-muted-foreground">
                下载后可运行 `mbox setup`，再执行 `mbox account login`
                完成初次绑定。
              </p>
            </article>

            <article className="rounded-2xl border border-border bg-card p-6 shadow-card hover:shadow-md transition-shadow">
              <div className="flex size-11 items-center justify-center rounded-xl bg-secondary text-secondary-foreground">
                <TerminalSquare aria-hidden="true" className="size-5" />
              </div>
              <h2 className="mt-5 text-lg font-semibold">mmdash CLI</h2>
              <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                本地进行项目环境绑定、认证登录以及启用 local MCP stdio
                桥接开发的重要命令行工具。当前提供开发环境的测试构建版本。
              </p>
              <div className="mt-6 flex flex-wrap gap-2">
                <a
                  className={downloadLinkClass}
                  download={developmentDownloads.cli.windows.filename}
                  href={developmentDownloads.cli.windows.href}
                >
                  <Download className="size-4" />
                  Windows (x64)
                </a>
                <a
                  className={downloadLinkClass}
                  download={developmentDownloads.cli.linux.filename}
                  href={developmentDownloads.cli.linux.href}
                >
                  <Download className="size-4" />
                  Linux (x64)
                </a>
              </div>
              <p className="mt-4 text-xs text-muted-foreground">
                用于本地工作空间的高效管理工具。
              </p>
            </article>
          </div>
        </section>

        {/* Section 2: Windows Install Guide */}
        <section id="windows-install" className="scroll-mt-24 space-y-6">
          <div className="border-b border-border pb-3">
            <h2 className="text-2xl font-bold tracking-tight text-foreground">
              Windows 平台安装教程
            </h2>
            <p className="mt-1 text-sm text-muted-foreground">
              通过 PowerShell 将下载的文件重命名并注册为 Windows
              后台服务的完整命令。
            </p>
          </div>
          <ol className="space-y-8">
            <li>
              <div className="flex items-center gap-2">
                <span className="flex size-5 items-center justify-center rounded-full bg-muted text-[11px] font-bold text-muted-foreground">
                  1
                </span>
                <p className="text-sm font-semibold">放置并重命名文件</p>
              </div>
              <p className="mt-1 text-sm text-muted-foreground">
                将下载的 Box 文件改名为 `mbox.exe`，将 CLI 文件改名为
                `mmdash.exe`，并放入用户目录的 bin 文件夹：
              </p>
              <CodeBlock
                code={String.raw`New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\mmdash\bin"
Move-Item .\mmdash-box-win32-x64.exe "$env:LOCALAPPDATA\mmdash\bin\mbox.exe"
Move-Item .\mmdash-cli-win32-x64.exe "$env:LOCALAPPDATA\mmdash\bin\mmdash.exe"`}
              />
            </li>

            <li>
              <div className="flex items-center gap-2">
                <span className="flex size-5 items-center justify-center rounded-full bg-muted text-[11px] font-bold text-muted-foreground">
                  2
                </span>
                <p className="text-sm font-semibold">添加到系统用户 PATH</p>
              </div>
              <p className="mt-1 text-sm text-muted-foreground">
                将 `%LocalAppData%\mmdash\bin` 目录路径追加到用户的 PATH
                环境变量中，然后重新打开 PowerShell：
              </p>
              <CodeBlock
                code={String.raw`$bin = "$env:LOCALAPPDATA\mmdash\bin"
[Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";" + $bin, "User")
mbox --help
mmdash --help`}
              />
            </li>

            <li>
              <div className="flex items-center gap-2">
                <span className="flex size-5 items-center justify-center rounded-full bg-muted text-[11px] font-bold text-muted-foreground">
                  3
                </span>
                <p className="text-sm font-semibold">初始化并启动后台服务</p>
              </div>
              <p className="mt-1 text-sm text-muted-foreground font-mono leading-relaxed">
                首先在普通 PowerShell 下初始化 mbox
                并登录（浏览器会弹窗授权）。然后**右键点击以管理员身份重新打开
                PowerShell** 来注册和启动 Windows 后台服务：
              </p>
              <CodeBlock
                code={String.raw`mbox setup
mbox account login
# 以下命令必须在“管理员管理员权限运行”的 PowerShell 中执行：
mbox service init
mbox service start
mbox service status
# 查看日志：
mbox service logs`}
              />
            </li>

            <li>
              <div className="flex items-center gap-2">
                <span className="flex size-5 items-center justify-center rounded-full bg-muted text-[11px] font-bold text-muted-foreground">
                  4
                </span>
                <p className="text-sm font-semibold">
                  登录 CLI 命令行工具（可选）
                </p>
              </div>
              <p className="mt-1 text-sm text-muted-foreground">
                CLI 登录是一个独立的终端认证，如果只使用 Box
                服务接收实验可以跳过这一步：
              </p>
              <CodeBlock code="mmdash login\nmmdash doctor" />
            </li>
          </ol>
        </section>

        {/* Section 3: Linux Install Guide */}
        <section id="linux-install" className="scroll-mt-24 space-y-6">
          <div className="border-b border-border pb-3">
            <h2 className="text-2xl font-bold tracking-tight text-foreground">
              Linux 平台安装教程
            </h2>
            <p className="mt-1 text-sm text-muted-foreground">
              在 Linux/Ubuntu 系统下通过 systemd 创建进程管理和开机自启。
            </p>
          </div>
          <ol className="space-y-8">
            <li>
              <div className="flex items-center gap-2">
                <span className="flex size-5 items-center justify-center rounded-full bg-muted text-[11px] font-bold text-muted-foreground">
                  1
                </span>
                <p className="text-sm font-semibold">重命名并赋予可执行权限</p>
              </div>
              <p className="mt-1 text-sm text-muted-foreground">
                将下载的文件重命名，并存放到本地的 `~/.local/bin` 目录中：
              </p>
              <CodeBlock
                code={String.raw`mkdir -p ~/.local/bin
mv ./mmdash-box-linux-x64 ~/.local/bin/mbox
mv ./mmdash-cli-linux-x64 ~/.local/bin/mmdash
chmod +x ~/.local/bin/mbox ~/.local/bin/mmdash`}
              />
            </li>

            <li>
              <div className="flex items-center gap-2">
                <span className="flex size-5 items-center justify-center rounded-full bg-muted text-[11px] font-bold text-muted-foreground">
                  2
                </span>
                <p className="text-sm font-semibold">追加 PATH 环境变量</p>
              </div>
              <p className="mt-1 text-sm text-muted-foreground">
                将目录添加到 Shell 配置的 PATH 环境变量中，使其成为全局命令：
              </p>
              <CodeBlock
                code={String.raw`echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.profile
export PATH="$HOME/.local/bin:$PATH"
mbox --help
mmdash --help`}
              />
            </li>

            <li>
              <div className="flex items-center gap-2">
                <span className="flex size-5 items-center justify-center rounded-full bg-muted text-[11px] font-bold text-muted-foreground">
                  3
                </span>
                <p className="text-sm font-semibold">
                  初始化并以 systemd 后台启动
                </p>
              </div>
              <p className="mt-1 text-sm text-muted-foreground">
                通过 `mbox setup` 配置连接参数并登录绑定，随后以 `sudo`
                管理员权限初始化并配置 systemd 常驻服务：
              </p>
              <CodeBlock
                code={String.raw`mbox setup
mbox account login
# 使用 systemd 托管后台服务：
sudo mbox service init --root "$HOME/.local/share/mmdash-box"
sudo mbox service start
sudo mbox service status`}
              />
            </li>

            <li>
              <div className="flex items-center gap-2">
                <span className="flex size-5 items-center justify-center rounded-full bg-muted text-[11px] font-bold text-muted-foreground">
                  4
                </span>
                <p className="text-sm font-semibold">
                  登录 CLI 命令端认证（可选）
                </p>
              </div>
              <p className="mt-1 text-sm text-muted-foreground">
                对于需要在本地开发桥接 MCP
                的开发者，执行以下命令完成用户身份绑定：
              </p>
              <CodeBlock code="mmdash login\nmmdash doctor" />
            </li>
          </ol>
        </section>

        {/* Section 4: Notes */}
        <section id="notes" className="scroll-mt-24 space-y-4">
          <div className="border-b border-border pb-3">
            <h2 className="text-2xl font-bold tracking-tight text-foreground">
              版本与使用注意事项
            </h2>
          </div>
          <div className="rounded-xl border border-yellow-500/20 bg-yellow-500/5 p-5 text-sm leading-relaxed text-amber-600 dark:text-amber-500 flex items-start gap-3">
            <FlaskConical className="size-5 shrink-0 mt-0.5" />
            <div>
              <p className="font-semibold text-base">开发构建版本说明</p>
              <p className="mt-1">
                当前下载通道分发的是预发布和开发联调的临时二进制镜像。正式版本（v0.1）上线发布后，我们将在此页面提供语义化版本号（Semantic
                Versioning）过滤以及带有数字签名的正式安装包下载。如果在搭建
                Docker Sandbox Runtime
                过程中发生容器端口占用或网络通信中断，可参阅 `docs/development/`
                内的技术指引手册。
              </p>
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}
