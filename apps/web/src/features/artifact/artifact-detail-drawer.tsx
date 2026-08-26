"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Download,
  Eye,
  ExternalLink,
  FileClock,
  FilePlus2,
  RotateCcw,
  Save,
  Trash2,
  X,
} from "lucide-react";
import Image from "next/image";
import { type FormEvent, useEffect, useState } from "react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { PDFReader } from "@/components/ui/pdf-reader";
import { useCurrentProject } from "@/components/providers/project-provider";

import { artifactApi } from "./artifact-api";
import { ArtifactUploader, formatBytes } from "./artifact-uploader";
import type { ArtifactDetail, PublicArtifactKind } from "./types";

type ArtifactDetailDrawerProps = {
  artifactId?: string;
  initialDetail?: ArtifactDetail;
  onClose: () => void;
  projectId: string;
  trashView: boolean;
};

export function ArtifactDetailDrawer({
  artifactId,
  initialDetail,
  onClose,
  projectId,
  trashView,
}: Readonly<ArtifactDetailDrawerProps>) {
  const project = useCurrentProject();
  const queryClient = useQueryClient();
  const [versionUploaderOpen, setVersionUploaderOpen] = useState(false);
  const [selectedPreviewVersionId, setSelectedPreviewVersionId] =
    useState<string>();
  const detailQueryKey = [
    "artifact",
    projectId,
    artifactId,
    trashView ? "trash" : "active",
  ] as const;
  const detail = useQuery({
    enabled: Boolean(artifactId) && !trashView,
    initialData: initialDetail,
    queryFn: () => artifactApi.get(projectId, artifactId!),
    queryKey: detailQueryKey,
  });
  const versions = useQuery({
    enabled: Boolean(artifactId),
    queryFn: () => artifactApi.listVersions(projectId, artifactId!),
    queryKey: ["artifact-versions", projectId, artifactId],
  });
  const currentVersion = detail.data?.current_version;
  const currentVersionId = currentVersion?.version_id;
  useEffect(() => {
    setSelectedPreviewVersionId(currentVersionId);
  }, [artifactId, currentVersionId]);
  const selectedPreviewVersion =
    versions.data?.items.find(
      (version) => version.version_id === selectedPreviewVersionId,
    ) ??
    (currentVersion?.version_id === selectedPreviewVersionId
      ? currentVersion
      : undefined);
  const previews = useQuery({
    enabled: Boolean(artifactId && !trashView && selectedPreviewVersionId),
    queryFn: () =>
      artifactApi.listPreviews(
        projectId,
        artifactId!,
        selectedPreviewVersionId!,
      ),
    queryKey: [
      "artifact-previews",
      projectId,
      artifactId,
      selectedPreviewVersionId,
    ],
    refetchInterval(query) {
      const items = query.state.data?.items ?? [];
      return items.some((item) =>
        ["queued", "processing"].includes(item.status),
      )
        ? 2_000
        : false;
    },
  });

  async function refresh(updated?: ArtifactDetail) {
    if (updated) {
      queryClient.setQueryData(
        [
          "artifact",
          projectId,
          updated.artifact.artifact_id,
          trashView ? "trash" : "active",
        ],
        updated,
      );
    }
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["artifacts", projectId] }),
      queryClient.invalidateQueries({
        queryKey: ["artifact", projectId, artifactId],
      }),
      queryClient.invalidateQueries({
        queryKey: ["artifact-versions", projectId, artifactId],
      }),
      queryClient.invalidateQueries({
        queryKey: ["artifact-previews", projectId, artifactId],
      }),
      queryClient.invalidateQueries({ queryKey: ["project-home", projectId] }),
    ]);
  }

  const canEdit = ["owner", "maintainer", "editor"].includes(
    project.role ?? "",
  );
  const canManageTrash = ["owner", "maintainer"].includes(project.role ?? "");

  if (!artifactId) {
    return null;
  }

  return (
    <>
      <div
        aria-label="文件详情"
        aria-modal="true"
        className="fixed inset-0 z-40 flex justify-end bg-black/30"
        role="dialog"
      >
        <button
          aria-label="关闭文件详情"
          className="absolute inset-0"
          onClick={onClose}
          type="button"
        />
        <section className="relative z-10 h-full w-full max-w-2xl overflow-y-auto border-l border-border bg-background shadow-xl">
          <header className="sticky top-0 z-10 flex items-start gap-4 border-b border-border bg-background/95 p-6 backdrop-blur">
            <div className="min-w-0 flex-1">
              <p className="text-xs font-medium uppercase tracking-[0.18em] text-muted-foreground">
                Artifact
              </p>
              <h2 className="mt-1 truncate text-xl font-semibold">
                {detail.data?.artifact.name ?? "正在加载文件…"}
              </h2>
            </div>
            <Button
              aria-label="关闭"
              onClick={onClose}
              size="icon"
              variant="ghost"
            >
              <X aria-hidden="true" className="size-4" />
            </Button>
          </header>

          {detail.isLoading ? (
            <p className="p-6 text-sm text-muted-foreground">
              正在读取文件详情…
            </p>
          ) : null}
          {detail.error ? (
            <p className="p-6 text-sm text-destructive">
              {detail.error.message}
            </p>
          ) : null}
          {detail.data ? (
            <div className="grid gap-8 p-6">
              <ArtifactSummary detail={detail.data} />
              <MetadataEditor
                canEdit={canEdit && !trashView}
                detail={detail.data}
                onSaved={(updated) => void refresh(updated)}
                projectId={projectId}
              />
              <VersionPanel
                artifactId={artifactId}
                allowDownload={!trashView}
                allowPreview={!trashView}
                canEdit={canEdit && !trashView}
                currentVersionId={currentVersionId}
                onDownload={async (versionId) => {
                  const grant = await artifactApi.download(
                    projectId,
                    artifactId,
                    versionId,
                  );
                  window.location.assign(grant.transfer.url);
                }}
                onRestore={async (versionId) => {
                  const updated = await artifactApi.restoreVersion(
                    projectId,
                    artifactId,
                    versionId,
                    createActionKey("restore-version"),
                  );
                  toast.success("历史版本已复制为新的当前版本");
                  await refresh(updated);
                }}
                onPreview={setSelectedPreviewVersionId}
                onUpload={() => setVersionUploaderOpen(true)}
                selectedPreviewVersionId={selectedPreviewVersionId}
                versions={versions.data?.items ?? []}
              />
              {!trashView && selectedPreviewVersion ? (
                <PreviewPanel
                  artifactId={artifactId}
                  items={previews.data?.items ?? []}
                  loading={previews.isLoading}
                  version={selectedPreviewVersion}
                />
              ) : null}
              <ArtifactActions
                artifactId={artifactId}
                canManage={canManageTrash}
                onChanged={async () => {
                  await refresh();
                  onClose();
                }}
                projectId={projectId}
                trashView={trashView}
              />
            </div>
          ) : null}
        </section>
      </div>
      <ArtifactUploader
        artifactId={artifactId}
        onClose={() => setVersionUploaderOpen(false)}
        onComplete={(updated) => {
          void refresh(updated);
          setVersionUploaderOpen(false);
        }}
        open={versionUploaderOpen}
        projectId={projectId}
      />
    </>
  );
}

