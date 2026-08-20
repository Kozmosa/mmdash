"use client";

import {
  ChevronDown,
  ChevronRight,
  CheckSquare2,
  File,
  FileArchive,
  Folder,
  Play,
  RotateCcw,
  Square,
  Terminal,
} from "lucide-react";
import Image from "next/image";
import Link from "next/link";
import { type ReactNode, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { CodeEditor } from "@/components/ui/code-editor";
import { MarkdownPreview } from "@/components/ui/markdown-preview";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  ContentPreview,
  languageForPath,
} from "@/features/repo-browser/content-preview";
import type { RepoFileContent } from "@/features/repo/types";
import { artifactApi } from "@/features/artifact/artifact-api";
import { apiClient } from "@/lib/api-client";
import { cn } from "@/lib/cn";

import type {
  Experiment,
  ExperimentLog,
  ExperimentStatus,
  ResultFile,
} from "./types";

export const activeStatuses: ExperimentStatus[] = [
  "queued",
  "preparing",
  "running",
  "uploading",
  "processing_result",
  "verifying_result",
];

export const terminalStatuses: ExperimentStatus[] = [
  "succeeded",
  "failed",
  "canceled",
  "timed_out",
  "archived",
];

export const rerunnableStatuses: ExperimentStatus[] = [
  "failed",
  "canceled",
  "timed_out",
];

export function ExperimentCard({
  checked,
  compareMode,
  item,
  onCancel,
  onCompare,
  onRerun,
  onRun,
  onSelect,
}: Readonly<{
  checked: boolean;
  compareMode: boolean;
  item: Experiment;
  onCancel: () => void;
  onCompare: () => void;
  onRerun: () => void;
  onRun: () => void;
  onSelect?: () => void;
}>) {
  const terminal = terminalStatuses.includes(item.execution_status);
  const href = `/projects/${encodeURIComponent(item.project_id)}/experiments/${encodeURIComponent(item.experiment_id)}`;
  return (
    <article className="rounded-lg border border-border p-4 transition-colors hover:border-foreground/25">
      <Link className="block text-left" href={href} onClick={onSelect}>
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-semibold">{item.name}</span>
          <Badge>{item.experiment_type}</Badge>
          <StatusBadge status={item.execution_status} />
          {item.connectivity_status === "box_offline" ? (
            <Badge className="border-amber-400 bg-amber-500/10 text-amber-700">
              Box 离线
            </Badge>
          ) : null}
          <span className="ml-auto text-xs text-muted-foreground">
            {new Date(item.updated_at).toLocaleString()}
          </span>
        </div>
        <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-muted">
          <div
            className="h-full rounded-full bg-primary transition-all"
            style={{ width: `${Math.max(0, Math.min(100, item.progress))}%` }}
          />
        </div>
        <p className="mt-2 text-xs text-muted-foreground">
          {item.source_commit.slice(0, 12)} · {item.entrypoint} ·{" "}
          {item.actual_runtime ?? item.requested_runtime_policy}
          {item.box_id ? ` · Box ${item.box_id.slice(0, 8)}` : ""}
        </p>
        {item.failure ? (
          <p className="mt-2 text-sm text-destructive">
            {item.failure.stage} / {item.failure.code}: {item.failure.message}
          </p>
        ) : null}
        {item.summary ? (
          <p className="mt-2 line-clamp-2 text-sm text-muted-foreground">
            {item.summary}
          </p>
        ) : null}
        {item.retry.warning_code ? (
          <p className="mt-2 text-xs text-amber-600">
            已有更新的重跑记录：{item.retry.latest_experiment_id}
          </p>
        ) : null}
      </Link>
      <div className="mt-3 flex flex-wrap gap-2">
        {item.execution_status === "created" ? (
          <Button onClick={onRun} size="sm">
            <Play className="size-3.5" />
            确认运行
          </Button>
        ) : null}
        {!terminal &&
        item.execution_status !== "created" &&
        item.execution_status !== "awaiting_result" ? (
          <Button onClick={onCancel} size="sm" variant="outline">
            <Square className="size-3.5" />
            取消
          </Button>
        ) : null}
        {rerunnableStatuses.includes(item.execution_status) &&
        item.experiment_type !== "self" ? (
          <Button onClick={onRerun} size="sm" variant="outline">
            <RotateCcw className="size-3.5" />
            创建重跑
          </Button>
        ) : null}
        {compareMode ? (
          <Button
            onClick={onCompare}
            size="sm"
            variant={checked ? "default" : "outline"}
          >
            <CheckSquare2 className="size-3.5" />
            {checked ? "已选择" : "加入比较"}
          </Button>
        ) : null}
      </div>
    </article>
  );
}

