"use client";

import {
  ArrowDown,
  ArrowUp,
  Check,
  Clipboard,
  Copy,
  FileCode2,
  Heading1,
  Heading2,
  Heading3,
  MessageSquareQuote,
  Pilcrow,
  Plus,
  Scissors,
  Trash2,
  X,
} from "lucide-react";
import type { ReactNode } from "react";

import type {
  ArticleBlockConversion,
  ArticleBlockInsertDirection,
  ArticleBlockMoveDirection,
} from "./article-block-commands";

export function ArticleBlockMenu({
  author,
  blockId,
  canMoveDown,
  canMoveUp,
  canReview,
  reviewed = false,
  onAction,
  onCopyId,
  onCut,
  onDelete,
  onClose,
  onReview,
  updatedAt,
}: Readonly<{
  author: string;
  blockId: string;
  canMoveDown: boolean;
  canMoveUp: boolean;
  canReview: boolean;
  reviewed?: boolean;
  onAction: (
    action:
      | ArticleBlockConversion
      | ArticleBlockInsertDirection
      | ArticleBlockMoveDirection
      | "duplicate",
  ) => void;
  onCopyId: () => void;
  onCut: () => void;
  onDelete: () => void;
  onClose: () => void;
  onReview: () => void;
  updatedAt?: string;
}>) {
  return (
    <div
      aria-label="块操作菜单"
      className="max-h-[min(34rem,calc(100dvh-1rem))] w-72 overflow-y-auto rounded-lg border bg-popover p-2 text-popover-foreground shadow-xl"
      data-article-block-menu
      onMouseDown={(event) => event.preventDefault()}
      role="menu"
    >
      <div className="relative mb-2 border-b px-2 pb-2 pr-8 text-[11px] text-muted-foreground">
        <p className="font-medium text-foreground">块信息</p>
        <button
          aria-label="关闭块操作菜单"
          className="absolute -right-1 -top-1 flex size-7 items-center justify-center rounded hover:bg-muted hover:text-foreground"
          onClick={onClose}
          type="button"
        >
          <X className="size-3.5" />
        </button>
        <p title={blockId}>ID：{blockId || "未生成"}</p>
        <p>
          作者：{author || "未知"}
          {updatedAt ? ` · 更新于 ${new Date(updatedAt).toLocaleString()}` : ""}
        </p>
      </div>
      <p className="px-2 pb-1 pt-3 text-[11px] text-muted-foreground">
        插入、复制与移动
      </p>
      <div className="grid grid-cols-2 gap-1">
        <MenuButton
          icon={<Plus className="size-3.5" />}
          onClick={() => onAction("before")}
        >
          上方插入空块
        </MenuButton>
        <MenuButton
          icon={<Plus className="size-3.5" />}
          onClick={() => onAction("after")}
        >
          下方插入空块
        </MenuButton>
        <MenuButton
          icon={<Copy className="size-3.5" />}
          onClick={() => onAction("duplicate")}
        >
          复制块
        </MenuButton>
        <MenuButton icon={<Scissors className="size-3.5" />} onClick={onCut}>
          剪切块
        </MenuButton>
        <MenuButton
          icon={<Clipboard className="size-3.5" />}
          onClick={onCopyId}
        >
          复制块 ID
        </MenuButton>
        <MenuButton
          disabled={!canMoveUp}
          icon={<ArrowUp className="size-3.5" />}
          onClick={() => onAction("up")}
        >
          向上移动
        </MenuButton>
        <MenuButton
          disabled={!canMoveDown}
          icon={<ArrowDown className="size-3.5" />}
          onClick={() => onAction("down")}
        >
          向下移动
        </MenuButton>
      </div>
      <p className="px-2 pb-1 pt-3 text-[11px] text-muted-foreground">转换为</p>
      <div className="grid grid-cols-2 gap-1">
        <MenuButton
          icon={<Pilcrow className="size-3.5" />}
          onClick={() => onAction("paragraph")}
        >
          正文
        </MenuButton>
        <MenuButton
          icon={<Heading1 className="size-3.5" />}
          onClick={() => onAction("heading1")}
        >
          一级标题
        </MenuButton>
        <MenuButton
          icon={<Heading2 className="size-3.5" />}
          onClick={() => onAction("heading2")}
        >
          二级标题
        </MenuButton>
        <MenuButton
          icon={<Heading3 className="size-3.5" />}
          onClick={() => onAction("heading3")}
        >
          三级标题
        </MenuButton>
        <MenuButton
          icon={<MessageSquareQuote className="size-3.5" />}
          onClick={() => onAction("blockquote")}
        >
          引用块
        </MenuButton>
        <MenuButton
          icon={<FileCode2 className="size-3.5" />}
          onClick={() => onAction("codeBlock")}
        >
          代码块
        </MenuButton>
      </div>
      <div className="mt-2 grid gap-1 border-t pt-2">
        {canReview ? (
          <MenuButton
            icon={<Check className="size-3.5 text-green-600" />}
            onClick={onReview}
          >
            {reviewed ? "撤回审阅" : "审阅通过"}
          </MenuButton>
        ) : null}
        <button
          className="flex min-h-8 items-center gap-2 rounded px-2 py-1 text-left text-destructive hover:bg-destructive/10"
          onClick={onDelete}
          type="button"
        >
          <Trash2 className="size-3.5" />
          删除块
        </button>
      </div>
    </div>
  );
}

function MenuButton({
  children,
  disabled,
  icon,
  onClick,
}: Readonly<{
  children: ReactNode;
  disabled?: boolean;
  icon?: ReactNode;
  onClick: () => void;
}>) {
  return (
    <button
      className="flex min-h-8 items-center gap-2 rounded px-2 py-1 text-left text-xs hover:bg-muted disabled:cursor-not-allowed disabled:opacity-40"
      disabled={disabled}
      onClick={onClick}
      type="button"
    >
      {icon}
      {children}
    </button>
  );
}