function ArtifactSummary({ detail }: Readonly<{ detail: ArtifactDetail }>) {
  const version = detail.current_version;
  return (
    <section aria-labelledby="artifact-summary-title">
      <h3 className="text-sm font-semibold" id="artifact-summary-title">
        文件信息
      </h3>
      <dl className="mt-3 grid grid-cols-2 gap-x-6 gap-y-4 rounded-xl border border-border bg-muted/20 p-4 text-sm">
        <SummaryTerm label="状态" value={detail.artifact.status} />
        <SummaryTerm label="来源" value={detail.artifact.source} />
        <SummaryTerm label="类型" value={detail.artifact.kind} />
        <SummaryTerm
          label="大小"
          value={version ? formatBytes(version.size_bytes) : "—"}
        />
        <SummaryTerm label="MIME" value={version?.mime_type ?? "—"} />
        <SummaryTerm
          label="版本"
          value={version ? `v${version.version_no}` : "—"}
        />
        <div className="col-span-2">
          <dt className="text-xs text-muted-foreground">SHA-256</dt>
          <dd className="mt-1 break-all font-mono text-xs">
            {version?.sha256 ?? "—"}
          </dd>
        </div>
      </dl>
      <div className="mt-3 flex flex-wrap gap-2">
        {detail.artifact.tags.length > 0 ? (
          detail.artifact.tags.map((tag) => <Badge key={tag}>{tag}</Badge>)
        ) : (
          <span className="text-xs text-muted-foreground">暂无标签</span>
        )}
      </div>
    </section>
  );
}

