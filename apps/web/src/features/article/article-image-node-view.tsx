"use client";

import type { NodeViewProps } from "@tiptap/react";
import { NodeViewWrapper } from "@tiptap/react";
import { GripVertical, ImageOff, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";

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
  imageAlignmentStyle,
  isImageArtifact,
  normalizeAlignment,
  normalizeImageWidth,
} from "./article-image-utils";

export function ArticleImageNodeView({
  editor,
  getPos,
  node,
  selected,
}: NodeViewProps) {
  const src = String(node.attrs.src ?? "").trim();
  const alt = String(node.attrs.alt ?? "");
  const caption = String(node.attrs.caption ?? "").trim();
  const align = normalizeAlignment(node.attrs.align);
  const width = normalizeImageWidth(node.attrs.width);
  const [loadedSrc, setLoadedSrc] = useState<string>();
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [isDragging, setIsDragging] = useState(false);
  const [dropIndicatorSide, setDropIndicatorSide] = useState<
    "left" | "right"
  >();

  const canEdit = editor?.isEditable ?? true;

  const resolveGroupInfo = () => {
    if (typeof getPos !== "function" || !editor) return undefined;
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

  useEffect(() => {
    setLoadedSrc(undefined);
    setFailed(false);
    setAttempt(0);
  }, [src]);

  return (
    <NodeViewWrapper
      as="figure"
      className={`article-figure relative group/image my-4 select-none ${selected ? "rounded-lg ring-2 ring-primary/30" : ""} ${isDragging ? "opacity-35 scale-[0.98]" : ""}`}
      data-article-image="true"
      data-article-node-kind="image"
      draggable={Boolean(groupInfo && canEdit)}
      onDragEnd={handleDragEnd}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDragStart={handleDragStart}
      onDrop={handleDrop}
      style={{ textAlign: align }}
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
      <div
        className={`relative min-h-24 overflow-hidden rounded-lg transition-colors ${
          loadedSrc ? "bg-transparent" : "border bg-muted/20"
        }`}
      >
        {src && !failed ? (
          <>
            {!loadedSrc ? (
              <div className="absolute inset-0 animate-pulse bg-muted/40" />
            ) : null}
            <img
              alt={alt}
              className={`max-h-[36rem] max-w-full object-contain transition-opacity select-none ${groupInfo ? "pointer-events-none" : ""} ${loadedSrc ? "opacity-100" : "opacity-0"}`}
              data-article-image-preview="true"
              key={`${src}:${attempt}`}
              onError={() => setFailed(true)}
              onLoad={() => setLoadedSrc(src)}
              src={src}
              style={{ ...imageAlignmentStyle(align), width: `${width}%` }}
            />
          </>
        ) : (
          <div className="flex min-h-24 flex-col items-center justify-center gap-2 p-6 text-sm text-muted-foreground">
            <ImageOff className="size-6" />
            <span>{src ? "图片预览加载失败" : "尚未设置图片地址"}</span>
            {src ? (
              <span className="max-w-full truncate text-xs" title={alt || src}>
                {alt || "普通图片"}
              </span>
            ) : null}
            {src ? (
              <Button
                onClick={() => {
                  setLoadedSrc(undefined);
                  setFailed(false);
                  setAttempt((value) => value + 1);
                }}
                size="sm"
                type="button"
                variant="outline"
              >
                <RefreshCw className="size-3.5" />
                重试
              </Button>
            ) : null}
          </div>
        )}
      </div>
      {caption ? (
        <figcaption className="mt-2 text-center text-sm text-muted-foreground">
          {caption}
        </figcaption>
      ) : null}
    </NodeViewWrapper>
  );
}
