"use client";

import {
  AlignCenter,
  AlignLeft,
  AlignRight,
  ArrowLeft,
  ArrowRight,
  Check,
  ChevronDown,
  Images,
  Trash2,
  Ungroup,
} from "lucide-react";
import type { ReactNode } from "react";

import type {
  ArticleImageGroupAction,
  ArticleImageGroupContext,
} from "./article-image-group";

export type ArticleNodeMenuKind = "image" | "imageGroup" | "table";
export type ArticleImageAlignment = "left" | "center" | "right";

type TableAction = "toggleHeaderRow" | "deleteTable";

export function ArticleNodeMenu({
  alt,
  alignment,
  caption,
  groupColumns,
  imageGroupContext,
  kind,
  onAlign,
  onAltChange,
  onApplyAlt,
  onApplyCaption,
  onCaptionChange,
  onDelete,
  onDownload,
  onGroupColumnsChange,
  onImageGroupAction,
  onOpenSource,
  onReplace,
  onReplaceFile,
  onTableAction,
  replaceUrl,
  width,
  onWidthChange,
  onReplaceUrlChange,
  placement = "below",
}: Readonly<{
  alt: string;
  alignment: ArticleImageAlignment;
  caption: string;
  groupColumns: number;
  imageGroupContext?: ArticleImageGroupContext;
  kind: ArticleNodeMenuKind;
  onAlign: (alignment: ArticleImageAlignment) => void;
  onAltChange: (value: string) => void;
  onApplyAlt: () => void;
  onApplyCaption: () => void;
  onCaptionChange: (value: string) => void;
  onDelete: () => void;
  onDownload: () => void;
  onGroupColumnsChange: (columns: number) => void;
  onImageGroupAction?: (action: ArticleImageGroupAction) => void;
  onOpenSource: () => void;
  onReplace: () => void;
  onReplaceFile: () => void;
  onReplaceUrlChange: (value: string) => void;
  placement?: "above" | "below";
  onTableAction: (action: TableAction) => void;
  replaceUrl: string;
  width: number;
  onWidthChange: (value: number) => void;
}>) {
  return (
    <div
      className={`absolute right-0 z-40 max-h-[min(32rem,calc(100dvh-1rem))] w-64 overflow-y-auto rounded-lg border bg-background p-2 text-xs shadow-xl ${placement === "above" ? "bottom-8" : "top-8"}`}
      data-article-node-menu-popover
      onMouseDown={(event) => event.stopPropagation()}
      role="menu"
    >
      {kind === "image" ? (
        <ImageMenu
          alignment={alignment}
          alt={alt}
          caption={caption}
          imageGroupContext={imageGroupContext}
          onAlign={onAlign}
          onAltChange={onAltChange}
          onApplyAlt={onApplyAlt}
          onApplyCaption={onApplyCaption}
          onCaptionChange={onCaptionChange}
          onDelete={onDelete}
          onDownload={onDownload}
          onImageGroupAction={onImageGroupAction}
          onOpenSource={onOpenSource}
          onReplace={onReplace}
          onReplaceFile={onReplaceFile}
          onReplaceUrlChange={onReplaceUrlChange}
          replaceUrl={replaceUrl}
          width={width}
          onWidthChange={onWidthChange}
        />
      ) : kind === "imageGroup" ? (
        <ImageGroupMenu
          caption={caption}
          columns={groupColumns}
          onApplyCaption={onApplyCaption}
          onCaptionChange={onCaptionChange}
          onColumnsChange={onGroupColumnsChange}
          onUngroup={() => onImageGroupAction?.("ungroup")}
        />
      ) : (
        <TableMenu
          caption={caption}
          onApplyCaption={onApplyCaption}
          onCaptionChange={onCaptionChange}
          onTableAction={onTableAction}
        />
      )}
    </div>
  );
}