function SummaryTerm({
  label,
  value,
}: Readonly<{ label: string; value: string }>) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 font-medium">{value}</dd>
    </div>
  );
}

function MetadataEditor({
  canEdit,
  detail,
  onSaved,
  projectId,
}: Readonly<{
  canEdit: boolean;
  detail: ArtifactDetail;
  onSaved: (detail: ArtifactDetail) => void;
  projectId: string;
}>) {
  const [description, setDescription] = useState(
    detail.artifact.description ?? "",
  );
  const [kind, setKind] = useState(detail.artifact.kind);
  const [name, setName] = useState(detail.artifact.name);
  const [tags, setTags] = useState(detail.artifact.tags.join(", "));
  useEffect(() => {
    setDescription(detail.artifact.description ?? "");
    setKind(detail.artifact.kind);
    setName(detail.artifact.name);
    setTags(detail.artifact.tags.join(", "));
  }, [detail]);
  const save = useMutation({
    mutationFn: () =>
      artifactApi.update(projectId, detail.artifact.artifact_id, {
        description: description.trim() || null,
        ...(editableKind ? { kind: kind as PublicArtifactKind } : {}),
        name,
        tags: tags
          .split(",")
          .map((tag) => tag.trim())
          .filter(Boolean),
      }),
    onError(error) {
      toast.error(error.message);
    },
    onSuccess(updated) {
      toast.success("文件元数据已更新");
      onSaved(updated);
    },
  });
  const editableKind = ["problem", "attachment", "other"].includes(kind);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    save.mutate();
  }

  return (
    <section aria-labelledby="artifact-metadata-title">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold" id="artifact-metadata-title">
          展示元数据
        </h3>
        {!canEdit ? <Badge>只读</Badge> : null}
      </div>
      <form className="mt-3 grid gap-4" onSubmit={submit}>
        <label className="grid gap-2 text-sm font-medium">
          展示名称
          <Input
            disabled={!canEdit}
            maxLength={255}
            onChange={(event) => setName(event.target.value)}
            required
            value={name}
          />
        </label>
        <label className="grid gap-2 text-sm font-medium">
          类型
          <select
            className="h-9 rounded-md border border-input bg-background px-3 text-sm disabled:opacity-60"
            disabled={!canEdit || !editableKind}
            onChange={(event) =>
              setKind(event.target.value as PublicArtifactKind)
            }
            value={kind}
          >
            {!editableKind ? <option value={kind}>{kind}</option> : null}
            <option value="problem">题目原始文件</option>
            <option value="attachment">附件</option>
            <option value="other">其他</option>
          </select>
        </label>
        <label className="grid gap-2 text-sm font-medium">
          标签
          <Input
            disabled={!canEdit}
            onChange={(event) => setTags(event.target.value)}
            placeholder="以英文逗号分隔"
            value={tags}
          />
        </label>
        <label className="grid gap-2 text-sm font-medium">
          人工说明
          <textarea
            className="min-h-28 rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
            disabled={!canEdit}
            maxLength={20_000}
            onChange={(event) => setDescription(event.target.value)}
            value={description}
          />
        </label>
        {canEdit ? (
          <Button
            className="justify-self-start"
            disabled={save.isPending}
            type="submit"
          >
            <Save aria-hidden="true" className="size-4" />
            保存元数据
          </Button>
        ) : null}
      </form>
    </section>
  );
}

