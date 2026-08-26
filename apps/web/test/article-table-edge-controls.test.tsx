import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ArticleTableEdgeControls } from "@/features/article/article-table-edge-controls";

describe("ArticleTableEdgeControls", () => {
  it("offers row actions from the left edge handle", () => {
    const onAction = vi.fn();
    render(
      <ArticleTableEdgeControls
        handle={{
          axis: "row",
          cellPos: 2,
          index: 1,
          left: 10,
          tablePos: 1,
          top: 20,
        }}
        menuOpen
        onAction={onAction}
        onClose={vi.fn()}
        onPointerDown={vi.fn()}
        onToggleMenu={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("menuitem", { name: "在上方插入行" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "在下方插入行" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "删除当前行" }));
    expect(onAction.mock.calls.map(([action]) => action)).toEqual([
      "addBefore",
      "addAfter",
      "delete",
    ]);
  });

  it("offers column actions from the top edge handle", () => {
    render(
      <ArticleTableEdgeControls
        handle={{
          axis: "column",
          cellPos: 2,
          index: 0,
          left: 10,
          tablePos: 1,
          top: 20,
        }}
        menuOpen
        onAction={vi.fn()}
        onClose={vi.fn()}
        onPointerDown={vi.fn()}
        onToggleMenu={vi.fn()}
      />,
    );
    expect(
      screen.getByRole("menuitem", { name: "在左侧插入列" }),
    ).toBeVisible();
    expect(
      screen.getByRole("menuitem", { name: "在右侧插入列" }),
    ).toBeVisible();
  });
});