function ImageMenu({
  alignment,
  alt,
  caption,
  imageGroupContext,
  onAlign,
  onAltChange,
  onApplyAlt,
  onApplyCaption,
  onCaptionChange,
  onDelete,
  onDownload,
  onImageGroupAction,
  onOpenSource,
  onReplace,
  onReplaceFile,
  onReplaceUrlChange,
  replaceUrl,
  width,
  onWidthChange,
}: Readonly<{
  alignment: ArticleImageAlignment;
  alt: string;
  caption: string;
  imageGroupContext?: ArticleImageGroupContext;
  onAlign: (alignment: ArticleImageAlignment) => void;
  onAltChange: (value: string) => void;
  onApplyAlt: () => void;
  onApplyCaption: () => void;
  onCaptionChange: (value: string) => void;
  onDelete: () => void;
  onDownload: () => void;
  onImageGroupAction?: (action: ArticleImageGroupAction) => void;
  onOpenSource: () => void;
  onReplace: () => void;
  onReplaceFile: () => void;
  onReplaceUrlChange: (value: string) => void;
  replaceUrl: string;
  width: number;
  onWidthChange: (value: number) => void;
}>) {
  return (
    <div className="grid gap-2">
      <MenuLabel>图片设置</MenuLabel>
      <MenuField
        label="图注（图片下方）"
        onChange={onCaptionChange}
        value={caption}
      />
      <ActionButton onClick={onApplyCaption}>
        <Check className="size-3.5" />
        保存图注
      </ActionButton>
      <MenuField label="替代文本" onChange={onAltChange} value={alt} />
      <ActionButton onClick={onApplyAlt}>
        <Check className="size-3.5" />
        保存替代文本
      </ActionButton>
      <div className="grid gap-1">
        <span className="text-muted-foreground">对齐</span>
        <div className="grid grid-cols-3 gap-1">
          <IconAction
            active={alignment === "left"}
            label="图片左对齐"
            onClick={() => onAlign("left")}
          >
            <AlignLeft className="size-3.5" />
          </IconAction>
          <IconAction
            active={alignment === "center"}
            label="图片居中"
            onClick={() => onAlign("center")}
          >
            <AlignCenter className="size-3.5" />
          </IconAction>
          <IconAction
            active={alignment === "right"}
            label="图片右对齐"
            onClick={() => onAlign("right")}
          >
            <AlignRight className="size-3.5" />
          </IconAction>
        </div>
      </div>
      <label className="grid gap-1 text-muted-foreground">
        图片宽度（{width}%）
        <input
          aria-label="图片宽度"
          max={100}
          min={20}
          onChange={(event) => onWidthChange(Number(event.target.value))}
          step={5}
          type="range"
          value={width}
        />
      </label>
      <div className="grid gap-1">
        <ActionButton onClick={onReplaceFile}>上传本地图片替换</ActionButton>
        <span className="text-muted-foreground">替换图片（URL）</span>
        <input
          aria-label="替换图片地址"
          className="h-8 rounded border bg-background px-2 outline-none focus:ring-2 focus:ring-ring"
          onChange={(event) => onReplaceUrlChange(event.target.value)}
          placeholder="https://…"
          value={replaceUrl}
        />
        <ActionButton disabled={!replaceUrl.trim()} onClick={onReplace}>
          替换图片
        </ActionButton>
      </div>
      <div className="grid grid-cols-2 gap-1">
        <ActionButton onClick={onOpenSource}>打开源文件</ActionButton>
        <ActionButton onClick={onDownload}>下载原图</ActionButton>
      </div>
      {imageGroupContext?.inGroup ? (
        <div className="grid gap-1 border-t pt-2">
          <MenuLabel>组合内操作</MenuLabel>
          <div className="grid grid-cols-2 gap-1">
            <ActionButton
              disabled={!imageGroupContext.canMoveEarlier}
              onClick={() => onImageGroupAction?.("moveEarlier")}
            >
              <ArrowLeft className="size-3.5" />
              向前移
            </ActionButton>
            <ActionButton
              disabled={!imageGroupContext.canMoveLater}
              onClick={() => onImageGroupAction?.("moveLater")}
            >
              向后移
              <ArrowRight className="size-3.5" />
            </ActionButton>
          </div>
          <ActionButton onClick={() => onImageGroupAction?.("removeFromGroup")}>
            <Ungroup className="size-3.5" />
            移出图片组合
          </ActionButton>
        </div>
      ) : imageGroupContext?.canMergeBefore ||
        imageGroupContext?.canMergeAfter ? (
        <div className="grid gap-1 border-t pt-2">
          <MenuLabel>横排组合</MenuLabel>
          {imageGroupContext.canMergeBefore ? (
            <ActionButton onClick={() => onImageGroupAction?.("mergeBefore")}>
              <Images className="size-3.5" />
              与上一张图片组合
            </ActionButton>
          ) : null}
          {imageGroupContext.canMergeAfter ? (
            <ActionButton onClick={() => onImageGroupAction?.("mergeAfter")}>
              <Images className="size-3.5" />
              与下一张图片组合
            </ActionButton>
          ) : null}
        </div>
      ) : null}
      <DangerButton onClick={onDelete}>删除图片</DangerButton>
    </div>
  );
}

