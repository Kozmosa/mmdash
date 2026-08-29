"use client";

import type { NodeViewProps } from "@tiptap/react";
import { NodeViewContent, NodeViewWrapper } from "@tiptap/react";
import { Images } from "lucide-react";
import { useEffect, useState, type CSSProperties } from "react";

import {
  articleArtifactMime,
  articleInsertArtifactIntoGroupEvent,
  articleUploadImageIntoGroupEvent,
  parseArticleArtifactDrop,
} from "./article-editor";
import {
  articleImageGroupGridTemplateColumns,
  normalizeArticleImageGroupColumns,
} from "./article-image-group";
import { isImageArtifact } from "./article-image-utils";

export function ArticleImageGroupNodeView({
  editor,
  getPos,
  node,
  selected,
}: NodeViewProps) {
  const [isDragOver, setIsDragOver] = useState(false);
  const caption = String(node.attrs.caption ?? "").trim();
  const count = Math.max(2, node.childCount);
  const columns = Math.min(
    count,
    normalizeArticleImageGroupColumns(node.attrs.columns),
  );
  const gridTemplateColumns = articleImageGroupGridTemplateColumns(columns);
  const canEdit = editor?.isEditable ?? true;

  useEffect(() => {
    const handleDragEnd = () => setIsDragOver(false);
    window.addEventListener("dragend", handleDragEnd);
    window.addEventListener("drop", handleDragEnd);
    return () => {
      window.removeEventListener("dragend", handleDragEnd);
      window.removeEventListener("drop", handleDragEnd);
    };
  }, []);

  const handleDragOver = (event: React.DragEvent<HTMLElement>) => {
    if (!canEdit) return;
    const isArtifact = event.dataTransfer.types.includes(articleArtifactMime);
    const isFile = event.dataTransfer.types.includes("Files");
    if (!isArtifact && !isFile) return;
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = "copy";
    setIsDragOver(true);
  };

  const handleDragLeave = (event: React.DragEvent<HTMLElement>) => {
    if (!event.currentTarget.contains(event.relatedTarget as Node)) {
      setIsDragOver(false);
    }
  };

  const handleDrop = (event: React.DragEvent<HTMLElement>) => {
    setIsDragOver(false);
    if (!canEdit || !editor) return;
    const groupPos = typeof getPos === "function" ? getPos() : undefined;
    if (groupPos === undefined) return;

    if (event.dataTransfer.types.includes(articleArtifactMime)) {
      event.preventDefault();
      event.stopPropagation();
      const raw = event.dataTransfer.getData(articleArtifactMime);
      if (!raw) return;
      try {
        const payload = parseArticleArtifactDrop(raw);
        if (!isImageArtifact(payload)) return;
        window.dispatchEvent(
          new CustomEvent(articleInsertArtifactIntoGroupEvent, {
            detail: {
              groupPos,
              insertIndex: node.childCount,
              payload,
            },
          }),
        );
      } catch {
        // ignore
      }
      return;
    }

    const localImage = Array.from(event.dataTransfer?.files ?? []).find(
      (item) => item.type.startsWith("image/"),
    );
    if (localImage) {
      event.preventDefault();
      event.stopPropagation();
      window.dispatchEvent(
        new CustomEvent(articleUploadImageIntoGroupEvent, {
          detail: {
            file: localImage,
            groupPos,
            insertIndex: node.childCount,
          },
        }),
      );
    }
  };

  return (
    <NodeViewWrapper
      as="figure"
      className={`article-image-group group/image-group relative my-4 rounded-xl p-3 transition-all ${
        selected
          ? "border border-border bg-muted/10 ring-2 ring-primary/30"
          : isDragOver
            ? "border border-primary/60 bg-primary/5 ring-2 ring-primary/20"
            : "border border-transparent hover:border-border/50 hover:bg-muted/5"
      }`}
      data-article-image-group="true"
      data-article-node-kind="imageGroup"
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
    >
      <div
        className={`mb-2 flex items-center justify-between text-xs text-muted-foreground transition-opacity ${
          selected || isDragOver
            ? "opacity-100"
            : "opacity-0 group-hover/image-group:opacity-100"
        }`}
      >
        <div className="flex items-center gap-1.5">
          <Images className="size-3.5" />
          <span>
            图片组合 · {count} 张 · 每行 {columns} 张（宽度自适应）
          </span>
        </div>
        <span className="text-[11px] opacity-75">
          支持拖拽调整图片顺序与添加
        </span>
      </div>
      <NodeViewContent
        className="min-w-0"
        data-article-image-group-content="true"
        style={
          {
            "--article-image-group-columns": gridTemplateColumns,
            "--article-image-group-columns-count": columns,
            "--article-image-group-item-basis": `calc((100% - (${columns} - 1) * 0.75rem) / ${columns} - 1px)`,
          } as CSSProperties
        }
      />
      {caption ? (
        <figcaption className="mt-3 border-t pt-2 text-center text-sm font-medium text-muted-foreground">
          {caption}
        </figcaption>
      ) : null}
    </NodeViewWrapper>
  );
}
