"use client";

import { FolderX, Trash2, X } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";

export function ArtifactFolderDeleteActions({
  compact = false,
  folderName,
  onDelete,
  pending,
}: {
  compact?: boolean;
  folderName: string;
  onDelete: (recursive: boolean) => void;
  pending: boolean;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="relative">
      <Button
        disabled={pending}
        onClick={() => setOpen(true)}
        size={compact ? "sm" : "default"}
        variant="outline"
      >
        删除文件夹
      </Button>
      {open ? (
        <div
          aria-label={`删除文件夹：${folderName}`}
          className="absolute right-0 top-full z-50 mt-1 w-80 rounded-lg border bg-popover p-3 text-popover-foreground shadow-xl"
          role="dialog"
        >
          <div className="flex items-start gap-2">
            <div className="min-w-0 flex-1">
              <p className="font-medium">删除“{folderName}”</p>
              <p className="mt-1 text-xs text-muted-foreground">
                Artifact 不会被删除；它们会移动到项目根目录。
              </p>
            </div>
            <button
              aria-label="取消删除文件夹"
              className="rounded p-1 hover:bg-muted"
              onClick={() => setOpen(false)}
              type="button"
            >
              <X className="size-4" />
            </button>
          </div>
          <div className="mt-3 grid gap-2">
            <Button
              className="border-destructive/40 text-destructive hover:bg-destructive/10 hover:text-destructive"
              disabled={pending}
              onClick={() => {
                setOpen(false);
                onDelete(false);
              }}
              size="sm"
              variant="outline"
            >
              <FolderX className="size-3.5" />
              仅删除当前文件夹
            </Button>
            <p className="text-[11px] text-muted-foreground">
              若存在子文件夹则拒绝操作。
            </p>
            <Button
              disabled={pending}
              onClick={() => {
                setOpen(false);
                onDelete(true);
              }}
              size="sm"
              variant="outline"
            >
              <Trash2 className="size-3.5" />
              递归移除文件夹结构
            </Button>
            <p className="text-[11px] text-muted-foreground">
              同时移除所有子文件夹，但保留其中的 Artifact。
            </p>
          </div>
        </div>
      ) : null}
    </div>
  );
}
