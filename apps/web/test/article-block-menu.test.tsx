// @vitest-environment jsdom

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ArticleBlockMenu } from "@/features/article/article-block-menu";

describe("Article block menu", () => {
  it("exposes block identity, insertion, duplication, review, and delete actions", () => {
    const onAction = vi.fn();
    const onCopyId = vi.fn();
    const onClose = vi.fn();
    const onCut = vi.fn();
    const onDelete = vi.fn();
    const onReview = vi.fn();
    render(
      <ArticleBlockMenu
        author="Human"
        blockId="block-1"
        canMoveDown
        canMoveUp
        canReview
        onAction={onAction}
        onClose={onClose}
        onCopyId={onCopyId}
        onCut={onCut}
        onDelete={onDelete}
        onReview={onReview}
        updatedAt="2026-08-24T00:00:00Z"
      />,
    );

    expect(screen.getByText("ID：block-1")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "上方插入空块" }));
    fireEvent.click(screen.getByRole("button", { name: "复制块" }));
    fireEvent.click(screen.getByRole("button", { name: "复制块 ID" }));
    fireEvent.click(screen.getByRole("button", { name: "剪切块" }));
    fireEvent.click(screen.getByRole("button", { name: "审阅通过" }));
    fireEvent.click(screen.getByRole("button", { name: "删除块" }));

    expect(onAction).toHaveBeenCalledWith("before");
    expect(onAction).toHaveBeenCalledWith("duplicate");
    expect(onCopyId).toHaveBeenCalledOnce();
    expect(onCut).toHaveBeenCalledOnce();
    expect(onReview).toHaveBeenCalledOnce();
    expect(onDelete).toHaveBeenCalledOnce();
  });
});
