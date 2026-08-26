"use client";

import { Check, CircleAlert, ChevronsLeft, ChevronsRight } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";

import type { ArticleBlock } from "./types";

const labels: Record<ArticleBlock["tag"], string> = {
  ai_draft: "AI 草稿",
  human_draft: "人工草稿",
  ai_revision: "AI 修订",
  human_revision: "人工修订",
  reviewed: "已审阅",
};

export type ArticleSectionTags = { blocks: ArticleBlock[]; title: string };

export function groupArticleSections(
  blocks: ArticleBlock[],
): ArticleSectionTags[] {
  const sections: ArticleSectionTags[] = [];
  let current: ArticleSectionTags = { blocks: [], title: "正文开头" };
  for (const block of blocks) {
    if (block.node_type === "heading") {
      if (current.blocks.length) sections.push(current);
      current = { blocks: [], title: block.text.trim() || "未命名章节" };
    }
    current.blocks.push(block);
  }
  if (current.blocks.length) sections.push(current);
  return sections;
}

export function ArticleTagPanel({
  blocks,
  canEdit,
  onReview,
}: Readonly<{
  blocks: ArticleBlock[];
  canEdit: boolean;
  onReview: (blockId: string) => Promise<void>;
}>) {
  const [pending, setPending] = useState<string>();
  const [error, setError] = useState<string>();
  const [expanded, setExpanded] = useState(true);
  const review = async (blockId: string) => {
    setPending(blockId);
    setError(undefined);
    try {
      await onReview(blockId);
    } catch (value) {
      setError(value instanceof Error ? value.message : "标记审阅失败");
    } finally {
      setPending(undefined);
    }
  };
  return (
    <section className="rounded-lg border bg-card lg:float-right lg:ml-3 lg:w-80">
      <div className="flex items-center justify-between px-3 py-2">
        <h2 className="text-sm font-medium">章节与块 tags</h2>
        <Button
          aria-label={expanded ? "收起只显示颜色" : "横向展开"}
          onClick={() => setExpanded((value) => !value)}
          size="sm"
          variant="ghost"
        >
          {expanded ? (
            <ChevronsLeft className="size-4" />
          ) : (
            <ChevronsRight className="size-4" />
          )}
        </Button>
      </div>
      <div className="max-h-64 space-y-3 overflow-auto border-t p-3">
        {groupArticleSections(blocks).map((section, sectionIndex) => (
          <section key={`${section.title}-${sectionIndex}`}>
            <div className="mb-1 flex items-center justify-between text-xs font-semibold">
              <span className="flex min-w-0 items-center gap-2">
                <TagMark label={section.blocks[0]?.tag ?? "human_draft"} />
                <span className="truncate">{section.title}</span>
              </span>
              <span className="text-muted-foreground">
                {
                  section.blocks.filter((block) => block.tag === "reviewed")
                    .length
                }
                /{section.blocks.length} 已审阅
              </span>
            </div>
            <div className="space-y-1">
              {section.blocks.map((block) => {
                const actor = String(
                  block.provenance.reviewed_by ??
                    block.provenance.agent_id ??
                    (block.tag.startsWith("ai_") ? "AI" : "Human"),
                );
                return (
                  <div
                    className="flex items-center gap-2 rounded border px-2 py-1.5 text-xs"
                    key={block.block_id}
                  >
                    <span
                      className="min-w-0 flex-1 truncate"
                      title={block.text}
                    >
                      {block.text || block.node_type}
                    </span>
                    <span
                      className="flex shrink-0 items-center gap-1"
                      title={`块 tag：${labels[block.tag]}`}
                    >
                      <TagMark expanded={expanded} label={block.tag} />
                    </span>
                    {expanded ? (
                      <>
                        <span
                          className="hidden text-muted-foreground lg:inline"
                          title={new Date(block.updated_at).toLocaleString()}
                        >
                          {actor}
                        </span>
                        <Button
                          aria-label={`审阅块 ${block.ordinal + 1}`}
                          disabled={
                            !canEdit ||
                            block.tag === "reviewed" ||
                            pending === block.block_id
                          }
                          onClick={() => void review(block.block_id)}
                          size="sm"
                          variant="ghost"
                        >
                          <Check className="size-3.5" />
                          审阅
                        </Button>
                      </>
                    ) : null}
                  </div>
                );
              })}
            </div>
          </section>
        ))}
        {!blocks.length ? (
          <p className="text-xs text-muted-foreground">
            保存草稿后，这里会显示稳定块 ID 对应的 tags。
          </p>
        ) : null}
        {error ? (
          <p className="flex items-center gap-1 text-xs text-destructive">
            <CircleAlert className="size-3.5" />
            {error}
          </p>
        ) : null}
      </div>
    </section>
  );
}

function TagMark({
  expanded = true,
  label,
}: Readonly<{ expanded?: boolean; label: ArticleBlock["tag"] }>) {
  const color =
    label === "reviewed"
      ? "bg-emerald-500"
      : label.startsWith("ai_")
        ? "bg-violet-500"
        : "bg-sky-500";
  return (
    <span className="inline-flex items-center gap-1 text-[10px] font-medium text-muted-foreground">
      <span aria-hidden="true" className={`size-2 rounded-full ${color}`} />
      {expanded ? <span>{labels[label]}</span> : null}
    </span>
  );
}
