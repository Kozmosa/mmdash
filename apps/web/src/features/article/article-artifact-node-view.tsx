"use client";

import { useQuery } from "@tanstack/react-query";
import type { NodeViewProps } from "@tiptap/react";
import { NodeViewWrapper } from "@tiptap/react";
import { GripVertical, ImageOff, LoaderCircle, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { artifactApi } from "@/features/artifact/artifact-api";

import {
  articleArtifactMime,
  articleInsertArtifactIntoGroupEvent,
  articleUploadImageIntoGroupEvent,
  parseArticleArtifactDrop,
} from "./article-editor";
import {
  groupAtChildPosition,
  reorderArticleImageInGroup,
} from "./article-image-group";
import {
  availableThumbnailURL,
  isImageArtifact,
  normalizeImageWidth,
} from "./article-image-utils";

const previewRefreshInterval = 4 * 60 * 1_000;

function imageAlignmentStyle(value: unknown) {
  const alignment = value === "left" || value === "right" ? value : "center";
  return {
    marginLeft: alignment === "right" || alignment === "center" ? "auto" : "0",
    marginRight: alignment === "left" || alignment === "center" ? "auto" : "0",
  };
}

export function ArticleArtifactNodeView({
  editor,
  getPos,
  node,
  projectId,
  selected,
}: NodeViewProps & { projectId: string }) {
  const artifactId = String(node.attrs.artifactId ?? node.attrs.objectId ?? "");
  const versionId = String(node.attrs.versionId ?? "");
  const mimeType = String(node.attrs.mimeType ?? "");
  const title = String(node.attrs.title ?? "固定版本引用");
  const alt = String(node.attrs.alt ?? title);
  const align =
    node.attrs.align === "left" || node.attrs.align === "right"
      ? node.attrs.align
      : "center";
  const caption = String(node.attrs.caption ?? "").trim();
  const width = normalizeImageWidth(node.attrs.width);
  const isImage = mimeType.startsWith("image/");
  const [isDragging, setIsDragging] = useState(false);
  const [dropIndicatorSide, setDropIndicatorSide] = useState<
    "left" | "right"
  >();

  const canEdit = editor?.isEditable ?? true;

  const resolveGroupInfo = () => {
    if (typeof getPos !== "function" || !editor || !isImage) return undefined;
    try {
      const currentPos = getPos();
      if (typeof currentPos !== "number") return undefined;
      const loc = groupAtChildPosition(editor, currentPos);
      if (loc) {
        return {
          childIndex: loc.childIndex,
          groupPos: loc.groupPos,
          totalChildren: loc.group.childCount,
        };
      }
      const resolved = editor.state.doc.resolve(currentPos);
      if (resolved.parent.type.name === "articleImageGroup") {
        return {
          childIndex: resolved.index(),
          groupPos: resolved.before(),
          totalChildren: resolved.parent.childCount,
        };
      }
    } catch {
      return undefined;
    }
  };

  const groupInfo = resolveGroupInfo();

  useEffect(() => {
    const handleReset = () => {
      setIsDragging(false);
      setDropIndicatorSide(undefined);
    };
    window.addEventListener("dragend", handleReset);
    window.addEventListener("drop", handleReset);
    return () => {
      window.removeEventListener("dragend", handleReset);
      window.removeEventListener("drop", handleReset);
    };
  }, []);

  const handleDragStart = (event: React.DragEvent<HTMLElement>) => {
    const info = resolveGroupInfo();
    if (!info || !canEdit) return;
    event.stopPropagation();
    setIsDragging(true);
    event.dataTransfer.effectAllowed = "move";
    event.dataTransfer.setData(
      "application/vnd.mmdash.image-group-item",
      JSON.stringify({
        fromIndex: info.childIndex,
        groupPos: info.groupPos,
      }),
    );
    event.dataTransfer.setData(
      "text/plain",
      `mmdash-image-group-item:${info.groupPos}:${info.childIndex}`,
    );
  };

  const handleDragEnd = () => {
    setIsDragging(false);
    setDropIndicatorSide(undefined);
  };

  const handleDragOver = (event: React.DragEvent<HTMLElement>) => {
    const info = resolveGroupInfo();
    if (!canEdit) return;
    const isGroupItem =
      info &&
      (event.dataTransfer.types.includes(
        "application/vnd.mmdash.image-group-item",
      ) ||
        event.dataTransfer.types.includes("text/plain") ||
        event.dataTransfer.types.includes("text"));
    const isArtifact = event.dataTransfer.types.includes(articleArtifactMime);
    const isFile = event.dataTransfer.types.includes("Files");

    if (!isGroupItem && !isArtifact && !isFile) return;

    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = isGroupItem ? "move" : "copy";
    const rect = event.currentTarget.getBoundingClientRect();
    const isAfter = event.clientX > rect.left + rect.width / 2;
    setDropIndicatorSide(isAfter ? "right" : "left");
  };

  const handleDragLeave = (event: React.DragEvent<HTMLElement>) => {
    if (!event.currentTarget.contains(event.relatedTarget as Node)) {
      setDropIndicatorSide(undefined);
    }
  };

  const handleDrop = (event: React.DragEvent<HTMLElement>) => {
    const info = resolveGroupInfo();
    if (!canEdit || !editor) return;
    event.preventDefault();
    event.stopPropagation();
    setDropIndicatorSide(undefined);
    setIsDragging(false);

    // 1. Artifact drop from left sidebar
    if (event.dataTransfer.types.includes(articleArtifactMime)) {
      const raw = event.dataTransfer.getData(articleArtifactMime);
      if (!raw) return;
      try {
        const payload = parseArticleArtifactDrop(raw);
        if (!isImageArtifact(payload)) return;
        const rect = event.currentTarget.getBoundingClientRect();
        const isAfter =
          rect.width > 0 ? event.clientX > rect.left + rect.width / 2 : false;
        const currentPos = typeof getPos === "function" ? getPos() : undefined;
        const insertIndex = info
          ? isAfter
            ? info.childIndex + 1
            : info.childIndex
          : isAfter
            ? 1
            : 0;
        window.dispatchEvent(
          new CustomEvent(articleInsertArtifactIntoGroupEvent, {
            detail: {
              groupPos: info?.groupPos,
              insertIndex,
              payload,
              standalonePos: !info ? currentPos : undefined,
            },
          }),
        );
      } catch {
        // ignore
      }
      return;
    }

    // 2. Local image file drop
    const localImage = Array.from(event.dataTransfer?.files ?? []).find(
      (item) => item.type.startsWith("image/"),
    );
    if (localImage) {
      const rect = event.currentTarget.getBoundingClientRect();
      const isAfter =
        rect.width > 0 ? event.clientX > rect.left + rect.width / 2 : false;
      const currentPos = typeof getPos === "function" ? getPos() : undefined;
      const insertIndex = info
        ? isAfter
          ? info.childIndex + 1
          : info.childIndex
        : isAfter
          ? 1
          : 0;
      window.dispatchEvent(
        new CustomEvent(articleUploadImageIntoGroupEvent, {
          detail: {
            file: localImage,
            groupPos: info?.groupPos,
            insertIndex,
            standalonePos: !info ? currentPos : undefined,
          },
        }),
      );
      return;
    }

    // 3. Existing intra-group reordering
    let fromIndex: number | undefined;
    let groupPos: number | undefined;

    const raw = event.dataTransfer.getData(
      "application/vnd.mmdash.image-group-item",
    );
    if (raw) {
      try {
        const data = JSON.parse(raw);
        fromIndex = data.fromIndex;
        groupPos = data.groupPos;
      } catch {
        // ignore
      }
    }

    if (fromIndex === undefined) {
      const text =
        event.dataTransfer.getData("text/plain") ||
        event.dataTransfer.getData("text");
      if (text?.startsWith("mmdash-image-group-item:")) {
        const parts = text.split(":");
        groupPos = Number(parts[1]);
        fromIndex = Number(parts[2]);
      }
    }

    if (
      !info ||
      fromIndex === undefined ||
      groupPos === undefined ||
      groupPos !== info.groupPos
    ) {
      return;
    }

    const rect = event.currentTarget.getBoundingClientRect();
    const isAfter =
      rect.width > 0 ? event.clientX > rect.left + rect.width / 2 : false;
    let targetIndex = info.childIndex;

    if (fromIndex < info.childIndex) {
      targetIndex =
        !isAfter && info.childIndex > fromIndex + 1
          ? info.childIndex - 1
          : info.childIndex;
    } else if (fromIndex > info.childIndex) {
      targetIndex =
        isAfter && info.childIndex < fromIndex - 1
          ? info.childIndex + 1
          : info.childIndex;
    }

    if (targetIndex < 0) targetIndex = 0;
    if (targetIndex >= info.totalChildren) {
      targetIndex = info.totalChildren - 1;
    }

    if (targetIndex !== fromIndex) {
      reorderArticleImageInGroup(editor, info.groupPos, fromIndex, targetIndex);
    }
  };

  const preview = useQuery({
    enabled: isImage && Boolean(projectId && artifactId && versionId),
    queryFn: async () => {
      try {
        const previews = await artifactApi.listPreviews(
          projectId,
          artifactId,
          versionId,
        );
        const thumbnailURL = availableThumbnailURL(previews.items);
        if (thumbnailURL) {
          return {
            source: "thumbnail" as const,
            url: thumbnailURL,
          };
        }
      } catch {
        // Preview generation is best effort; the immutable original remains usable.
      }
      const original = await artifactApi.download(
        projectId,
        artifactId,
        versionId,
      );
      return { source: "original" as const, url: original.transfer.url };
    },
    queryKey: ["article-artifact-preview", projectId, artifactId, versionId],
    refetchInterval: previewRefreshInterval,
    refetchOnWindowFocus: false,
    staleTime: previewRefreshInterval,
  });

  return (
    <NodeViewWrapper
      className={`article-reference relative group/image my-4 select-none ${selected ? "rounded-lg ring-2 ring-primary/30" : ""} ${isDragging ? "opacity-35 scale-[0.98]" : ""}`}
      data-article-artifact-image={isImage ? "true" : undefined}
      data-artifact-reference={artifactId}
      data-article-node-kind={isImage ? "image" : undefined}
      draggable={Boolean(groupInfo && canEdit)}
      onDragEnd={handleDragEnd}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDragStart={handleDragStart}
      onDrop={handleDrop}
      style={isImage ? { textAlign: align } : undefined}
    >
      {dropIndicatorSide === "left" ? (
        <div className="pointer-events-none absolute -left-1.5 top-1 bottom-1 z-30 flex items-center justify-center">
          <div className="h-full w-0.5 rounded-full bg-foreground dark:bg-white shadow-[0_0_6px_rgba(0,0,0,0.15)] dark:shadow-[0_0_6px_rgba(255,255,255,0.5)]" />
        </div>
      ) : null}
      {dropIndicatorSide === "right" ? (
        <div className="pointer-events-none absolute -right-1.5 top-1 bottom-1 z-30 flex items-center justify-center">
          <div className="h-full w-0.5 rounded-full bg-foreground dark:bg-white shadow-[0_0_6px_rgba(0,0,0,0.15)] dark:shadow-[0_0_6px_rgba(255,255,255,0.5)]" />
        </div>
      ) : null}
      {groupInfo && canEdit ? (
        <div
          aria-label="拖拽调整顺序"
          className={`absolute left-2 top-2 z-20 flex items-center gap-1 rounded-md bg-background/95 border px-2 py-0.5 text-[11px] font-medium text-foreground shadow-sm backdrop-blur select-none cursor-grab active:cursor-grabbing hover:bg-background transition-opacity ${
            selected || isDragging
              ? "opacity-100"
              : "opacity-0 group-hover/image:opacity-100"
          }`}
          data-drag-handle="true"
          draggable={true}
          onDragStart={handleDragStart}
          title="按住拖拽可调整此图片在组合中的顺序"
        >
          <GripVertical className="size-3.5 text-muted-foreground" />
          <span>
            {groupInfo.childIndex + 1}/{groupInfo.totalChildren}
          </span>
        </div>
      ) : null}
      {isImage ? (
        <div
          className={`overflow-hidden rounded-lg transition-colors ${
            preview.data?.url ? "bg-transparent" : "border bg-muted/20"
          }`}
        >
          {preview.data?.url ? (
            <img
              alt={alt}
              className={`max-h-[36rem] max-w-full object-contain transition-opacity select-none ${groupInfo ? "pointer-events-none" : ""}`}
              data-preview-source={preview.data.source}
              onError={() => void preview.refetch()}
              src={preview.data.url}
              style={{ ...imageAlignmentStyle(align), width: `${width}%` }}
            />
          ) : preview.isError ? (
            <div className="flex min-h-40 flex-col items-center justify-center gap-3 p-6 text-sm text-muted-foreground">
              <ImageOff className="size-6" />
              <span>图片预览加载失败</span>
              <span className="max-w-full truncate text-xs" title={title}>
                {title} · {versionId ? versionId.slice(0, 12) : "未知版本"}
              </span>
              <Button
                onClick={() => void preview.refetch()}
                size="sm"
                type="button"
                variant="outline"
              >
                <RefreshCw className="size-3.5" />
                重试
              </Button>
            </div>
          ) : (
            <div className="flex min-h-40 items-center justify-center gap-2 p-6 text-sm text-muted-foreground">
              <LoaderCircle className="size-4 animate-spin" />
              正在加载图片预览…
            </div>
          )}
        </div>
      ) : (
        <aside className="rounded-lg border border-dashed bg-muted/20 p-3 text-sm">
          {title} · {versionId || "未指定版本"}
        </aside>
      )}
      {caption ? (
        <figcaption className="mt-2 text-center text-sm text-muted-foreground">
          {caption}
        </figcaption>
      ) : null}
    </NodeViewWrapper>
  );
}