function PreviewPanel({
  artifactId,
  items,
  loading,
  version,
}: Readonly<{
  artifactId: string;
  items: Awaited<ReturnType<typeof artifactApi.listPreviews>>["items"];
  loading: boolean;
  version: Awaited<
    ReturnType<typeof artifactApi.listVersions>
  >["items"][number];
}>) {
  const thumbnail = items.find(
    (item) =>
      item.preview_type === "thumbnail" &&
      item.status === "available" &&
      item.transfer,
  );
  const primary = items.find(
    (item) => item.preview_type !== "thumbnail" && item.status === "available",
  );
  const preview = primary?.transfer ? primary : undefined;
  const structure =
    primary?.structural_summary &&
    Object.keys(primary.structural_summary).length > 0
      ? primary
      : undefined;
  const pdfSummary =
    primary?.preview_type === "pdf" ? primary.structural_summary : undefined;
  const pdfPageCount =
    typeof pdfSummary?.page_count === "number"
      ? pdfSummary.page_count
      : undefined;
  const pdfText =
    typeof pdfSummary?.text === "string" ? pdfSummary.text : undefined;
  const pdfTextPages =
    typeof pdfSummary?.text_pages_scanned === "number"
      ? pdfSummary.text_pages_scanned
      : undefined;
  const displayedStructure =
    pdfSummary && pdfText
      ? Object.fromEntries(
          Object.entries(pdfSummary).filter(([key]) => key !== "text"),
        )
      : structure?.structural_summary;
  const previewLabel =
    primary?.preview_type === "pdf"
      ? `PDF${pdfPageCount === undefined ? "" : ` · ${pdfPageCount} 页`}`
      : primary?.preview_type === "image"
        ? "图片预览"
        : primary
          ? `${primary.preview_type.toUpperCase()} 预览`
          : undefined;
  const thumbnailLabel =
    primary?.preview_type === "pdf" ? "PDF 首页预览" : "缩略图";
  const previewStatus =
    items.find((item) => item.preview_type !== "thumbnail")?.status ??
    items[0]?.status;
  const statusMessage =
    previewStatus === "queued"
      ? "该版本的预览已排队，Worker 将异步生成。"
      : previewStatus === "processing"
        ? "Worker 正在生成该版本的预览。"
        : previewStatus === "unsupported"
          ? "此格式不支持预览，原文件仍可下载。"
          : previewStatus === "failed"
            ? "预览生成失败，原文件仍可下载。"
            : undefined;
  return (
    <section aria-labelledby="artifact-preview-title">
      <h3 className="text-sm font-semibold" id="artifact-preview-title">
        预览与结构摘要
      </h3>
      <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <span>
          正在查看 v{version.version_no} · {version.filename}
        </span>
        {previewLabel ? <Badge>{previewLabel}</Badge> : null}
      </div>
      {loading ? (
        <p className="mt-3 text-sm text-muted-foreground">正在读取预览状态…</p>
      ) : null}
      {!loading && items.length === 0 ? (
        <p className="mt-3 rounded-lg border border-dashed border-border p-4 text-sm text-muted-foreground">
          预览任务尚未产生结果。
        </p>
      ) : null}
      {thumbnail?.transfer ? (
        <div className="mt-3 overflow-hidden rounded-xl border border-border bg-muted/20">
          <Image
            alt={`${artifactId} v${version.version_no} ${thumbnailLabel}`}
            className="h-auto max-h-80 w-full object-contain"
            height={600}
            src={thumbnail.transfer.url}
            unoptimized
            width={900}
          />
        </div>
      ) : null}
      {primary?.preview_type === "pdf" && preview?.transfer ? (
        <PDFReader
          className="mt-3 block h-[calc(100vh-20rem)] min-h-[28rem] w-full rounded-xl border border-border bg-muted/20"
          title={`${artifactId} v${version.version_no} PDF 预览`}
          transfer={preview.transfer}
        />
      ) : null}
      <div className="mt-3 flex flex-wrap gap-2">
        {preview?.transfer ? (
          <a
            className="inline-flex h-9 items-center gap-2 rounded-md border border-border px-3 text-sm font-medium hover:bg-accent"
            href={preview.transfer.url}
            rel="noreferrer"
            target="_blank"
          >
            <ExternalLink aria-hidden="true" className="size-4" />
            打开安全预览
          </a>
        ) : null}
        {statusMessage ? <Badge>{statusMessage}</Badge> : null}
      </div>
      {pdfText ? (
        <div
          aria-label={`PDF v${version.version_no} 提取文本`}
          className="mt-3 rounded-lg border border-border bg-muted/20 p-4"
        >
          <p className="text-xs font-medium text-muted-foreground">
            有界提取文本
            {pdfTextPages === undefined ? "" : ` · 已扫描前 ${pdfTextPages} 页`}
          </p>
          <p className="mt-2 whitespace-pre-wrap text-sm leading-6">
            {pdfText}
          </p>
        </div>
      ) : null}
      {displayedStructure && Object.keys(displayedStructure).length > 0 ? (
        <pre className="mt-3 max-h-64 overflow-auto rounded-lg bg-muted p-4 text-xs">
          {JSON.stringify(displayedStructure, null, 2)}
        </pre>
      ) : null}
    </section>
  );
}

