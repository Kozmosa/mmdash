"use client";

import { Check, CircleAlert } from "lucide-react";
import { useState } from "react";

import { Badge } from "@/components/ui/badge";
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
    <details className="rounded-lg border bg-card" open>
      <summary className="cursor-pointer px-3 py-2 text-sm font-medium">
        行级与章节 tags
      </summary>
      <div className="max-h-64 space-y-3 overflow-auto border-t p-3">
        {groupArticleSections(blocks).map((section, sectionIndex) => (
          <section key={`${section.title}-${sectionIndex}`}>
            <div className="mb-1 flex items-center justify-between text-xs font-semibold">
              <span>{section.title}</span>
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
                    <Badge
                      className={
                        block.tag === "reviewed"
                          ? "border-emerald-600/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
                          : undefined
                      }
                    >
                      {labels[block.tag]}
                    </Badge>
                    <span
                      className="min-w-0 flex-1 truncate"
                      title={block.text}
                    >
                      {block.text || block.node_type}
                    </span>
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
    </details>
  );
}
