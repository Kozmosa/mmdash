"use client";

import type { NodeViewProps } from "@tiptap/react";
import { NodeViewContent, NodeViewWrapper } from "@tiptap/react";
import { Images } from "lucide-react";

export function ArticleImageGroupNodeView({ node, selected }: NodeViewProps) {
  const caption = String(node.attrs.caption ?? "").trim();
  const count = Math.max(2, node.childCount);
  const requestedColumns = Number(node.attrs.columns);
  const columns = Math.min(
    count,
    Number.isFinite(requestedColumns)
      ? Math.max(1, Math.min(4, Math.round(requestedColumns)))
      : 2,
  );

  return (
    <NodeViewWrapper
      as="figure"
      className={`article-image-group my-4 rounded-xl border bg-muted/10 p-3 ${selected ? "ring-2 ring-primary/30" : ""}`}
      data-article-image-group="true"
      data-article-node-kind="imageGroup"
    >
      <div className="mb-2 flex items-center gap-1.5 text-xs text-muted-foreground">
        <Images className="size-3.5" />
        图片组合 · {count} 张 · 每行 {columns} 张
      </div>
      <NodeViewContent
        className="grid items-start gap-3 [&>.article-figure]:m-0 [&>.article-reference]:m-0 [&>figure]:min-w-0"
        data-article-image-group-content="true"
        style={{
          gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))`,
        }}
      />
      {caption ? (
        <figcaption className="mt-3 border-t pt-2 text-center text-sm font-medium text-muted-foreground">
          {caption}
        </figcaption>
      ) : null}
    </NodeViewWrapper>
  );
}