function VersionPanel({
  allowDownload,
  allowPreview,
  artifactId,
  canEdit,
  currentVersionId,
  onDownload,
  onPreview,
  onRestore,
  onUpload,
  selectedPreviewVersionId,
  versions,
}: Readonly<{
  allowDownload: boolean;
  allowPreview: boolean;
  artifactId: string;
  canEdit: boolean;
  currentVersionId?: string;
  onDownload: (versionId: string) => Promise<void>;
  onPreview: (versionId: string) => void;
  onRestore: (versionId: string) => Promise<void>;
  onUpload: () => void;
  selectedPreviewVersionId?: string;
  versions: Awaited<ReturnType<typeof artifactApi.listVersions>>["items"];
}>) {
  const [pendingAction, setPendingAction] = useState<string>();
  return (
    <section aria-labelledby={`artifact-${artifactId}-versions-title`}>
      <div className="flex items-center justify-between gap-3">
        <h3
          className="text-sm font-semibold"
          id={`artifact-${artifactId}-versions-title`}
        >
          永久保留的版本
        </h3>
        {canEdit ? (
          <Button onClick={onUpload} size="sm" variant="outline">
            <FilePlus2 aria-hidden="true" className="size-4" />
            上传新版本
          </Button>
        ) : null}
      </div>
      <ul className="mt-3 divide-y divide-border rounded-xl border border-border">
        {versions.map((version) => (
          <li
            className="flex flex-wrap items-center gap-3 p-4"
            key={version.version_id}
          >
            <span className="flex size-9 items-center justify-center rounded-lg bg-muted">
              <FileClock aria-hidden="true" className="size-4" />
            </span>
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium">
                v{version.version_no} · {version.filename}
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
                {formatBytes(version.size_bytes)} · {version.status}
                {version.version_id === currentVersionId ? " · 当前版本" : ""}
              </p>
            </div>
            {allowPreview && version.status === "available" ? (
              <Button
                aria-label={`预览版本 ${version.version_no}`}
                aria-pressed={version.version_id === selectedPreviewVersionId}
                onClick={() => onPreview(version.version_id)}
                size="sm"
                variant={
                  version.version_id === selectedPreviewVersionId
                    ? "secondary"
                    : "outline"
                }
              >
                <Eye aria-hidden="true" className="size-4" />
                {version.version_id === selectedPreviewVersionId
                  ? "正在预览"
                  : "预览"}
              </Button>
            ) : null}
            {allowDownload && version.status === "available" ? (
              <Button
                aria-label={`下载版本 ${version.version_no}`}
                disabled={Boolean(pendingAction)}
                onClick={async () => {
                  setPendingAction(`download-${version.version_id}`);
                  try {
                    await onDownload(version.version_id);
                  } catch (error) {
                    toast.error(
                      error instanceof Error ? error.message : "下载授权失败",
                    );
                  } finally {
                    setPendingAction(undefined);
                  }
                }}
                size="icon"
                variant="ghost"
              >
                <Download aria-hidden="true" className="size-4" />
              </Button>
            ) : null}
            {canEdit &&
            version.status === "available" &&
            version.version_id !== currentVersionId ? (
              <Button
                disabled={Boolean(pendingAction)}
                onClick={async () => {
                  setPendingAction(`restore-${version.version_id}`);
                  try {
                    await onRestore(version.version_id);
                  } catch (error) {
                    toast.error(
                      error instanceof Error ? error.message : "恢复版本失败",
                    );
                  } finally {
                    setPendingAction(undefined);
                  }
                }}
                size="sm"
                variant="outline"
              >
                <RotateCcw aria-hidden="true" className="size-4" />
                恢复为新版本
              </Button>
            ) : null}
          </li>
        ))}
      </ul>
    </section>
  );
}

