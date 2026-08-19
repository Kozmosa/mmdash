import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  ArticleTagPanel,
  groupArticleSections,
} from "@/features/article/article-tag-panel";
import type { ArticleBlock } from "@/features/article/types";

const blocks: ArticleBlock[] = [
  {
    attrs: {},
    block_id: "00000000-0000-4000-8000-000000000001",
    node_type: "heading",
    ordinal: 0,
    provenance: {},
    tag: "human_draft",
    text: "方法",
    updated_at: "2026-08-19T01:00:00Z",
  },
  {
    attrs: {},
    block_id: "00000000-0000-4000-8000-000000000002",
    node_type: "paragraph",
    ordinal: 1,
    provenance: { agent_id: "agent-1" },
    tag: "ai_revision",
    text: "结果段落",
    updated_at: "2026-08-19T02:00:00Z",
  },
];

describe("Article block and section tags", () => {
  it("groups stable blocks by heading and allows an explicit review", async () => {
    expect(groupArticleSections(blocks)).toEqual([{ blocks, title: "方法" }]);
    const review = vi.fn().mockResolvedValue(undefined);
    render(<ArticleTagPanel blocks={blocks} canEdit onReview={review} />);
    expect(screen.getByText("AI 修订")).toBeInTheDocument();
    expect(screen.getByText("agent-1")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "审阅块 2" }));
    await waitFor(() =>
      expect(review).toHaveBeenCalledWith(blocks[1]!.block_id),
    );
  });
});
