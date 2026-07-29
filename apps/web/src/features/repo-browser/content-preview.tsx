"use client";

import { AlertTriangle, FileQuestion } from "lucide-react";

import { CodeEditor } from "@/components/ui/code-editor";
import type { RepoFileContent } from "@/features/repo/types";

export function ContentPreview({
  content,
}: Readonly<{ content: RepoFileContent }>) {
  if (content.preview_status === "text" && content.content !== null) {
    return (
      <div className="space-y-3">
        <ObjectMetadata content={content} />
        <CodeEditor
          language={languageForPath(content.path)}
          value={content.content}
        />
      </div>
    );
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
