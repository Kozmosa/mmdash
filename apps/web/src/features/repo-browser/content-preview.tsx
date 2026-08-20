"use client";

import Image from "next/image";
import { useEffect, useMemo, useState } from "react";
import { AlertTriangle, FileQuestion } from "lucide-react";

import { CodeEditor } from "@/components/ui/code-editor";
import { MarkdownPreview } from "@/components/ui/markdown-preview";
import { Button } from "@/components/ui/button";
import type { RepoFileContent } from "@/features/repo/types";

export function ContentPreview({
  content,
  projectId,
}: Readonly<{ content: RepoFileContent; projectId?: string }>) {
  const rawUrl = projectId ? repositoryRawUrl(projectId, content) : undefined;
  if (
    content.preview_status === "binary" &&
    isImagePath(content.path) &&
    rawUrl
  ) {
    return <ImagePreview content={content} rawUrl={rawUrl} />;
  }
  if (content.preview_status === "text" && content.content !== null) {
    return <TextPreview content={content} />;
  }
  const message =
    content.preview_status === "text"
      ? {
          description: "Core 将该对象分类为文本，但没有返回可预览内容。",
          title: "文本内容不可用",
        }
      : previewMessages[content.preview_status];
  return (
    <div
      className="flex min-h-72 flex-col items-center justify-center rounded-xl border bg-muted/30 p-8 text-center"
      role="status"
    >
      {content.preview_status === "too_large" ? (
        <AlertTriangle
          aria-hidden="true"
          className="mb-3 size-8 text-amber-600"
        />
      ) : (
        <FileQuestion
          aria-hidden="true"
          className="mb-3 size-8 text-muted-foreground"
        />
      )}
      <h3 className="font-semibold">{message.title}</h3>
      <p className="mt-1 max-w-lg text-sm text-muted-foreground">
        {message.description}
      </p>
      <div className="mt-5">
        <ObjectMetadata content={content} />
      </div>
    </div>
  );
}

function ImagePreview({
  content,
  rawUrl,
}: Readonly<{ content: RepoFileContent; rawUrl: string }>) {
  return (
    <div className="space-y-3">
      <ObjectMetadata content={content} />
      <div className="rounded-xl border bg-muted/20 p-2">
        <Image
          alt={content.path}
          className="h-auto w-full object-contain"
          height={900}
          src={rawUrl}
          unoptimized
          width={1200}
        />
      </div>
      <a
        className="text-xs text-muted-foreground underline"
        href={rawUrl}
        rel="noreferrer"
        target="_blank"
      >
        在新窗口打开原图
      </a>
    </div>
  );
}

function TextPreview({ content }: Readonly<{ content: RepoFileContent }>) {
  const csv = isCsvPath(content.path);
  const markdown = isMarkdownPath(content.path);
  const [view, setView] = useState<"source" | "table" | "rendered">("source");
  const rows = useMemo(
    () => (csv && content.content !== null ? parseCsv(content.content) : []),
    [content.content, csv],
  );

  useEffect(() => {
    setView("source");
  }, [content.path, csv, markdown]);

  return (
    <div className="space-y-3">
      <ObjectMetadata content={content} />
      {csv || markdown ? (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-muted-foreground">预览模式</span>
          <Button
            aria-pressed={view === "source"}
            onClick={() => setView("source")}
            size="sm"
            variant={view === "source" ? "secondary" : "outline"}
          >
            源码
          </Button>
          {csv ? (
            <Button
              aria-pressed={view === "table"}
              onClick={() => setView("table")}
              size="sm"
              variant={view === "table" ? "secondary" : "outline"}
            >
              表格
            </Button>
          ) : null}
          {markdown ? (
            <Button
              aria-pressed={view === "rendered"}
              onClick={() => setView("rendered")}
              size="sm"
              variant={view === "rendered" ? "secondary" : "outline"}
            >
              渲染
            </Button>
          ) : null}
        </div>
      ) : null}
      {view === "table" && csv ? (
        <CsvTable rows={rows} />
      ) : view === "rendered" && markdown && content.content !== null ? (
        <MarkdownPreview source={content.content} />
      ) : (
        <CodeEditor
          language={languageForPath(content.path)}
          value={content.content ?? ""}
        />
      )}
    </div>
  );
}