export function StatusBadge({
  status,
}: Readonly<{ status: ExperimentStatus }>) {
  const label: Record<ExperimentStatus, string> = {
    archived: "已归档",
    awaiting_result: "等待结果",
    canceled: "已取消",
    created: "待确认",
    failed: "失败",
    preparing: "准备中",
    processing_result: "处理结果",
    queued: "排队中",
    running: "运行中",
    succeeded: "已完成",
    timed_out: "超时",
    uploading: "上传结果",
    verifying_result: "验证结果",
  };
  const failure = status === "failed" || status === "timed_out";
  return (
    <Badge
      className={
        failure
          ? "border-destructive/40 bg-destructive/10 text-destructive"
          : undefined
      }
    >
      {label[status]}
    </Badge>
  );
}

export function ExperimentDetail({ item }: Readonly<{ item: Experiment }>) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{item.name}</CardTitle>
        <CardDescription>{item.experiment_id}</CardDescription>
      </CardHeader>
      <CardContent className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-2 text-xs">
        <span className="text-muted-foreground">结果目录</span>
        <code className="break-all">{item.result_directory}</code>
        <span className="text-muted-foreground">项目时区</span>
        <span>{item.project_timezone}</span>
        <span className="text-muted-foreground">Runtime</span>
        <span>
          {item.actual_runtime
            ? `${item.actual_runtime} ${item.runtime_version ?? ""}`
            : item.requested_runtime_policy}
        </span>
        <span className="text-muted-foreground">结果 Commit</span>
        <code className="break-all">
          {item.result_commit_sha ?? "尚未绑定"}
        </code>
        {item.result_contract ? (
          <>
            <span className="text-muted-foreground">自行运行说明</span>
            <span className="whitespace-pre-wrap">
              {item.result_contract.instructions}
            </span>
          </>
        ) : null}
      </CardContent>
    </Card>
  );
}

export function ExperimentTerminal({
  item,
  logs,
}: Readonly<{ item: Experiment; logs: ExperimentLog[] }>) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Terminal className="size-4" />
          只读 Terminal
        </CardTitle>
        <CardDescription>
          实时输出和完整历史日志；Terminal 不提供远程 Shell。
        </CardDescription>
      </CardHeader>
      <CardContent>
        {item.experiment_type === "self" ? (
          <p className="text-sm text-muted-foreground">
            自行运行类型不采集托管日志。Coding Agent push 后通过 MCP 绑定
            Commit。
          </p>
        ) : (
          <>
            {item.logs_truncated ? (
              <p className="mb-3 text-xs text-amber-600">
                Box 磁盘不足，新增日志已停止保存；实验仍会继续运行。
              </p>
            ) : null}
            <pre className="max-h-[32rem] overflow-auto rounded-lg bg-zinc-950 p-4 font-mono text-xs leading-5 text-zinc-200">
              {logs
                .slice()
                .sort((a, b) => a.sequence - b.sequence)
                .map((log) => `[${log.sequence}] ${log.stream}> ${log.message}`)
                .join("\n") || "暂无日志"}
            </pre>
          </>
        )}
      </CardContent>
    </Card>
  );
}

export function ExecutionProgress({ item }: Readonly<{ item: Experiment }>) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>执行进度</CardTitle>
        <CardDescription>
          进度来自 Box Gateway 的阶段状态；Box 离线不会直接把实验标记为失败。
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="h-2 overflow-hidden rounded-full bg-muted">
          <div
            className="h-full rounded-full bg-primary transition-all"
            style={{ width: `${Math.max(0, Math.min(100, item.progress))}%` }}
          />
        </div>
        <p className="mt-2 text-xs text-muted-foreground">
          {item.progress}% ·{" "}
          {item.started_at
            ? `开始于 ${new Date(item.started_at).toLocaleString()}`
            : "尚未开始"}
          {item.finished_at
            ? ` · 结束于 ${new Date(item.finished_at).toLocaleString()}`
            : ""}
        </p>
        {item.failure ? (
          <p className="mt-3 text-sm text-destructive">
            失败阶段：{item.failure.stage}；失败码：{item.failure.code}；
            {item.failure.message}
          </p>
        ) : null}
      </CardContent>
    </Card>
  );
}