function ImageGroupMenu({
  caption,
  columns,
  onApplyCaption,
  onCaptionChange,
  onColumnsChange,
  onUngroup,
}: Readonly<{
  caption: string;
  columns: number;
  onApplyCaption: () => void;
  onCaptionChange: (value: string) => void;
  onColumnsChange: (columns: number) => void;
  onUngroup: () => void;
}>) {
  return (
    <div className="grid gap-2">
      <MenuLabel>图片组合设置</MenuLabel>
      <MenuField
        label="组合大题注（组合下方）"
        onChange={onCaptionChange}
        value={caption}
      />
      <ActionButton onClick={onApplyCaption}>
        <Check className="size-3.5" />
        保存组合大题注
      </ActionButton>
      <div className="grid gap-1">
        <span className="text-muted-foreground">每行最多数量（宽度自适应）</span>
        <div className="grid grid-cols-4 gap-1">
          {[1, 2, 3, 4].map((value) => (
            <button
              aria-label={`每行 ${value} 张图片`}
              aria-pressed={columns === value}
              className="h-7 rounded border hover:bg-muted aria-pressed:border-primary aria-pressed:bg-primary/10"
              key={value}
              onClick={() => onColumnsChange(value)}
              type="button"
            >
              {value}
            </button>
          ))}
        </div>
      </div>
      <ActionButton onClick={onUngroup}>
        <Ungroup className="size-3.5" />
        拆分为独立图片
      </ActionButton>
    </div>
  );
}

function TableMenu({
  caption,
  onApplyCaption,
  onCaptionChange,
  onTableAction,
}: Readonly<{
  caption: string;
  onApplyCaption: () => void;
  onCaptionChange: (value: string) => void;
  onTableAction: (action: TableAction) => void;
}>) {
  return (
    <div className="grid gap-2">
      <MenuLabel>表格设置</MenuLabel>
      <MenuField
        label="表注（表格上方）"
        onChange={onCaptionChange}
        value={caption}
      />
      <ActionButton onClick={onApplyCaption}>
        <Check className="size-3.5" />
        保存表注
      </ActionButton>
      <div className="grid gap-1">
        <ActionButton onClick={() => onTableAction("toggleHeaderRow")}>
          <ChevronDown className="size-3.5" />
          切换首行为表头
        </ActionButton>
      </div>
      <DangerButton onClick={() => onTableAction("deleteTable")}>
        <Trash2 className="size-3.5" />
        删除表格
      </DangerButton>
    </div>
  );
}

function MenuField({
  label,
  onChange,
  value,
}: Readonly<{
  label: string;
  onChange: (value: string) => void;
  value: string;
}>) {
  return (
    <label className="grid gap-1 text-muted-foreground">
      {label}
      <input
        aria-label={label}
        className="h-8 rounded border bg-background px-2 text-foreground outline-none focus:ring-2 focus:ring-ring"
        onChange={(event) => onChange(event.target.value)}
        value={value}
      />
    </label>
  );
}

function MenuLabel({ children }: Readonly<{ children: ReactNode }>) {
  return <p className="font-medium text-foreground">{children}</p>;
}

function ActionButton({
  children,
  disabled,
  onClick,
}: Readonly<{
  children: ReactNode;
  disabled?: boolean;
  onClick: () => void;
}>) {
  return (
    <button
      className="flex min-h-7 items-center gap-1 rounded px-2 py-1 text-left hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
      disabled={disabled}
      onClick={onClick}
      type="button"
    >
      {children}
    </button>
  );
}

function DangerButton({
  children,
  onClick,
}: Readonly<{ children: ReactNode; onClick: () => void }>) {
  return (
    <button
      className="flex min-h-7 items-center gap-1 rounded px-2 py-1 text-left text-destructive hover:bg-destructive/10"
      onClick={onClick}
      type="button"
    >
      {children}
    </button>
  );
}

function IconAction({
  active,
  children,
  label,
  onClick,
}: Readonly<{
  active: boolean;
  children: ReactNode;
  label: string;
  onClick: () => void;
}>) {
  return (
    <button
      aria-label={label}
      aria-pressed={active}
      className="flex h-7 items-center justify-center rounded border hover:bg-muted aria-pressed:border-primary aria-pressed:bg-primary/10"
      onClick={onClick}
      type="button"
    >
      {children}
    </button>
  );
}
