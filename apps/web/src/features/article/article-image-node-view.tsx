"use client";

import type { NodeViewProps } from "@tiptap/react";
import { NodeViewWrapper } from "@tiptap/react";
import { ImageOff, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";

import {
  imageAlignmentStyle,
  normalizeAlignment,
  normalizeImageWidth,
} from "./article-image-utils";

export function ArticleImageNodeView({ node, selected }: NodeViewProps) {
  const src = String(node.attrs.src ?? "").trim();
  const alt = String(node.attrs.alt ?? "");
  const caption = String(node.attrs.caption ?? "").trim();
  const align = normalizeAlignment(node.attrs.align);
  const width = normalizeImageWidth(node.attrs.width);
  const [loadedSrc, setLoadedSrc] = useState<string>();
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    setLoadedSrc(undefined);
    setFailed(false);
    setAttempt(0);
  }, [src]);

  return (
    <NodeViewWrapper
      as="figure"
      className={`article-figure my-4 ${selected ? "rounded-lg ring-2 ring-primary/30" : ""}`}
      data-article-image="true"
      data-article-node-kind="image"
      style={{ textAlign: align }}
    >
      <div className="relative min-h-24 overflow-hidden rounded-lg border bg-muted/20">
        {src && !failed ? (
          <>
            {!loadedSrc ? (
              <div className="absolute inset-0 animate-pulse bg-muted/40" />
            ) : null}
            <img
              alt={alt}
              className={`max-h-[36rem] max-w-full object-contain transition-opacity ${loadedSrc ? "opacity-100" : "opacity-0"}`}
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
