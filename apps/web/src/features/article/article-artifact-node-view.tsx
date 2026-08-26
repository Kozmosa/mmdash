"use client";

import { useQuery } from "@tanstack/react-query";
import type { NodeViewProps } from "@tiptap/react";
import { NodeViewWrapper } from "@tiptap/react";
import { ImageOff, LoaderCircle, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";
import { artifactApi } from "@/features/artifact/artifact-api";

import {
  availableThumbnailURL,
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
  node,
  projectId,
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
      className="article-reference my-4"
      data-article-artifact-image={isImage ? "true" : undefined}
      data-artifact-reference={artifactId}
      data-article-node-kind={isImage ? "image" : undefined}
      style={isImage ? { textAlign: align } : undefined}
    >
      {isImage ? (
        <div className="overflow-hidden rounded-lg border bg-muted/20">
          {preview.data?.url ? (
            <img
              alt={alt}
              className="max-h-[36rem] max-w-full object-contain"
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
