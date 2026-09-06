// @vitest-environment jsdom

import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ArticleTagRail } from "@/features/article/article-tag-rail";
import type { ArticleBlock, ArticleChapterTag } from "@/features/article/types";

afterEach(cleanup);

const blocks: ArticleBlock[] = [
  {
    attrs: {},
    block_id: "heading-1",
    node_type: "heading",
    ordinal: 0,
    provenance: {},
    tag: "human_draft",
    text: "方法",
    updated_at: "2026-08-24T01:00:00Z",
  },
  {
    attrs: {},
    block_id: "paragraph-1",
    node_type: "paragraph",
    ordinal: 1,
    provenance: { agent_id: "agent-1" },
    tag: "ai_revision",
    text: "正文",
    updated_at: "2026-08-24T02:00:00Z",
  },
];

const chapters: ArticleChapterTag[] = [
  {
    chapter_tag_id: "chapter-tag-1",
    heading_block_id: "heading-1",
    heading_block_type: "heading",
    heading_fingerprint: "sha256",
    project_id: "project-1",
    status: "unreviewed",
    updated_at: "2026-08-24T03:00:00Z",
    updated_by: "reviewer-1",
  },
];

describe("Article tag rail", () => {
  it("uses an independent chapter tag at headings and block tags elsewhere", async () => {
    const reviewBlock = vi.fn().mockResolvedValue(undefined);
    const reviewChapter = vi.fn().mockResolvedValue(undefined);
    render(
      <ArticleTagRail
        blocks={blocks}
        canEdit
        chapterTags={chapters}
        onLocate={vi.fn()}
        onReview={reviewBlock}
        onReviewChapter={reviewChapter}
        positions={{ "heading-1": 60, "paragraph-1": 120 }}
      />,
    );

    expect(
      screen.getByRole("button", { name: "章节 1：未审阅" }),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "块 2：AI 修订" })).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: "横向展开章节与块 tags" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "审阅章节 1" }));
    fireEvent.click(screen.getByRole("button", { name: "审阅块 2" }));

    await waitFor(() => {
      expect(reviewChapter).toHaveBeenCalledWith("chapter-tag-1");
      expect(reviewBlock).toHaveBeenCalledWith("paragraph-1");
    });
  });

  it("allows a reviewed block to withdraw its review", async () => {
    const reviewBlock = vi.fn().mockResolvedValue(undefined);
    render(
      <ArticleTagRail
        blocks={[{ ...blocks[1]!, tag: "reviewed" }]}
        canEdit
        chapterTags={[]}
        onLocate={vi.fn()}
        onReview={reviewBlock}
        onReviewChapter={vi.fn()}
        positions={{ "paragraph-1": 120 }}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "横向展开章节与块 tags" }),
    );
    const withdraw = screen.getByRole("button", { name: "撤回审阅块 2" });
    expect(withdraw).not.toBeDisabled();
    fireEvent.click(withdraw);
    await waitFor(() =>
      expect(reviewBlock).toHaveBeenCalledWith("paragraph-1"),
    );
  });
});