function ArtifactActions({
  artifactId,
  canManage,
  onChanged,
  projectId,
  trashView,
}: Readonly<{
  artifactId: string;
  canManage: boolean;
  onChanged: () => Promise<void>;
  projectId: string;
  trashView: boolean;
}>) {
  const action = useMutation({
    mutationFn: async (kind: "purge" | "restore" | "trash") => {
      if (kind === "purge") {
        await artifactApi.purge(projectId, artifactId);
      } else if (kind === "restore") {
        await artifactApi.restore(projectId, artifactId);
      } else {
        await artifactApi.trash(projectId, artifactId);
      }
      return kind;
    },
    onError(error) {
      toast.error(error.message);
    },
    onSuccess(kind) {
      toast.success(
        kind === "purge"
          ? "文件已永久删除；无其他引用的对象字节已安全清理"
          : kind === "restore"
            ? "文件已从回收站恢复"
            : "文件已移入回收站，全部历史版本仍保留",
      );
      void onChanged();
    },
  });
  if (!canManage) {
    return null;
  }
  return (
    <section
      aria-labelledby="artifact-danger-title"
      className="rounded-xl border border-destructive/30 p-4"
    >
      <h3 className="text-sm font-semibold" id="artifact-danger-title">
        {trashView ? "回收站操作" : "文件生命周期"}
      </h3>
      <p className="mt-1 text-xs text-muted-foreground">
        {trashView
          ? "永久删除不可恢复，仅在 blob 没有其他 Artifact/Version 引用时清除对象字节。"
          : "普通删除仅移入回收站，不会删除文件或任何历史版本。"}
      </p>
      <div className="mt-3 flex flex-wrap gap-2">
        {trashView ? (
          <>
            <Button
              disabled={action.isPending}
              onClick={() => action.mutate("restore")}
              variant="outline"
            >
              <RotateCcw aria-hidden="true" className="size-4" />
              恢复
            </Button>
            <Button
              className="border-destructive/40 text-destructive hover:bg-destructive/10"
              disabled={action.isPending}
              onClick={() => {
                if (
                  window.confirm(
                    "确定永久删除此文件吗？此操作不可撤销，但仍被其他版本引用的 blob 不会被删除。",
                  )
                ) {
                  action.mutate("purge");
                }
              }}
              variant="outline"
            >
              <Trash2 aria-hidden="true" className="size-4" />
              永久删除
            </Button>
          </>
        ) : (
          <Button
            disabled={action.isPending}
            onClick={() => action.mutate("trash")}
            variant="outline"
          >
            <Trash2 aria-hidden="true" className="size-4" />
            移入回收站
          </Button>
        )}
      </div>
    </section>
  );
}

function createActionKey(prefix: string): string {
  return typeof crypto !== "undefined" && "randomUUID" in crypto
    ? `${prefix}-${crypto.randomUUID()}`
    : `${prefix}-${Date.now()}`;
}