function CsvTable({ rows }: Readonly<{ rows: string[][] }>) {
  if (!rows.length) {
    return (
      <p className="rounded-lg border bg-muted/20 p-4 text-sm text-muted-foreground">
        CSV 没有可展示的行。
      </p>
    );
  }
  const width = Math.max(...rows.map((row) => row.length));
  const [header, ...body] = rows;
  return (
    <div className="overflow-x-auto rounded-lg border">
      <table className="min-w-full text-left text-xs">
        <thead className="bg-muted/95">
          <tr>
            {Array.from({ length: width }, (_, index) => (
              <th
                className="whitespace-nowrap border-b px-3 py-2 font-semibold"
                key={index}
              >
                {header?.[index] ?? `列 ${index + 1}`}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {body.map((row, rowIndex) => (
            <tr className="border-b last:border-0" key={rowIndex}>
              {Array.from({ length: width }, (_, columnIndex) => (
                <td
                  className="max-w-96 whitespace-pre-wrap px-3 py-2 align-top"
                  key={columnIndex}
                >
                  {row[columnIndex] ?? ""}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ObjectMetadata({ content }: Readonly<{ content: RepoFileContent }>) {
  return (
    <dl className="flex flex-wrap gap-x-5 gap-y-1 text-xs text-muted-foreground">
      <div>
        <dt className="inline font-medium text-foreground">Path </dt>
        <dd className="inline">{content.path}</dd>
      </div>
      <div>
        <dt className="inline font-medium text-foreground">Mode </dt>
        <dd className="inline">{content.mode}</dd>
      </div>
      <div>
        <dt className="inline font-medium text-foreground">Size </dt>
        <dd className="inline">{content.size} bytes</dd>
      </div>
      <div>
        <dt className="inline font-medium text-foreground">Object </dt>
        <dd className="inline">{content.object_id.slice(0, 12)}</dd>
      </div>
    </dl>
  );
}

function repositoryRawUrl(projectId: string, content: RepoFileContent): string {
  const query = new URLSearchParams({
    path: content.path,
    revision: content.resolved_revision,
    workspace: content.workspace,
  });
  return `/api/projects/${encodeURIComponent(projectId)}/repository/raw?${query.toString()}`;
}

function isCsvPath(path: string): boolean {
  return path.toLowerCase().endsWith(".csv");
}

function isImagePath(path: string): boolean {
  return /\.(avif|gif|jpe?g|png|svg|webp)$/i.test(path);
}

function isMarkdownPath(path: string): boolean {
  return /\.(md|markdown|mdown)$/i.test(path);
}

function parseCsv(source: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let quoted = false;
  for (let index = 0; index < source.length; index += 1) {
    const character = source[index];
    const next = source[index + 1];
    if (character === '"') {
      if (quoted && next === '"') {
        cell += '"';
        index += 1;
      } else {
        quoted = !quoted;
      }
    } else if (character === "," && !quoted) {
      row.push(cell);
      cell = "";
    } else if ((character === "\n" || character === "\r") && !quoted) {
      if (character === "\r" && next === "\n") index += 1;
      row.push(cell);
      rows.push(row);
      row = [];
      cell = "";
    } else {
      cell += character;
    }
  }
  if (cell !== "" || row.length) {
    row.push(cell);
    rows.push(row);
  }
  return rows;
}

export function languageForPath(path: string): string {
  const extension = path.split(".").at(-1)?.toLowerCase();
  return (
    {
      c: "c",
      cc: "cpp",
      cpp: "cpp",
      css: "css",
      go: "go",
      html: "html",
      java: "java",
      js: "javascript",
      json: "json",
      jsx: "javascript",
      md: "markdown",
      py: "python",
      r: "r",
      rs: "rust",
      sh: "shell",
      sql: "sql",
      ts: "typescript",
      tsx: "typescript",
      yaml: "yaml",
      yml: "yaml",
    }[extension ?? ""] ?? "plaintext"
  );
}

const previewMessages: Record<
  Exclude<RepoFileContent["preview_status"], "text">,
  { description: string; title: string }
> = {
  binary: {
    description:
      "这是二进制对象。Stage 1 只显示安全元数据，不把内容送入编辑器。",
    title: "Binary 文件不可预览",
  },
  lfs_not_materialized: {
    description:
      "该对象是 Git LFS pointer；Core 不在读取路径中自动下载 LFS 内容。",
    title: "Git LFS 内容未物化",
  },
  submodule: {
    description: "该条目指向另一个 Git repository 的固定 commit。",
    title: "Submodule",
  },
  symlink: {
    description: "为避免路径逃逸，符号链接只显示对象元数据，不跟随目标。",
    title: "Symbolic link",
  },
  too_large: {
    description: "对象超过 Repo 文本预览上限。可在本地 Git 工作区中读取。",
    title: "文件过大",
  },
};
