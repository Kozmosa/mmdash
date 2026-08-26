"use client";

import {
  Check,
  ChevronsLeft,
  ChevronsRight,
  CircleAlert,
  Heading,
  Square,
} from "lucide-react";
import { useMemo, useState } from "react";

import { Button } from "@/components/ui/button";

import type { ArticleBlock, ArticleChapterTag } from "./types";

const blockLabels: Record<ArticleBlock["tag"], string> = {
  ai_draft: "AI 草稿",
  human_draft: "人工草稿",
  ai_revision: "AI 修订",
  human_revision: "人工修订",
  reviewed: "已审阅",
};

const chapterLabels: Record<ArticleChapterTag["status"], string> = {
  unedited: "未编辑",
  unreviewed: "未审阅",
  reviewed: "已审阅",
  needs_review: "需复审",
};

type Marker =
  | { block: ArticleBlock; kind: "block" }
  | { block: ArticleBlock; chapter?: ArticleChapterTag; kind: "chapter" };

export function ArticleTagRail({
  blocks,
  canEdit,
  chapterTags,
  onLocate,
  onReview,
  onReviewChapter,
  positions,
}: Readonly<{
  blocks: ArticleBlock[];
  canEdit: boolean;
  chapterTags: ArticleChapterTag[];
  onLocate: (blockId: string) => void;
  onReview: (blockId: string) => Promise<void>;
  onReviewChapter: (chapterTagId: string) => Promise<void>;
  positions: Record<string, number>;
}>) {
  const [expanded, setExpanded] = useState(false);
  const [pending, setPending] = useState<string>();
  const [error, setError] = useState<string>();
  const markers = useMemo<Marker[]>(() => {
    const chapters = new Map(
      chapterTags.map((item) => [item.heading_block_id, item]),
    );
    return blocks.map((block) =>
      block.node_type === "heading"
        ? { block, chapter: chapters.get(block.block_id), kind: "chapter" }
        : { block, kind: "block" },
    );
  }, [blocks, chapterTags]);

  const review = async (marker: Marker) => {
    const key =
      marker.kind === "chapter"
        ? marker.chapter?.chapter_tag_id
        : marker.block.block_id;
    if (!key) return;
    setPending(key);
    setError(undefined);
    try {
      if (marker.kind === "chapter") await onReviewChapter(key);
      else await onReview(key);
    } catch (value) {
      setError(value instanceof Error ? value.message : "审阅失败");
    } finally {
      setPending(undefined);
    }
  };

  return (
    <aside
      aria-label="章节与块 tags"
      className={`pointer-events-none absolute bottom-2 right-4 top-2 z-20 overflow-hidden transition-[width] ${expanded ? "w-72" : "w-7"}`}
    >
      <Button
        aria-label={expanded ? "收起只显示颜色" : "横向展开章节与块 tags"}
        className="pointer-events-auto absolute right-0 top-0 z-10 size-7 bg-background shadow-sm"
        onClick={() => setExpanded((value) => !value)}
        size="sm"
        title={expanded ? "收起只显示颜色" : "展开 tags 与审阅操作"}
        variant="outline"
      >
        {expanded ? (
          <ChevronsRight className="size-3.5" />
        ) : (
          <ChevronsLeft className="size-3.5" />
        )}
      </Button>
      {expanded && error ? (
        <p className="pointer-events-auto absolute right-0 top-9 flex w-72 items-center gap-1 rounded border border-destructive/30 bg-background px-2 py-1 text-[11px] text-destructive shadow-sm">
          <CircleAlert className="size-3.5 shrink-0" />
          <span className="truncate">{error}</span>
        </p>
      ) : null}
      {markers.map((marker) => {
        const { block } = marker;
        const top = positions[block.block_id];
        if (top === undefined || top < 34) return null;
        const isChapter = marker.kind === "chapter";
        const status = isChapter
          ? (marker.chapter?.status ?? "unedited")
          : block.tag;
        const label = isChapter
          ? chapterLabels[status as ArticleChapterTag["status"]]
          : blockLabels[status as ArticleBlock["tag"]];
        const reviewKey = isChapter
          ? marker.chapter?.chapter_tag_id
          : block.block_id;
        const reviewed = status === "reviewed";

        return expanded ? (
          <div
            className="pointer-events-auto absolute right-0 flex min-h-9 w-72 items-center gap-2 rounded-md border bg-background/95 px-2 py-1 text-[11px] shadow-sm backdrop-blur"
            key={`${marker.kind}:${block.block_id}`}
            style={{
              top,
              transform:
                "translate3d(0, var(--article-editor-scroll-offset, 0px), 0)",
            }}
          >
            <button
              className="flex min-w-0 flex-1 items-center gap-2 text-left"
              onClick={() => onLocate(block.block_id)}
              title={block.text || block.node_type}
              type="button"
            >
              <span
                aria-hidden="true"
                className={`h-4 w-1 shrink-0 rounded-full ${markerColor(marker)}`}
              />
              {isChapter ? (
                <Heading aria-hidden="true" className="size-3.5 shrink-0" />
              ) : (
                <Square aria-hidden="true" className="size-3.5 shrink-0" />
              )}
              <span className="shrink-0 font-medium">
                {isChapter ? "章节" : "块"}
              </span>
              <span className="truncate text-muted-foreground">
                {block.text || block.node_type}
              </span>
              <span className="shrink-0 text-right leading-tight">{label}</span>
            </button>
            <Button
              aria-label={`审阅${isChapter ? "章节" : "块"} ${block.ordinal + 1}`}
              disabled={
                !canEdit || reviewed || !reviewKey || pending === reviewKey
              }
              onClick={() => void review(marker)}
              size="sm"
              variant="ghost"
            >
              <Check className="size-3" />
              审阅通过
            </Button>
          </div>
        ) : (
          <button
            aria-label={`${isChapter ? "章节" : "块"} ${block.ordinal + 1}：${label}`}
            className={`pointer-events-auto absolute right-0 h-6 w-1.5 rounded-full shadow-sm transition-[width] hover:w-2.5 ${markerColor(marker)}`}
            key={`${marker.kind}:${block.block_id}`}
            onClick={() => onLocate(block.block_id)}
            style={{
              top,
              transform:
                "translate3d(0, var(--article-editor-scroll-offset, 0px), 0)",
            }}
            title={`${isChapter ? "章节" : "块"} · ${label}`}
            type="button"
          />
        );
      })}
    </aside>
  );
}

function markerColor(marker: Marker) {
  if (marker.kind === "chapter") {
    if (marker.chapter?.status === "reviewed") return "bg-emerald-500";
    if (marker.chapter?.status === "unreviewed") return "bg-amber-500";
    if (marker.chapter?.status === "needs_review") return "bg-orange-600";
    return "bg-slate-400";
  }
  if (marker.block.tag === "reviewed") return "bg-emerald-500";
  if (marker.block.tag.startsWith("ai_")) return "bg-violet-500";
  return "bg-sky-500";
}