export function ResultPanel({
  current,
  files,
  projectId,
}: Readonly<{ current: Experiment; files: ResultFile[]; projectId: string }>) {
  const [selectedPath, setSelectedPath] = useState<string>();
  const complete =
    current.execution_status === "succeeded" ||
    current.execution_status === "archived";
  const selectedFile =
    files.find((file) => file.path === selectedPath) ?? files[0];

  useEffect(() => {
    if (!files.some((file) => file.path === selectedPath)) {
      setSelectedPath(files[0]?.path);
    }
  }, [files, selectedPath]);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <FileArchive className="size-4" />
          结果文件树与预览
        </CardTitle>
        <CardDescription>
          固定到 result commit 的只读视图；选择文件后在右侧预览，Artifact
          文件按其版本授权。
        </CardDescription>
      </CardHeader>
      <CardContent>
        {!complete ? (
          <p className="text-sm text-muted-foreground">
            实验完成并绑定 result commit 后显示文件树。
          </p>
        ) : !files.length ? (
          <p className="text-sm text-muted-foreground">
            结果已绑定，暂无可展示文件。
          </p>
        ) : (
          <div className="grid h-[100vh] min-h-0 min-w-0 gap-4 lg:grid-cols-[minmax(220px,300px)_minmax(0,1fr)]">
            <ResultFileTree
              files={files}
              onSelect={setSelectedPath}
              selectedPath={selectedFile?.path}
            />
            {selectedFile ? (
              <div className="min-h-0 min-w-0 overflow-auto">
                <ResultFilePreview
                  current={current}
                  file={selectedFile}
                  projectId={projectId}
                />
              </div>
            ) : null}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

type ResultTreeNode = {
  children: ResultTreeNode[];
  file?: ResultFile;
  name: string;
  path: string;
};

function ResultFileTree({
  files,
  onSelect,
  selectedPath,
}: Readonly<{
  files: ResultFile[];
  onSelect: (path: string) => void;
  selectedPath?: string;
}>) {
  const root = useMemo(() => buildResultTree(files), [files]);
  return (
    <div
      aria-label="结果文件树"
      className="h-full min-h-0 overflow-auto rounded-lg border bg-muted/10 p-2 text-sm"
      role="tree"
    >
      <ResultTreeLevel
        nodes={root.children}
        onSelect={onSelect}
        selectedPath={selectedPath}
      />
    </div>
  );
}

function ResultTreeLevel({
  nodes,
  onSelect,
  selectedPath,
  level = 1,
}: Readonly<{
  nodes: ResultTreeNode[];
  onSelect: (path: string) => void;
  selectedPath?: string;
  level?: number;
}>) {
  return (
    <div
      className={level === 1 ? undefined : "ml-4"}
      role={level === 1 ? "presentation" : "group"}
    >
      {nodes.map((node) => (
        <ResultTreeItem
          key={node.path}
          level={level}
          node={node}
          onSelect={onSelect}
          selectedPath={selectedPath}
        />
      ))}
    </div>
  );
}

function ResultTreeItem({
  level,
  node,
  onSelect,
  selectedPath,
}: Readonly<{
  level: number;
  node: ResultTreeNode;
  onSelect: (path: string) => void;
  selectedPath?: string;
}>) {
  const directory = node.children.length > 0;
  const [expanded, setExpanded] = useState(level < 2);
  const selected = Boolean(node.file && node.file.path === selectedPath);
  return (
    <div role="none">
      <button
        aria-expanded={directory ? expanded : undefined}
        aria-level={level}
        aria-selected={selected}
        className={cn(
          "flex min-h-9 w-full items-center gap-1.5 rounded-md px-2 text-left outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring",
          selected ? "bg-accent font-medium" : null,
        )}
        onClick={() => {
          if (directory) setExpanded((value) => !value);
          else onSelect(node.file!.path);
        }}
        role="treeitem"
        type="button"
      >
        {directory ? (
          expanded ? (
            <ChevronDown aria-hidden="true" className="size-3.5 shrink-0" />
          ) : (
            <ChevronRight aria-hidden="true" className="size-3.5 shrink-0" />
          )
        ) : (
          <span aria-hidden="true" className="size-3.5 shrink-0" />
        )}
        {directory ? (
          <Folder
            aria-hidden="true"
            className="size-4 shrink-0 fill-muted text-muted-foreground"
          />
        ) : (
          <File
            aria-hidden="true"
            className="size-4 shrink-0 text-muted-foreground"
          />
        )}
        <span className="truncate">{node.name}</span>
        {!directory && node.file ? (
          <Badge className="ml-auto shrink-0 bg-background text-[10px]">
            {node.file.storage_kind}
          </Badge>
        ) : null}
      </button>
      {directory && expanded ? (
        <ResultTreeLevel
          level={level + 1}
          nodes={node.children}
          onSelect={onSelect}
          selectedPath={selectedPath}
        />
      ) : null}
    </div>
  );
}

function buildResultTree(files: ResultFile[]): ResultTreeNode {
  const root: ResultTreeNode = { children: [], name: "/", path: "" };
  for (const file of files) {
    let parent = root;
    const parts = file.path.split("/").filter(Boolean);
    parts.forEach((part, index) => {
      const path = parts.slice(0, index + 1).join("/");
      let node = parent.children.find((child) => child.name === part);
      if (!node) {
        node = { children: [], name: part, path };
        parent.children.push(node);
      }
      if (index === parts.length - 1) node.file = file;
      parent = node;
    });
  }
  sortResultTree(root);
  return root;
}

function sortResultTree(node: ResultTreeNode) {
  node.children.sort((left, right) => {
    const leftDirectory = left.children.length > 0;
    const rightDirectory = right.children.length > 0;
    if (leftDirectory !== rightDirectory) return leftDirectory ? -1 : 1;
    return left.name.localeCompare(right.name);
  });
  node.children.forEach(sortResultTree);
}

function ResultFilePreview({
  current,
  file,
  projectId,
}: Readonly<{ current: Experiment; file: ResultFile; projectId: string }>) {
  if (file.storage_kind === "artifact")
    return <ArtifactResultPreview file={file} projectId={projectId} />;
  return (
    <GitResultPreview current={current} file={file} projectId={projectId} />
  );
}

function GitResultPreview({
  current,
  file,
  projectId,
}: Readonly<{ current: Experiment; file: ResultFile; projectId: string }>) {
  const path = file.repository_path ?? file.path;
  const rawUrl = current.result_commit_sha
    ? repositoryRawUrl(projectId, current.result_commit_sha, path)
    : undefined;
  const content = useQuery({
    enabled: Boolean(current.result_commit_sha && path),
    queryFn: () =>
      apiClient.request<RepoFileContent>(
        `/projects/${encodeURIComponent(projectId)}/repository/content`,
        {
          query: {
            path,
            revision: current.result_commit_sha!,
            workspace: "result",
          },
        },
      ),
    queryKey: [
      "experiment-result-file-content",
      projectId,
      current.result_commit_sha,
      path,
    ],
    retry: false,
  });
  return (
    <div className="min-h-0 min-w-0 space-y-3">
      {!current.result_commit_sha ? (
        <p className="flex min-h-0 items-center justify-center rounded-lg border bg-muted/20 p-8 text-sm text-muted-foreground">
          结果 commit 尚未绑定。
        </p>
      ) : content.isPending ? (
        <p className="flex min-h-0 items-center justify-center rounded-lg border bg-muted/20 p-8 text-sm text-muted-foreground">
          正在读取文件…
        </p>
      ) : content.isError ? (
        <p
          className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
          role="alert"
        >
          无法读取该结果文件：{content.error.message}
        </p>
      ) : content.data ? (
        content.data.preview_status === "text" &&
        content.data.content !== null ? (
          <ResultTextPreview
            metadata={repoObjectMetadata(content.data)}
            path={content.data.path}
            source={content.data.content}
          />
        ) : content.data.preview_status === "binary" &&
          isImagePath(content.data.path) &&
          rawUrl ? (
          <GitImagePreview content={content.data} rawUrl={rawUrl} />
        ) : (
          <div className="min-w-0 space-y-3">
            <ResultObjectMetadata metadata={repoObjectMetadata(content.data)} />
            <ContentPreview content={content.data} />
          </div>
        )
      ) : (
        <p className="flex min-h-72 items-center justify-center rounded-lg border bg-muted/20 p-8 text-sm text-muted-foreground">
          结果 commit 尚未绑定。
        </p>
      )}
    </div>
  );
}

function GitImagePreview({
  content,
  rawUrl,
}: Readonly<{ content: RepoFileContent; rawUrl: string }>) {
  return (
    <div className="min-h-0 min-w-0 space-y-3">
      <ResultObjectMetadata metadata={repoObjectMetadata(content)} />
      <div className="overflow-hidden rounded-lg border bg-muted/20 p-2">
        <Image
          alt={content.path}
          className="h-auto max-h-[calc(100vh-8rem)] w-full object-contain"
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

function repoObjectMetadata(content: RepoFileContent) {
  return {
    mode: content.mode,
    objectId: content.object_id,
    path: content.path,
    size: content.size,
  };
}

type ResultObjectMetadataValue = {
  mode: string;
  objectId: string;
  path: string;
  size: number;
};

function ResultObjectMetadata({
  metadata,
}: Readonly<{ metadata: ResultObjectMetadataValue }>) {
  return (
    <dl className="flex flex-wrap gap-x-5 gap-y-1 text-xs text-muted-foreground">
      <div>
        <dt className="inline font-medium text-foreground">Path </dt>
        <dd className="inline">{metadata.path}</dd>
      </div>
      <div>
        <dt className="inline font-medium text-foreground">Mode </dt>
        <dd className="inline">{metadata.mode}</dd>
      </div>
      <div>
        <dt className="inline font-medium text-foreground">Size </dt>
        <dd className="inline">{metadata.size} bytes</dd>
      </div>
      <div>
        <dt className="inline font-medium text-foreground">Object </dt>
        <dd className="inline">{metadata.objectId.slice(0, 12)}</dd>
      </div>
    </dl>
  );
}

function repositoryRawUrl(
  projectId: string,
  revision: string,
  path: string,
): string {
  const query = new URLSearchParams({ path, revision, workspace: "result" });
  return `/api/projects/${encodeURIComponent(projectId)}/repository/raw?${query.toString()}`;
}

function ArtifactResultPreview({
  file,
  projectId,
}: Readonly<{ file: ResultFile; projectId: string }>) {
  const previews = useQuery({
    enabled: Boolean(file.artifact_id && file.artifact_version_id),
    queryFn: () =>
      artifactApi.listPreviews(
        projectId,
        file.artifact_id!,
        file.artifact_version_id!,
      ),
    queryKey: [
      "experiment-result-artifact-previews",
      projectId,
      file.artifact_id,
      file.artifact_version_id,
    ],
    refetchInterval(query) {
      return (query.state.data?.items ?? []).some((item) =>
        ["queued", "processing"].includes(item.status),
      )
        ? 2_000
        : false;
    },
    retry: false,
  });
  const available = (previews.data?.items ?? []).filter(
    (item) => item.status === "available" && item.transfer,
  );
  const primary =
    available.find((item) => item.preview_type !== "thumbnail") ?? available[0];
  const imageFile =
    file.media_type.toLowerCase().startsWith("image/") ||
    isImagePath(file.path);
  const download = useQuery({
    enabled: Boolean(imageFile && file.artifact_id && file.artifact_version_id),
    queryFn: () =>
      artifactApi.download(
        projectId,
        file.artifact_id!,
        file.artifact_version_id!,
      ),
    queryKey: [
      "experiment-result-artifact-download",
      projectId,
      file.artifact_id,
      file.artifact_version_id,
    ],
    retry: false,
  });
  const imagePreviewUrl =
    primary?.preview_type === "image" || primary?.preview_type === "thumbnail"
      ? primary.transfer?.url
      : imageFile
        ? download.data?.transfer.url
        : undefined;
  const previewStatus = previews.data?.items.find(
    (item) => item.preview_type !== "thumbnail",
  )?.status;
  const textPreview = useQuery({
    enabled: Boolean(
      primary?.transfer && primary && isTextPreviewType(primary.preview_type),
    ),
    queryFn: () =>
      fetchPreviewText(primary!.transfer!.url, primary!.transfer!.headers),
    queryKey: [
      "experiment-result-artifact-preview-content",
      primary?.preview_id,
    ],
    retry: false,
  });
  if (!file.artifact_id || !file.artifact_version_id) {
    return (
      <p className="rounded-lg border bg-muted/20 p-4 text-sm text-muted-foreground">
        该 Artifact 缺少可用的版本引用，暂时无法预览。
      </p>
    );
  }
  if (imageFile && download.data?.transfer.url && !primary) {
    return (
      <div className="min-h-0 min-w-0 space-y-3">
        <ArtifactResultMetadata file={file} />
        <ArtifactImagePreview
          path={file.path}
          url={download.data.transfer.url}
        />
      </div>
    );
  }
  if (previews.isPending)
    return (
      <p className="flex min-h-72 items-center justify-center rounded-lg border bg-muted/20 p-8 text-sm text-muted-foreground">
        正在读取 Artifact 预览…
      </p>
    );
  if (previews.isError)
    return (
      <p
        className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
        role="alert"
      >
        无法读取 Artifact 预览：{previews.error.message}
      </p>
    );
  return (
    <div className="min-w-0 space-y-3">
      <ArtifactResultMetadata file={file} />
      {!primary && !imagePreviewUrl ? (
        <p className="rounded-lg border bg-muted/20 p-4 text-sm text-muted-foreground">
          该 Artifact 暂无可内嵌预览
          {previewStatus ? `（当前状态：${previewStatus}）` : ""}。
        </p>
      ) : imagePreviewUrl ? (
        <ArtifactImagePreview path={file.path} url={imagePreviewUrl} />
      ) : primary?.preview_type === "pdf" ? (
        <iframe
          className="h-[calc(100vh-8rem)] min-h-0 w-full rounded-lg border"
          src={primary.transfer!.url}
          title={`${file.path} PDF 预览`}
        />
      ) : primary && textPreview.isPending ? (
        <p className="rounded-lg border bg-muted/20 p-4 text-sm text-muted-foreground">
          正在加载文本预览…
        </p>
      ) : textPreview.data !== undefined ? (
        <ResultTextPreview
          path={file.path}
          previewType={
            primary && isTextPreviewType(primary.preview_type)
              ? primary.preview_type
              : undefined
          }
          source={textPreview.data}
        />
      ) : (
        <p className="rounded-lg border bg-muted/20 p-4 text-sm text-muted-foreground">
          该 Artifact 暂无可内嵌预览。
          <a
            className="ml-1 underline"
            href={primary.transfer!.url}
            rel="noreferrer"
            target="_blank"
          >
            打开安全预览
          </a>
        </p>
      )}
    </div>
  );
}

function ArtifactResultMetadata({ file }: Readonly<{ file: ResultFile }>) {
  return (
    <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
      <code className="break-all">{file.path}</code>
      <Badge>{file.media_type || "未知类型"}</Badge>
      <span>{formatBytes(file.size_bytes)}</span>
    </div>
  );
}

function ArtifactImagePreview({
  path,
  url,
}: Readonly<{ path: string; url: string }>) {
  return (
    <div className="max-h-[calc(100vh-8rem)] overflow-auto rounded-lg border bg-muted/20 p-2">
      <Image
        alt={path}
        className="h-auto max-h-[calc(100vh-8rem)] w-full object-contain"
        height={900}
        src={url}
        unoptimized
        width={1200}
      />
    </div>
  );
}

function isTextPreviewType(value: string): value is "csv" | "json" | "text" {
  return value === "csv" || value === "json" || value === "text";
}

function ResultTextPreview({
  metadata,
  path,
  previewType,
  source,
}: Readonly<{
  metadata?: ResultObjectMetadataValue;
  path: string;
  previewType?: "csv" | "json" | "text";
  source: string;
}>) {
  const csv = previewType === "csv" || isCsvPath(path);
  const markdown = isMarkdownPath(path);
  const [view, setView] = useState<"source" | "table" | "rendered">("source");
  const rows = useMemo(() => (csv ? parseCsv(source) : []), [csv, source]);

  useEffect(() => {
    setView("source");
  }, [csv, markdown, path, previewType]);

  return (
    <div className="min-w-0 space-y-3">
      {metadata ? <ResultObjectMetadata metadata={metadata} /> : null}
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
      {view === "table" && csv ? (
        <CsvTable rows={rows} />
      ) : view === "rendered" && markdown ? (
        <MarkdownPreview
          className="max-h-[calc(100vh-8rem)] overflow-auto"
          source={source}
        />
      ) : (
        <CodeEditor
          className="h-[calc(100vh-8rem)] min-h-0"
          language={languageForPath(path)}
          value={source}
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
    <div className="max-h-[calc(100vh-8rem)] min-h-0 overflow-auto rounded-lg border">
      <table className="min-w-full text-left text-xs">
        <thead className="sticky top-0 bg-muted/95 backdrop-blur">
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
      if (row.some((value) => value.length > 0)) rows.push(row);
      row = [];
      cell = "";
    } else {
      cell += character;
    }
  }
  if (cell.length > 0 || row.length > 0) {
    row.push(cell);
    if (row.some((value) => value.length > 0)) rows.push(row);
  }
  return rows;
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

async function fetchPreviewText(
  url: string,
  headers: Record<string, string>,
): Promise<string> {
  const response = await fetch(url, { headers });
  if (!response.ok) throw new Error(`预览读取失败（HTTP ${response.status}）`);
  return response.text();
}

function formatBytes(value: number): string {
  if (value < 1_024) return `${value} B`;
  if (value < 1_024 * 1_024) return `${(value / 1_024).toFixed(1)} KB`;
  return `${(value / (1_024 * 1_024)).toFixed(1)} MB`;
}

export function ComparisonPanel({
  items,
  selectedCount,
}: Readonly<{ items: Experiment[]; selectedCount: number }>) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>实验比较</CardTitle>
        <CardDescription>
          {selectedCount < 2
            ? "至少选择两条实验记录。"
            : `正在比较 ${selectedCount} 条实验记录。`}
        </CardDescription>
      </CardHeader>
      {items.length >= 2 ? (
        <CardContent className="overflow-x-auto">
          <table className="w-full min-w-2xl text-left text-sm">
            <thead>
              <tr className="border-b">
                <th className="p-2">实验</th>
                <th className="p-2">状态</th>
                <th className="p-2">Runtime</th>
                <th className="p-2">Commit</th>
                <th className="p-2">结果</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr className="border-b last:border-0" key={item.experiment_id}>
                  <td className="p-2 font-medium">
                    <Link
                      className="hover:underline"
                      href={`/projects/${encodeURIComponent(item.project_id)}/experiments/${encodeURIComponent(item.experiment_id)}`}
                    >
                      {item.name}
                    </Link>
                  </td>
                  <td className="p-2">{item.execution_status}</td>
                  <td className="p-2">
                    {item.actual_runtime ?? item.requested_runtime_policy}
                  </td>
                  <td className="p-2 font-mono text-xs">
                    {item.source_commit.slice(0, 12)}
                  </td>
                  <td className="p-2">
                    {item.summary ??
                      item.result_commit_sha?.slice(0, 12) ??
                      "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      ) : null}
    </Card>
  );
}

export function LabeledSelect({
  children,
  disabled,
  label,
  onChange,
  value,
}: Readonly<{
  children: ReactNode;
  disabled?: boolean;
  label: string;
  onChange: (value: string) => void;
  value: string;
}>) {
  return (
    <label className="grid gap-1.5 text-xs font-medium text-muted-foreground">
      {label}
      <select
        className="h-9 rounded-md border border-input bg-background px-3 text-sm text-foreground disabled:opacity-50"
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
        value={value}
      >
        {children}
      </select>
    </label>
  );
}
