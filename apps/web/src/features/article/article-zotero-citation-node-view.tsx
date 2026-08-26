"use client";

import type { NodeViewProps } from "@tiptap/react";
import { NodeViewWrapper } from "@tiptap/react";
import { BookOpen, Trash2, X } from "lucide-react";
import { useState } from "react";

export function ZoteroCitationChip({
  canDelete,
  citationKey,
  itemKey,
  onDelete,
  referenceId,
  title,
  version,
}: {
  canDelete: boolean;
  citationKey: string;
  itemKey: string;
  onDelete: () => void;
  referenceId: string;
  title: string;
  version: number;
}) {
  const [open, setOpen] = useState(false);
  return (
    <span className="relative inline-flex align-baseline">
      <button
        aria-expanded={open}
        aria-label={`查看 Zotero 引用：${title || citationKey}`}
        className="inline-flex items-center gap-1 rounded-full border border-primary/20 bg-primary/10 px-2 py-0.5 font-mono text-xs text-primary transition-colors hover:bg-primary/15"
        onClick={() => setOpen((value) => !value)}
        type="button"
      >
        <BookOpen className="size-3" />@{citationKey}
      </button>
      {open ? (
        <span
          className="absolute left-0 top-full z-40 mt-1 block w-72 rounded-lg border bg-popover p-3 text-left font-sans text-xs text-popover-foreground shadow-lg"
          role="dialog"
        >
          <span className="flex items-start gap-2">
            <span className="min-w-0 flex-1">
              <span className="block truncate font-medium">
                {title || `@${citationKey}`}
              </span>
              <span className="mt-1 block text-muted-foreground">
                Zotero item {itemKey || "未知"} · version {version || "未知"}
              </span>
              {referenceId ? (
                <span
                  className="mt-1 block truncate font-mono text-[10px] text-muted-foreground"
                  title={referenceId}
                >
                  固定引用 {referenceId}
                </span>
              ) : null}
            </span>
            <button
              aria-label="关闭引用详情"
              className="rounded p-1 hover:bg-muted"
              onClick={() => setOpen(false)}
              type="button"
            >
              <X className="size-3.5" />
            </button>
          </span>
          {canDelete ? (
            <button
              className="mt-3 inline-flex items-center gap-1 rounded px-2 py-1 text-destructive hover:bg-destructive/10"
              onClick={onDelete}
              type="button"
            >
              <Trash2 className="size-3.5" />
              删除此引用
            </button>
          ) : null}
        </span>
      ) : null}
    </span>
  );
}

export function ArticleZoteroCitationNodeView({
  deleteNode,
  editor,
  node,
}: NodeViewProps) {
  return (
    <NodeViewWrapper as="span" contentEditable={false}>
      <ZoteroCitationChip
        canDelete={editor.isEditable}
        citationKey={String(node.attrs.citationKey ?? "citation")}
        itemKey={String(node.attrs.itemKey ?? "")}
        onDelete={deleteNode}
        referenceId={String(node.attrs.referenceId ?? "")}
        title={String(node.attrs.title ?? "")}
        version={Number(node.attrs.version ?? 0)}
      />
    </NodeViewWrapper>
  );
}
