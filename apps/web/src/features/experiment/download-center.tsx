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
          这里提供 Box Gateway 和 CLI
          的可执行文件。无需登录，点击对应平台即可下载。
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
            当前提供开发版 CLI。
          </p>
        </article>
      </section>

      <section aria-labelledby="install-guide-title" className="space-y-6">
        <div>
          <p className="text-sm font-medium text-primary">安装教程</p>
          <h2
            className="mt-2 text-2xl font-semibold tracking-tight"
            id="install-guide-title"
          >
            安装 Box 和 CLI
          </h2>
          <p className="mt-2 text-sm text-muted-foreground">
            两个平台都使用同一套命令：Box 负责常驻运行，CLI 用于项目和 MCP
            操作。
          </p>
        </div>

        <div className="grid gap-5 lg:grid-cols-2">
          <InstallGuide
            commands={[
              {
                description:
                  "将下载的 Box 文件改名为 mbox.exe，将 CLI 文件改名为 mmdash.exe，并放入同一个目录。推荐使用当前用户目录下的 bin 文件夹：",
                code: String.raw`New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\mmdash\bin"
Move-Item .\mmdash-box-win32-x64.exe "$env:LOCALAPPDATA\mmdash\bin\mbox.exe"
Move-Item .\mmdash-cli-win32-x64.exe "$env:LOCALAPPDATA\mmdash\bin\mmdash.exe"`,
                title: "放置并重命名文件",
              },
              {
                description:
                  "将 %LocalAppData%\\mmdash\\bin 添加到当前用户的 PATH。添加后重新打开 PowerShell：",
                code: String.raw`$bin = "$env:LOCALAPPDATA\mmdash\bin"
[Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";" + $bin, "User")
mbox --help
mmdash --help`,
                title: "添加到 PATH",
              },
              {
                description:
                  "初始化 Box、完成浏览器登录，并注册和启动 Windows 服务。setup 会询问 mmdash 地址、Box 名称和 Runtime 设置：",
                code: String.raw`mbox setup
mbox account login
mbox service init
mbox service start
mbox service status`,
                title: "初始化并启动服务",
              },
              {
                description:
                  "CLI 登录是独立的用户命令行认证；如果只使用 Box，可以跳过这一步：",
                code: "mmdash login\nmmdash doctor",
                title: "登录 CLI（可选）",
              },
            ]}
            title="Windows"
          />
          <InstallGuide
            commands={[
              {
                description:
                  "将下载的 Box 文件改名为 mbox，将 CLI 文件改名为 mmdash，放入用户级 ~/.local/bin：",
                code: String.raw`mkdir -p ~/.local/bin
mv ./mmdash-box-linux-x64 ~/.local/bin/mbox
mv ./mmdash-cli-linux-x64 ~/.local/bin/mmdash
chmod +x ~/.local/bin/mbox ~/.local/bin/mmdash`,
                title: "放置并重命名文件",
              },
              {
                description:
                  "将 ~/.local/bin 加入当前用户的 PATH。根据你使用的 Shell 选择对应配置文件，然后重新打开终端：",
                code: String.raw`echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.profile
export PATH="$HOME/.local/bin:$PATH"
mbox --help
mmdash --help`,
                title: "添加到 PATH",
              },
              {
                description:
                  "初始化 Box、完成浏览器登录，并注册和启动 systemd 服务。setup 会询问 mmdash 地址、Box 名称和 Runtime 设置：",
                code: String.raw`mbox setup
mbox account login
mbox service init
mbox service start
mbox service status`,
                title: "初始化并启动服务",
              },
              {
                description:
                  "CLI 登录是独立的用户命令行认证；如果只使用 Box，可以跳过这一步：",
                code: "mmdash login\nmmdash doctor",
                title: "登录 CLI（可选）",
              },
            ]}
            title="Linux"
          />
        </div>
      </section>

      <p className="text-sm text-muted-foreground">
        这是开发下载页；正式版本发布后会在相同位置提供版本化下载。
      </p>
    </div>
  );
}

function InstallGuide({
  commands,
  title,
}: Readonly<{
  commands: Array<{
    code: string;
    description: string;
    title: string;
  }>;
  title: string;
}>) {
  return (
    <article className="rounded-2xl border border-border bg-card p-6 shadow-sm">
      <h3 className="text-xl font-semibold">{title}</h3>
      <ol className="mt-5 space-y-5">
        {commands.map((command, index) => (
          <li key={command.title}>
            <p className="text-sm font-medium">
              {index + 1}. {command.title}
            </p>
            <p className="mt-1 text-sm leading-6 text-muted-foreground">
              {command.description}
            </p>
            <pre className="mt-3 overflow-x-auto rounded-lg bg-muted p-3 text-xs leading-5">
              <code>{command.code}</code>
            </pre>
          </li>
        ))}
      </ol>
    </article>
  );
}
