"use client";

import {
  CheckCircle2,
  FileUp,
  Pause,
  Play,
  RotateCcw,
  Trash2,
  X,
} from "lucide-react";
import {
  type ChangeEvent,
  type FormEvent,
  useEffect,
  useRef,
  useState,
} from "react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

import {
  listStoredUploads,
  MultipartUploadTask,
  type StoredArtifactUpload,
  type UploadTaskSnapshot,
} from "./multipart-upload";
import type { ArtifactDetail, PublicArtifactKind } from "./types";

type ArtifactUploaderProps = {
  artifactId?: string;
  defaultKind?: PublicArtifactKind;
  onClose: () => void;
  onComplete: (detail: ArtifactDetail) => void;
  open: boolean;
  projectId: string;
};

const initialSnapshot: UploadTaskSnapshot = {
  completedBytes: 0,
  fileName: "",
  progress: 0,
  status: "idle",
  totalBytes: 0,
};

export function ArtifactUploader({
  artifactId,
  defaultKind = "attachment",
  onClose,
  onComplete,
  open,
  projectId,
}: Readonly<ArtifactUploaderProps>) {
  const [activeTask, setActiveTask] = useState<MultipartUploadTask>();
  const [files, setFiles] = useState<File[]>([]);
  const [recoverable, setRecoverable] = useState<StoredArtifactUpload[]>([]);
  const [snapshot, setSnapshot] = useState<UploadTaskSnapshot>(initialSnapshot);
  const running = useRef(false);

  useEffect(() => {
    if (open && !artifactId) {
      setRecoverable(listStoredUploads(projectId));
    }
  }, [artifactId, open, projectId]);

  useEffect(() => activeTask?.subscribe(setSnapshot), [activeTask]);

  if (!open) {
    return null;
  }

  async function runTask(task: MultipartUploadTask): Promise<void> {
    setActiveTask(task);
    try {
      const detail = await task.start();
      onComplete(detail);
      toast.success(
        artifactId ? "新版本已上传并验证" : "文件已上传并完成完整性验证",
      );
    } catch (error) {
      if (task.getSnapshot().status !== "cancelled") {
        toast.error(error instanceof Error ? error.message : "文件上传失败");
      }
      throw error;
    } finally {
      setRecoverable(listStoredUploads(projectId));
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (files.length === 0 || running.current) {
      return;
    }
    running.current = true;
    const form = new FormData(event.currentTarget);
    const kind = String(form.get("kind") || defaultKind) as PublicArtifactKind;
    const tags = String(form.get("tags") ?? "")
      .split(",")
      .map((tag) => tag.trim())
      .filter(Boolean);
    const description = String(form.get("description") ?? "").trim();
    const displayName = String(form.get("name") ?? "").trim();
    try {
      for (const file of files) {
        await runTask(
          new MultipartUploadTask({
            artifactId,
            description: description || undefined,
            file,
            kind,
            name: files.length === 1 && displayName ? displayName : file.name,
            projectId,
            tags,
          }),
        );
        setFiles((current) => current.filter((item) => item !== file));
      }
    } catch {
      // The active task already presents a safe error and remains recoverable.
    } finally {
      running.current = false;
    }
  }

  async function recoverUpload(
    stored: StoredArtifactUpload,
    event: ChangeEvent<HTMLInputElement>,
  ) {
    const file = event.target.files?.[0];
    if (!file || running.current) {
      return;
    }
    running.current = true;
    try {
      await runTask(new MultipartUploadTask({ file, projectId, stored }));
    } catch {
      // Error state is rendered by the task.
    } finally {
      running.current = false;
      event.target.value = "";
    }
  }

  const taskBusy = [
    "hashing",
    "initializing",
    "uploading",
    "paused",
    "confirming",
  ].includes(snapshot.status);

  return (
    <div
      aria-label={artifactId ? "上传新版本" : "上传文件"}
      aria-modal="true"
      className="fixed inset-0 z-50 flex justify-end bg-black/30"
      role="dialog"
    >
      <button
        aria-label="关闭上传器"
        className="absolute inset-0"
        disabled={taskBusy}
        onClick={onClose}
        type="button"
      />
      <section className="relative z-10 flex h-full w-full max-w-xl flex-col overflow-y-auto border-l border-border bg-background shadow-xl">
        <header className="flex items-start gap-4 border-b border-border p-6">
          <span className="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <FileUp aria-hidden="true" className="size-5" />
          </span>
          <div className="min-w-0 flex-1">
            <h2 className="text-lg font-semibold">
              {artifactId ? "上传不可变新版本" : "上传项目文件"}
            </h2>
            <p className="mt-1 text-sm text-muted-foreground">
              浏览器会分片计算
              SHA-256，并以有界并发上传；不会把整个文件读入内存。
            </p>
          </div>
          <Button
            aria-label="关闭"
            disabled={taskBusy}
            onClick={onClose}
            size="icon"
            variant="ghost"
          >
            <X aria-hidden="true" className="size-4" />
          </Button>
        </header>

        <form className="grid gap-5 p-6" onSubmit={handleSubmit}>
          <label className="grid gap-2 text-sm font-medium">
            选择文件
            <Input
              multiple={!artifactId}
              onChange={(event) =>
                setFiles(Array.from(event.target.files ?? []))
              }
              required={files.length === 0}
              type="file"
            />
          </label>
          {files.length > 0 ? (
            <ul className="grid gap-2">
              {files.map((file) => (
                <li
                  className="flex items-center justify-between rounded-lg border border-border px-3 py-2 text-sm"
                  key={`${file.name}-${file.lastModified}`}
                >
                  <span className="truncate">{file.name}</span>
                  <span className="ml-3 shrink-0 text-xs text-muted-foreground">
                    {formatBytes(file.size)}
                  </span>
                </li>
              ))}
            </ul>
          ) : null}

          {!artifactId ? (
            <>
              <label className="grid gap-2 text-sm font-medium">
                文件类型
                <select
                  className="h-9 rounded-md border border-input bg-background px-3 text-sm"
                  defaultValue={defaultKind}
                  name="kind"
                >
                  <option value="problem">题目原始文件</option>
                  <option value="attachment">附件</option>
                  <option value="other">其他</option>
                </select>
              </label>
              <label className="grid gap-2 text-sm font-medium">
                展示名称
                <Input
                  disabled={files.length > 1}
                  name="name"
                  placeholder={
                    files.length > 1
                      ? "多文件上传时使用各自文件名"
                      : "默认使用文件名"
                  }
                />
              </label>
              <label className="grid gap-2 text-sm font-medium">
                标签
                <Input
                  name="tags"
                  placeholder="以英文逗号分隔，例如 source, baseline"
                />
              </label>
              <label className="grid gap-2 text-sm font-medium">
                说明
                <textarea
                  className="min-h-24 rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  maxLength={20_000}
                  name="description"
                  placeholder="可选的人工说明"
                />
              </label>
            </>
          ) : null}

          {snapshot.status !== "idle" ? (
            <UploadProgress snapshot={snapshot} />
          ) : null}

          <div className="flex flex-wrap gap-2">
            <Button disabled={files.length === 0 || taskBusy} type="submit">
              <FileUp aria-hidden="true" className="size-4" />
              开始上传
            </Button>
            {snapshot.status === "uploading" ? (
              <Button onClick={() => activeTask?.pause()} variant="outline">
                <Pause aria-hidden="true" className="size-4" />
                暂停
              </Button>
            ) : null}
            {snapshot.status === "paused" ? (
              <Button onClick={() => activeTask?.resume()} variant="outline">
                <Play aria-hidden="true" className="size-4" />
                继续
              </Button>
            ) : null}
            {taskBusy ? (
              <Button onClick={() => void activeTask?.cancel()} variant="ghost">
                <Trash2 aria-hidden="true" className="size-4" />
                取消上传
              </Button>
            ) : null}
          </div>
        </form>

        {!artifactId && recoverable.length > 0 ? (
          <section
            aria-labelledby="recoverable-uploads-title"
            className="border-t border-border p-6"
          >
            <h3 className="font-medium" id="recoverable-uploads-title">
              可恢复的上传
            </h3>
            <p className="mt-1 text-sm text-muted-foreground">
              浏览器刷新后请重新选择同一文件；Core 会核对会话和已完成分片。
            </p>
            <ul className="mt-4 grid gap-3">
              {recoverable.map((stored) => (
                <li
                  className="rounded-lg border border-border p-3"
                  key={stored.uploadId}
                >
                  <div className="flex items-center gap-2">
                    <RotateCcw
                      aria-hidden="true"
                      className="size-4 text-muted-foreground"
                    />
                    <span className="min-w-0 flex-1 truncate text-sm font-medium">
                      {stored.fileName}
                    </span>
                    <Badge>{formatBytes(stored.fileSize)}</Badge>
                  </div>
                  <label className="mt-3 inline-flex cursor-pointer items-center gap-2 rounded-md border border-border px-3 py-1.5 text-xs font-medium hover:bg-accent">
                    选择同一文件并继续
                    <input
                      className="sr-only"
                      onChange={(event) => void recoverUpload(stored, event)}
                      type="file"
                    />
                  </label>
                </li>
              ))}
            </ul>
          </section>
        ) : null}
      </section>
    </div>
  );
}

function UploadProgress({
  snapshot,
}: Readonly<{ snapshot: UploadTaskSnapshot }>) {
  const percent = Math.min(
    100,
    Math.max(0, Math.round(snapshot.progress * 100)),
  );
  return (
    <section
      aria-live="polite"
      className="rounded-lg border border-border bg-muted/30 p-4"
    >
      <div className="flex items-center justify-between gap-3 text-sm">
        <span className="font-medium">{statusLabel(snapshot.status)}</span>
        <span className="tabular-nums text-muted-foreground">{percent}%</span>
      </div>
      <div
        aria-label="上传进度"
        aria-valuemax={100}
        aria-valuemin={0}
        aria-valuenow={percent}
        className="mt-3 h-2 overflow-hidden rounded-full bg-muted"
        role="progressbar"
      >
        <div
          className="h-full rounded-full bg-primary transition-[width]"
          style={{ width: `${percent}%` }}
        />
      </div>
      <p className="mt-2 text-xs text-muted-foreground">
        {formatBytes(snapshot.completedBytes)} /{" "}
        {formatBytes(snapshot.totalBytes)}
      </p>
      {snapshot.status === "completed" ? (
        <p className="mt-2 flex items-center gap-2 text-xs text-emerald-600">
          <CheckCircle2 aria-hidden="true" className="size-4" />
          完整大小与 SHA-256 已由 Core 验证
        </p>
      ) : null}
      {snapshot.error ? (
        <p className="mt-2 text-xs text-destructive">
          {snapshot.error.message}
        </p>
      ) : null}
    </section>
  );
}

function statusLabel(status: UploadTaskSnapshot["status"]): string {
  return {
    cancelled: "已取消",
    completed: "已完成",
    confirming: "服务端正在校验完整文件",
    failed: "上传失败",
    hashing: "正在分片计算 SHA-256",
    idle: "等待开始",
    initializing: "正在创建上传会话",
    paused: "已暂停",
    uploading: "正在上传分片",
  }[status];
}

export function formatBytes(value: number): string {
  if (value < 1024) {
    return `${value} B`;
  }
  const units = ["KB", "MB", "GB", "TB"];
  let size = value / 1024;
  let unit = units[0]!;
  for (let index = 1; index < units.length && size >= 1024; index += 1) {
    size /= 1024;
    unit = units[index]!;
  }
  return `${size.toFixed(size >= 10 ? 1 : 2)} ${unit}`;
}
