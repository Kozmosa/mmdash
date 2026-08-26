"use client";

import {
  Columns3,
  GripHorizontal,
  GripVertical,
  Rows3,
  Trash2,
} from "lucide-react";
import type { PointerEvent as ReactPointerEvent, ReactNode } from "react";

export type ArticleTableEdgeHandle = {
  axis: "column" | "row";
  cellPos: number;
  index: number;
  left: number;
  tablePos: number;
  top: number;
};

export type ArticleTableEdgeAction = "addBefore" | "addAfter" | "delete";

export function ArticleTableEdgeControls({
  handle,
  menuOpen,
  onAction,
  onClose,
  onPointerDown,
  onToggleMenu,
}: Readonly<{
  handle: ArticleTableEdgeHandle;
  menuOpen: boolean;
  onAction: (action: ArticleTableEdgeAction) => void;
  onClose: () => void;
  onPointerDown: (event: ReactPointerEvent<HTMLButtonElement>) => void;
  onToggleMenu: () => void;
}>) {
  const isRow = handle.axis === "row";
  return (
    <div
      className="fixed z-[95]"
      data-article-table-controls
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          onClose();
        }
      }}
      style={{ left: handle.left, top: handle.top }}
    >
      <button
        aria-expanded={menuOpen}
        aria-label={
          isRow
            ? `第 ${handle.index + 1} 行操作`
            : `第 ${handle.index + 1} 列操作`
        }
        className="flex size-6 touch-none cursor-grab items-center justify-center rounded-md border border-primary/40 bg-primary text-primary-foreground shadow-sm hover:bg-primary/90 active:cursor-grabbing"
        onClick={onToggleMenu}
        onPointerDown={onPointerDown}
        type="button"
      >
        {isRow ? (
          <GripVertical className="size-3.5" />
        ) : (
          <GripHorizontal className="size-3.5" />
        )}
      </button>
      {menuOpen ? (
        <div
          className={`absolute z-10 grid w-44 gap-1 rounded-lg border bg-background p-1.5 text-xs shadow-xl ${isRow ? "left-7 top-0" : "left-0 top-7"}`}
          role="menu"
        >
          <TableActionButton onClick={() => onAction("addBefore")}>
            {isRow ? <Rows3 /> : <Columns3 />}
            {isRow ? "在上方插入行" : "在左侧插入列"}
          </TableActionButton>
          <TableActionButton onClick={() => onAction("addAfter")}>
            {isRow ? <Rows3 /> : <Columns3 />}
            {isRow ? "在下方插入行" : "在右侧插入列"}
          </TableActionButton>
          <TableActionButton danger onClick={() => onAction("delete")}>
            <Trash2 />
            {isRow ? "删除当前行" : "删除当前列"}
          </TableActionButton>
        </div>
      ) : null}
    </div>
  );
}

function TableActionButton({
  children,
  danger = false,
  onClick,
}: Readonly<{
  children: ReactNode;
  danger?: boolean;
  onClick: () => void;
}>) {
  return (
    <button
      className={`flex min-h-8 items-center gap-2 rounded px-2 text-left hover:bg-muted [&>svg]:size-3.5 ${danger ? "text-destructive hover:bg-destructive/10" : ""}`}
      onClick={onClick}
      role="menuitem"
      type="button"
    >
      {children}
    </button>
  );
}
