import { describe, expect, it } from "vitest";

import {
  articleOutlineDefaultHeight,
  articleSidebarDefaultWidth,
  clampArticleOutlineHeight,
  clampArticleSidebarWidth,
} from "@/features/article/article-layout";

describe("Article writing layout", () => {
  it("keeps drag-resized sidebar widths within the usable range and respects container width", () => {
    expect(clampArticleSidebarWidth(100)).toBe(220);
    expect(clampArticleSidebarWidth(391.6)).toBe(392);
    expect(clampArticleSidebarWidth(900)).toBe(900);
    expect(clampArticleSidebarWidth(900, 1000)).toBe(640);
    expect(clampArticleSidebarWidth(Number.NaN)).toBe(
      articleSidebarDefaultWidth,
    );
  });

  it("clamps outline heights between minimum and maximum boundaries", () => {
    expect(clampArticleOutlineHeight(50)).toBe(100);
    expect(clampArticleOutlineHeight(250.4)).toBe(250);
    expect(clampArticleOutlineHeight(800)).toBe(600);
    expect(clampArticleOutlineHeight(Number.NaN)).toBe(
      articleOutlineDefaultHeight,
    );
  });
});
