"use client";

import { Check, Trash2, X } from "lucide-react";
import { useEffect, useRef } from "react";

import type { ArticleMathKind } from "./article-math-shortcuts";

export function ArticleMathEditor({
  kind,
  latex,
  left,
  onChange,
  onClose,
  onDelete,
  onSave,
  placement,
  top,
}: Readonly<{
  kind: ArticleMathKind;
  latex: string;
  left: number;
  onChange: (value: string) => void;
  onClose: () => void;
  onDelete: () => void;
  onSave: () => void;
  placement: "above" | "below";
  top: number;
}>) {
  const inputRef = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);

  return (
    <div
      aria-label={kind === "block" ? "编辑块公式" : "编辑行内公式"}
      className={`fixed z-[120] grid w-[min(24rem,calc(100vw-1rem))] gap-2 rounded-xl border bg-background p-3 text-sm shadow-2xl ${placement === "above" ? "-translate-y-full" : ""}`}
      data-article-math-editor
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          onClose();
        } else if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
          event.preventDefault();
          onSave();
        }
      }}
      role="dialog"
      style={{ left, top }}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium">
          {kind === "block" ? "块公式" : "行内公式"}
        </span>
        <button
          aria-label="关闭公式编辑框"
          className="flex size-7 items-center justify-center rounded hover:bg-muted"
          onClick={onClose}
          type="button"
        >
          <X className="size-4" />
        </button>
      </div>
      <textarea
        aria-label="LaTeX 公式"
        className="min-h-20 resize-y rounded-md border bg-background px-3 py-2 font-mono text-sm outline-none focus:ring-2 focus:ring-ring"
        onChange={(event) => onChange(event.target.value)}
        ref={inputRef}
        spellCheck={false}
        value={latex}
      />
      <div className="flex items-center justify-between gap-2">
        <button
          className="flex min-h-8 items-center gap-1.5 rounded px-2 text-destructive hover:bg-destructive/10"
          onClick={onDelete}
          type="button"
        >
          <Trash2 className="size-3.5" />
          删除公式
        </button>
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">Ctrl+Enter 保存</span>
          <button
            className="flex min-h-8 items-center gap-1.5 rounded bg-primary px-3 text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            disabled={!latex.trim()}
            onClick={onSave}
            type="button"
          >
            <Check className="size-3.5" />
            保存
          </button>
        </div>
      </div>
    </div>
  );
}
